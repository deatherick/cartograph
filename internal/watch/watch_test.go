package watch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatch_FileChange_FiresDebouncedEvent(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-w.Events():
	case err := <-w.Errors():
		t.Fatalf("unexpected watch error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("no change event received within 2s of writing a file")
	}
}

func TestWatch_FileChange_ReportsTheChangedPath(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	target := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(target, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case paths := <-w.Events():
		found := false
		for _, p := range paths {
			if p == target {
				found = true
			}
		}
		if !found {
			t.Fatalf("expected %q in the reported changed paths, got %v", target, paths)
		}
	case err := <-w.Errors():
		t.Fatalf("unexpected watch error: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("no change event received within 2s of writing a file")
	}
}

func TestWatch_Burst_CoalescesIntoOneEventWithEveryDistinctPath(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 80*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	a := filepath.Join(dir, "a.txt")
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(a, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(b, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := os.WriteFile(a, []byte("2"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case paths := <-w.Events():
		got := map[string]bool{}
		for _, p := range paths {
			got[p] = true
		}
		if !got[a] || !got[b] {
			t.Fatalf("expected both %q and %q in the coalesced batch, got %v", a, b, paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no change event received")
	}

	select {
	case paths := <-w.Events():
		t.Fatalf("received a second event from a single coalesced burst: %v", paths)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestWatch_SlowConsumer_MergesInsteadOfDroppingChanges(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	a := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(a, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Let a's batch flush into the (empty, buffered-1) channel and sit
	// there unconsumed — nothing has called Events() yet.
	time.Sleep(60 * time.Millisecond)

	// Two more changes while that first batch is still unconsumed. The
	// old bare-signal Watcher had nothing to lose here; this one must not
	// drop b/c's paths just because a's batch hasn't been read yet.
	b := filepath.Join(dir, "b.txt")
	if err := os.WriteFile(b, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Millisecond)
	c := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(c, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}

	// First receive: exactly the already-buffered batch from "a" alone —
	// it was sent before b/c ever existed.
	select {
	case paths := <-w.Events():
		if len(paths) != 1 || paths[0] != a {
			t.Fatalf("expected the first batch to be exactly [%q], got %v", a, paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no first event received")
	}

	// Second receive: b and c, merged into one batch rather than dropped
	// while they waited for the first batch to drain.
	select {
	case paths := <-w.Events():
		got := map[string]bool{}
		for _, p := range paths {
			got[p] = true
		}
		if !got[b] || !got[c] {
			t.Fatalf("expected b and c merged into the second batch, got %v", paths)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no second event received — b/c's changes were lost while the first batch sat unconsumed")
	}
}

func TestWatch_Burst_CoalescesIntoOneEvent(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	for i := 0; i < 5; i++ {
		if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("v"), 0o644); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no change event received")
	}

	// No second event should arrive shortly after — the burst above must
	// have coalesced into exactly one signal, not five.
	select {
	case <-w.Events():
		t.Fatal("received a second event from a single coalesced burst")
	case <-time.After(200 * time.Millisecond):
	}
}

func TestWatch_NewSubdirectory_IsWatchedToo(t *testing.T) {
	dir := t.TempDir()
	w, err := New(dir, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer func() { _ = w.Close() }()

	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Drain the event from the mkdir itself.
	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event for the new subdirectory's creation")
	}

	// A little time for addTree to register the new directory before a
	// file appears inside it.
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	select {
	case <-w.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event for a file created inside a newly-created subdirectory — addTree did not pick it up")
	}
}
