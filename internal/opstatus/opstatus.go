// Package opstatus tracks ctxd's own operational state — when it started,
// whether its watcher is currently healthy, and what its last reindex did
// (succeeded with what stats, or failed with what error) — so that state
// can be queried from outside the daemon process instead of only ever
// being visible in its stdout log. This is deliberately NOT product data:
// internal/service (and the graph/entities it exposes) knows nothing about
// this; it exists purely so ctxd can answer "are you alive and current"
// (docs/MVP.md's "Operations" backlog item, ADR-0018).
package opstatus

import (
	"sync"
	"time"

	"github.com/deatherick/cartograph/internal/index"
)

// Status is ctxd's current operational snapshot. All fields are set by the
// daemon's own reindex/watch loop (cmd/ctxd) and read by whatever adapter
// wants to report them (today, internal/httpserver's /api/operations).
type Status struct {
	StartedAt      time.Time
	Watching       bool
	ReindexCount   int
	LastReindexAt  time.Time
	LastReason     string      // what triggered the last reindex ("initial index", "change detected", ...)
	LastStats      index.Stats // zero value until the first reindex completes
	LastError      string      // non-empty if the last reindex failed; cleared on the next success
	LastWatchError string      // non-empty if the watcher itself reported an error; not cleared automatically
}

// Tracker is a thread-safe holder for Status — one per running ctxd
// process, constructed once at startup and updated as reindexes/watch
// events happen. The zero value is not usable; use New.
type Tracker struct {
	mu     sync.RWMutex
	status Status
}

// New constructs a Tracker with StartedAt set to now.
func New() *Tracker {
	return &Tracker{status: Status{StartedAt: time.Now()}}
}

// Snapshot returns a copy of the current status — safe to serialize
// (e.g. as JSON) without holding any lock afterward.
func (t *Tracker) Snapshot() Status {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.status
}

// SetWatching records whether the watcher is currently believed healthy —
// true once watch.New succeeds, false if it's ever torn down.
func (t *Tracker) SetWatching(watching bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.Watching = watching
}

// RecordReindexSuccess records a completed reindex triggered by reason.
func (t *Tracker) RecordReindexSuccess(reason string, stats index.Stats) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.ReindexCount++
	t.status.LastReindexAt = time.Now()
	t.status.LastReason = reason
	t.status.LastStats = stats
	t.status.LastError = ""
}

// RecordReindexFailure records a reindex attempt that errored — the
// snapshot on disk is whatever the last successful reindex left (index.Run
// never partially overwrites it, ADR-0003), so LastStats is deliberately
// left untouched here; only LastError changes.
func (t *Tracker) RecordReindexFailure(reason string, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.ReindexCount++
	t.status.LastReindexAt = time.Now()
	t.status.LastReason = reason
	t.status.LastError = err.Error()
}

// RecordWatchError records an error the watcher itself reported (distinct
// from a reindex failure — the watcher can misbehave independently of
// whether the last reindex succeeded).
func (t *Tracker) RecordWatchError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.status.LastWatchError = err.Error()
}
