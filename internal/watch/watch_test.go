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
