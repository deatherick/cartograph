// Command ctxd is the context engine daemon — Phase 3d's vertical slice,
// since ADR-0020 upgraded to TRUE per-file incremental indexing: index
// once, then watch each project's source (internal/watch) and re-index
// automatically whenever it changes, re-processing only the files a
// change actually affects (internal/index.Indexer) instead of re-walking
// and re-resolving the whole project every time.
//
// Multi-project since ADR-0019: ctxd accepts more than one <path>
// argument, one project each, and watches all of them concurrently from
// one process. Each <path> also accepts a name registered via `ctx
// project add` (internal/project.Resolve), same as every `ctx` CLI
// command already does.
//
// Two modes, since ADR-0026 (Phase 9):
//   - `ctxd <path> [<path>...]` — the original, explicit-argument mode,
//     UNCHANGED: a fixed set of projects for this process's whole
//     lifetime, exactly as ADR-0019 built it.
//   - `ctxd` with NO arguments — the mode a system-level service install
//     (`ctx service install`) actually uses: watches every project
//     currently in `~/.cartograph/projects.json` (internal/project), and
//     RECONCILES against that registry every registryPollInterval, so
//     `ctx project add`/`remove` while this daemon is already running
//     takes effect with no restart — see reconcile's own doc. This is
//     the "real ctxd project add/list for an already-running daemon" gap
//     docs/MVP.md and docs/requirements/phase9-global-install-and-daemon.md
//     both named as still open before this ADR.
//
// System-level installation (launchd on macOS, systemd --user on Linux)
// is `ctx service install/uninstall/status`, internal/sysservice — see
// ADR-0026 and docs/requirements/phase9-global-install-and-daemon.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/deatherick/cartograph/internal/httpserver"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/project"
	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
	"github.com/deatherick/cartograph/internal/store"
	"github.com/deatherick/cartograph/internal/watch"
)

// registryPollInterval is how often zero-argument mode re-reads
// ~/.cartograph/projects.json to reconcile which projects it watches —
// deliberately a different, coarser cadence than the Web UI's own 3-second
// live-refresh poll (ADR-0019's usePoll): that one re-reads an in-memory
// snapshot on every tick; this one does real file I/O against a registry
// that changes far less often (a human running `ctx project add`, not a
// continuously-updating stats view), so a slower cadence is the right
// tradeoff, not an arbitrary mismatch.
const registryPollInterval = 5 * time.Second

func main() {
	webAddr := flag.String("web", "127.0.0.1:7420", "address to serve the web UI on (127.0.0.1-only by default — see the project plan's permanent 'bind to localhost' restriction); empty disables it")
	flag.Parse()
	args := flag.Args()

	svc := service.New() // the HTTP/Web UI read side only — never used for indexing itself, see watchProject
	registry := httpserver.NewProjectRegistry(nil)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	if *webAddr != "" {
		go func() {
			handler := httpserver.New(svc, registry)
			fmt.Printf("ctxd: web UI at http://%s (operational status: /api/operations)\n", *webAddr)
			if err := http.ListenAndServe(*webAddr, handler); err != nil { //nolint:gosec // 127.0.0.1 default binding is the security boundary here, not timeouts on a local single-user dev tool
				fmt.Fprintf(os.Stderr, "ctxd: web UI server error: %v\n", err)
			}
		}()
	}

	if len(args) > 0 {
		runFixedSet(args, registry, sig)
		return
	}
	runRegistryReconciled(registry, sig)
}

// runFixedSet is ADR-0019's original explicit-argument mode, unchanged: a
// fixed set of projects for the whole process lifetime, each resolved
// through the project registry the same way every `ctx` CLI command
// already does (internal/project.Resolve).
func runFixedSet(args []string, registry *httpserver.ProjectRegistry, sig <-chan os.Signal) {
	handles := make([]httpserver.Project, len(args))
	for i, arg := range args {
		root := project.Resolve(arg)
		repo := service.RepoName(root)
		name := repo
		if root != arg {
			name = arg // arg was a registered project name — keep it as the friendlier identity
		}
		h := httpserver.Project{Name: name, Repo: repo, Root: root, Ops: opstatus.New()}
		handles[i] = h
		registry.Set(h)
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	for _, h := range handles {
		wg.Add(1)
		go func(h httpserver.Project) {
			defer wg.Done()
			watchProject(done, h)
		}(h)
	}

	<-sig
	fmt.Println("ctxd: shutting down")
	close(done)
	wg.Wait()
}

// runningProject is what runRegistryReconciled tracks per watched
// project: the root it's currently watching (so reconcile can detect a
// project RE-registered at a new path — internal/project.Add's own
// "re-adding points it at a new location" semantics — and restart its
// watcher, not just add/remove by name alone) and its own done channel.
type runningProject struct {
	root string
	done chan struct{}
}

// runRegistryReconciled is the zero-argument mode (ADR-0026): starts by
// watching every project in `~/.cartograph/projects.json`
// (internal/project, the same registry `ctx project add/list/remove`
// manages), then reconcile keeps that set current for as long as ctxd
// keeps running.
func runRegistryReconciled(registry *httpserver.ProjectRegistry, sig <-chan os.Signal) {
	fmt.Println("ctxd: no <path> given — watching every project registered via `ctx project add` (~/.cartograph/projects.json), reconciling every", registryPollInterval)

	running := map[string]runningProject{}
	var wg sync.WaitGroup

	start := func(name, root string) {
		repo := service.RepoName(root)
		h := httpserver.Project{Name: name, Repo: repo, Root: root, Ops: opstatus.New()}
		registry.Set(h)
		done := make(chan struct{})
		running[name] = runningProject{root: root, done: done}
		wg.Add(1)
		go func() {
			defer wg.Done()
			watchProject(done, h)
		}()
	}
	stop := func(name string) {
		close(running[name].done)
		delete(running, name)
		registry.Remove(name)
	}

	reconcile(running, start, stop) // seed the initial set before the first tick

	ticker := time.NewTicker(registryPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			reconcile(running, start, stop)
		case <-sig:
			fmt.Println("ctxd: shutting down")
			for name := range running {
				close(running[name].done)
			}
			wg.Wait()
			return
		}
	}
}

// reconcile re-reads the project registry and diffs it against running:
// starts watching anything newly registered, stops watching anything
// removed, and RESTARTS watching anything whose registered path changed
// (see runningProject's own doc for why) — a stale watcher pointed at an
// old path would otherwise keep running forever, silently never seeing
// the project's real, current location again.
func reconcile(running map[string]runningProject, start func(name, root string), stop func(name string)) {
	projects, err := project.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxd: reading project registry: %v\n", err)
		return
	}
	if len(projects) == 0 && len(running) == 0 {
		fmt.Fprintln(os.Stderr, "ctxd: no projects registered yet — run `ctx project add <name> <path>`; watching nothing until then")
	}

	registered := make(map[string]string, len(projects)) // name -> path
	for _, p := range projects {
		registered[p.Name] = p.Path
	}

	for name := range running {
		if _, ok := registered[name]; !ok {
			fmt.Printf("ctxd: %q removed from the project registry — stopping its watcher\n", name)
			stop(name)
		}
	}
	for name, path := range registered {
		if rp, ok := running[name]; ok {
			if rp.root != path {
				fmt.Printf("ctxd: %q re-registered at a new path — restarting its watcher (%s -> %s)\n", name, rp.root, path)
				stop(name)
				start(name, path)
			}
			continue
		}
		fmt.Printf("ctxd: %q added to the project registry — starting to watch %s\n", name, path)
		start(name, path)
	}
}

// watchProject runs one project's whole lifecycle: an initial full index
// (index.Indexer.FullIndex), then incremental updates
// (index.Indexer.UpdateFiles) as its watcher reports changed paths, each
// persisted via store.Write, until done is closed. Runs in its own
// goroutine so ctxd's multiple projects (ADR-0019) watch fully
// concurrently, each with its own opstatus.Tracker (h.Ops) and its own
// live Indexer (h.Root/h.Repo scope everything to this one project —
// nothing here is shared across projects).
func watchProject(done <-chan struct{}, h httpserver.Project) {
	label := h.Repo
	if h.Name != h.Repo {
		label = h.Name + "/" + h.Repo
	}
	ix := index.NewIndexer(h.Root, h.Repo)

	// persist writes ix's current graph to the same snapshot path
	// internal/service.Index would have used — internal/httpserver's read
	// side (svc.Stats/svc.Related/...) opens that same path, so it never
	// needs to know an Indexer exists at all.
	persist := func(stats index.Stats) error {
		path, err := store.SnapshotPath(h.Root, h.Repo)
		if err != nil {
			return fmt.Errorf("resolving snapshot path: %w", err)
		}
		meta := store.Meta{Files: stats.Files, Dispositions: stats.Dispositions}
		if err := store.Write(path, h.Repo, ix.Graph(), meta); err != nil {
			return fmt.Errorf("persisting snapshot: %w", err)
		}
		return nil
	}

	// finish is the shared tail of both the initial index and every
	// incremental update: persist, then record success/failure to h.Ops
	// (a persist failure always wins over a milder per-file extraction
	// error, since it means the snapshot on disk did NOT change — the
	// most important fact an operator needs), then print the same
	// IndexStats summary a full `ctx index` run would show.
	finish := func(reason string, stats index.Stats, updateErr error) {
		if err := persist(stats); err != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: %v\n", label, err)
			h.Ops.RecordReindexFailure(reason, err)
			return
		}
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: %s: %v\n", label, reason, updateErr)
			h.Ops.RecordReindexFailure(reason, updateErr)
		} else {
			h.Ops.RecordReindexSuccess(reason, stats)
		}
		fmt.Print(render.IndexStats(stats))
	}

	fmt.Printf("ctxd[%s]: initial index — indexing %s\n", label, h.Root)
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats, err := ix.FullIndex(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: index error: %v\n", label, err)
			h.Ops.RecordReindexFailure("initial index", err)
			return
		}
		finish("initial index", stats, nil)
	}()

	w, err := watch.New(h.Root, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxd[%s]: starting watcher: %v\n", label, err)
		return
	}
	defer func() { _ = w.Close() }()
	h.Ops.SetWatching(true)

	fmt.Printf("ctxd[%s]: watching %s for changes\n", label, h.Root)
	for {
		select {
		case paths := <-w.Events():
			func() {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				stats, updateErr := ix.UpdateFiles(ctx, paths)
				finish("change detected", stats, updateErr)
			}()
		case werr := <-w.Errors():
			fmt.Fprintf(os.Stderr, "ctxd[%s]: watch error: %v\n", label, werr)
			h.Ops.RecordWatchError(werr)
		case <-done:
			h.Ops.SetWatching(false)
			return
		}
	}
}
