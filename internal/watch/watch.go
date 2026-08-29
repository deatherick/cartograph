// Package watch is Phase 3d's file-watching layer: notice when a
// project's source changes and tell the caller WHICH files changed —
// debounced into one batch per burst of activity, never one signal per
// individual write. Since ADR-0020 (true per-file incremental indexing)
// this package surfaces the actual changed paths, not just a bare
// "something changed" pulse; cmd/ctxd (via internal/index.Indexer) still
// owns every decision about what to DO with that list — extract, resolve,
// invalidate — this package's only job is telling it what changed.
//
// Built on github.com/fsnotify/fsnotify (kqueue on macOS, inotify on
// Linux) rather than the FSEvents-based, zero-descriptor-per-file backend
// docs/research/05-watcher-and-invalidation.md's discovery on Grafel
// recommends for real production scale (Grafel measured ~40,000
// descriptors watching one ~32k-file repo via kqueue, against a ~61,440
// ceiling). fsnotify is the pragmatic choice for a V0 vertical slice on
// repos this project's own size (tens to low hundreds of files) —
// switching to a real FSEvents binding before this is used on a
// large real repo is a documented, deferred follow-up (docs/MVP.md), not
// a silently ignored warning.
package watch

import (
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/deatherick/cartograph/internal/exclude"
)

// DefaultDebounce mirrors the project plan's own figure for coalescing a
// burst of saves into one re-index, not one per keystroke-adjacent write.
const DefaultDebounce = 300 * time.Millisecond

// Watcher watches a directory tree and emits a debounced batch of changed
// (absolute) file paths on Events(). Errors() surfaces non-fatal watch
// errors (e.g. a directory removed out from under the watcher) for the
// caller to log; neither channel is ever closed except by Close().
type Watcher struct {
	fsw      *fsnotify.Watcher
	events   chan []string
	errs     chan error
	debounce time.Duration
	done     chan struct{}
}

// New starts watching root recursively, honoring the exact same
// directory exclusions (internal/exclude.SkipDir) a real index walk uses
// — so the watcher never burns a descriptor on node_modules, vendor,
// .git, or any other directory Run() would skip anyway. debounce<=0 uses
// DefaultDebounce.
func New(root string, debounce time.Duration) (*Watcher, error) {
	if debounce <= 0 {
		debounce = DefaultDebounce
	}
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		fsw:      fsw,
		events:   make(chan []string, 1),
		errs:     make(chan error, 1),
		debounce: debounce,
		done:     make(chan struct{}),
	}
	if err := w.addTree(root); err != nil {
		_ = fsw.Close()
		return nil, err
	}
	go w.loop()
	return w, nil
}

// addTree registers every non-excluded directory under root with the
// underlying watcher. fsnotify is not recursive on its own — this is what
// makes a tree watch out of it, both at startup and (via loop's handling
// of Create events) as new directories appear later.
func (w *Watcher) addTree(root string) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return nil
		}
		if path != root && exclude.SkipDir(info.Name()) {
			return filepath.SkipDir
		}
		return w.fsw.Add(path)
	})
}

// Events returns the debounced changed-paths channel: a receive means the
// tree changed (paths lists every distinct file touched, in no particular
// order) and activity has settled for at least the debounce period.
// Buffered size 1 — a caller still mid-reindex when more changes arrive
// does not miss them: instead of being dropped (the pre-ADR-0020 bare-
// signal behavior, safe only because there was no data to lose), further
// changes accumulate and MERGE into the next batch once the caller
// receives, so no changed path is ever silently lost waiting for a slow
// consumer.
func (w *Watcher) Events() <-chan []string { return w.events }

// Errors returns non-fatal watch errors as they occur.
func (w *Watcher) Errors() <-chan error { return w.errs }

// Close stops watching and releases the underlying OS resources.
func (w *Watcher) Close() error {
	close(w.done)
	return w.fsw.Close()
}

func (w *Watcher) loop() {
	var timer *time.Timer
	var timerC <-chan time.Time
	pending := map[string]struct{}{}

	for {
		select {
		case ev, ok := <-w.fsw.Events:
			if !ok {
				return
			}
			if ev.Op&fsnotify.Create != 0 {
				if info, statErr := os.Stat(ev.Name); statErr == nil && info.IsDir() && !exclude.SkipDir(filepath.Base(ev.Name)) {
					// Best-effort: a new subdirectory created while
					// watching (e.g. `mkdir` then files added into it)
					// must be added or its own future events are never
					// seen. A failure here (permissions, a race where the
					// dir vanished already) is not fatal to the watch as
					// a whole.
					_ = w.addTree(ev.Name)
				}
			}
			pending[ev.Name] = struct{}{}

			if timer == nil {
				timer = time.NewTimer(w.debounce)
			} else {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(w.debounce)
			}
			timerC = timer.C

		case <-timerC:
			timerC = nil
			if len(pending) == 0 {
				continue
			}
			paths := make([]string, 0, len(pending))
			for p := range pending {
				paths = append(paths, p)
			}
			select {
			case w.events <- paths:
				pending = map[string]struct{}{}
			default:
				// The previous batch is still unconsumed — keep
				// accumulating (pending is left as-is, so a future
				// successful send carries everything merged; nothing
				// changed is ever lost) and retry the send after another
				// debounce period rather than waiting for a new fs event
				// that may never come before the consumer catches up.
				timer.Reset(w.debounce)
				timerC = timer.C
			}

		case err, ok := <-w.fsw.Errors:
			if !ok {
				return
			}
			select {
			case w.errs <- err:
			default:
			}

		case <-w.done:
			return
		}
	}
}
