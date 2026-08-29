package opstatus

import (
	"errors"
	"testing"

	"github.com/deatherick/cartograph/internal/index"
)

func TestTracker_InitialSnapshot_NotWatchingNoReindexYet(t *testing.T) {
	tr := New()
	s := tr.Snapshot()
	if s.StartedAt.IsZero() {
		t.Error("expected StartedAt to be set at construction")
	}
	if s.Watching {
		t.Error("expected Watching=false before SetWatching is called")
	}
	if s.ReindexCount != 0 {
		t.Errorf("expected ReindexCount=0 before any reindex, got %d", s.ReindexCount)
	}
}

func TestTracker_RecordReindexSuccess_UpdatesStatsAndClearsError(t *testing.T) {
	tr := New()
	tr.RecordReindexFailure("initial index", errors.New("boom"))
	if s := tr.Snapshot(); s.LastError == "" {
		t.Fatal("expected LastError to be set after a recorded failure")
	}

	stats := index.Stats{Files: 5, Entities: 12}
	tr.RecordReindexSuccess("change detected", stats)

	s := tr.Snapshot()
	if s.ReindexCount != 2 {
		t.Errorf("ReindexCount = %d, want 2 (one failure + one success)", s.ReindexCount)
	}
	if s.LastReason != "change detected" {
		t.Errorf("LastReason = %q, want %q", s.LastReason, "change detected")
	}
	if s.LastStats.Files != 5 || s.LastStats.Entities != 12 {
		t.Errorf("LastStats = %+v, want Files=5 Entities=12", s.LastStats)
	}
	if s.LastError != "" {
		t.Errorf("expected LastError cleared after a subsequent success, got %q", s.LastError)
	}
	if s.LastReindexAt.IsZero() {
		t.Error("expected LastReindexAt to be set")
	}
}

func TestTracker_RecordReindexFailure_PreservesPriorStats(t *testing.T) {
	tr := New()
	tr.RecordReindexSuccess("initial index", index.Stats{Files: 5})
	tr.RecordReindexFailure("change detected", errors.New("parse error"))

	s := tr.Snapshot()
	if s.LastStats.Files != 5 {
		t.Errorf("expected LastStats to be preserved from the last success, got %+v", s.LastStats)
	}
	if s.LastError == "" {
		t.Error("expected LastError to be set")
	}
	if s.LastReason != "change detected" {
		t.Errorf("LastReason = %q, want %q", s.LastReason, "change detected")
	}
}

func TestTracker_SetWatchingAndRecordWatchError(t *testing.T) {
	tr := New()
	tr.SetWatching(true)
	if !tr.Snapshot().Watching {
		t.Fatal("expected Watching=true after SetWatching(true)")
	}
	tr.RecordWatchError(errors.New("descriptor limit hit"))
	s := tr.Snapshot()
	if s.LastWatchError == "" {
		t.Error("expected LastWatchError to be set")
	}
	// A watch error does not itself flip Watching off — the daemon decides
	// that independently (e.g. only on a fatal, unrecoverable watch failure).
	if !s.Watching {
		t.Error("expected Watching to remain true — RecordWatchError must not silently flip it")
	}
}
