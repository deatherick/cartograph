package main

import (
	"testing"

	"github.com/deatherick/cartograph/internal/project"
)

// TestReconcile_StartsStopsAndRestarts exercises reconcile's real diff
// logic against a REAL project registry (a temp $HOME, not a mock) — the
// same technique internal/project's own tests use — with fake
// start/stop closures recording what they were called with instead of
// actually spawning a watcher goroutine, so this test stays fast and
// deterministic while still exercising the real internal/project.List()
// read path ADR-0026's zero-argument ctxd mode depends on.
func TestReconcile_StartsStopsAndRestarts(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	dirA := t.TempDir()
	dirB := t.TempDir()
	dirBMoved := t.TempDir()

	if err := project.Add("a", dirA); err != nil {
		t.Fatalf("project.Add(a): %v", err)
	}
	if err := project.Add("b", dirB); err != nil {
		t.Fatalf("project.Add(b): %v", err)
	}

	running := map[string]runningProject{}
	var started, stopped []string
	start := func(name, root string) {
		started = append(started, name)
		running[name] = runningProject{root: root, done: make(chan struct{})}
	}
	stop := func(name string) {
		stopped = append(stopped, name)
		delete(running, name)
	}

	// First reconcile: both a and b are new, both start; nothing stops.
	reconcile(running, start, stop)
	if len(started) != 2 || len(stopped) != 0 {
		t.Fatalf("first reconcile: got started=%v stopped=%v, want both a and b started, none stopped", started, stopped)
	}
	if _, ok := running["a"]; !ok {
		t.Error("expected project a to be running after the first reconcile")
	}
	if _, ok := running["b"]; !ok {
		t.Error("expected project b to be running after the first reconcile")
	}

	// Second reconcile with no registry change: nothing starts or stops
	// again — reconcile must be idempotent against an unchanged registry.
	started, stopped = nil, nil
	reconcile(running, start, stop)
	if len(started) != 0 || len(stopped) != 0 {
		t.Fatalf("second reconcile (no change): got started=%v stopped=%v, want neither", started, stopped)
	}

	// Remove b from the registry: only b's watcher should stop.
	if err := project.Remove("b"); err != nil {
		t.Fatalf("project.Remove(b): %v", err)
	}
	started, stopped = nil, nil
	reconcile(running, start, stop)
	if len(started) != 0 || len(stopped) != 1 || stopped[0] != "b" {
		t.Fatalf("after removing b: got started=%v stopped=%v, want only b stopped", started, stopped)
	}
	if _, ok := running["b"]; ok {
		t.Error("expected project b to no longer be running after it was removed from the registry")
	}
	if _, ok := running["a"]; !ok {
		t.Error("expected project a to still be running — reconcile must not touch untouched projects")
	}

	// Add a new project c: only c should start.
	dirC := t.TempDir()
	if err := project.Add("c", dirC); err != nil {
		t.Fatalf("project.Add(c): %v", err)
	}
	started, stopped = nil, nil
	reconcile(running, start, stop)
	if len(started) != 1 || started[0] != "c" || len(stopped) != 0 {
		t.Fatalf("after adding c: got started=%v stopped=%v, want only c started", started, stopped)
	}

	// Re-add a at a NEW path: reconcile must restart it (stop then start),
	// not leave it silently watching the old, stale location.
	if err := project.Add("a", dirBMoved); err != nil {
		t.Fatalf("re-adding a at a new path: %v", err)
	}
	started, stopped = nil, nil
	reconcile(running, start, stop)
	if len(stopped) != 1 || stopped[0] != "a" {
		t.Fatalf("after moving a: got stopped=%v, want exactly [a]", stopped)
	}
	if len(started) != 1 || started[0] != "a" {
		t.Fatalf("after moving a: got started=%v, want exactly [a]", started)
	}
	if running["a"].root != dirBMoved {
		t.Fatalf("after moving a: running[a].root = %q, want %q", running["a"].root, dirBMoved)
	}
}

// TestReconcile_EmptyRegistry_StartsNothing verifies the zero-projects
// case (a fresh install, before `ctx project add` has ever been run)
// doesn't panic or start anything spurious.
func TestReconcile_EmptyRegistry_StartsNothing(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	running := map[string]runningProject{}
	var calls int
	start := func(name, root string) { calls++ }
	stop := func(name string) { calls++ }

	reconcile(running, start, stop)
	if calls != 0 {
		t.Errorf("expected no start/stop calls against an empty registry, got %d", calls)
	}
}
