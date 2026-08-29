package ledger

import (
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

func TestState_HandleFor_Stable(t *testing.T) {
	s := New()
	id := model.EntityID("abc123")
	h1 := s.HandleFor(id)
	h2 := s.HandleFor(id)
	if h1 != h2 {
		t.Fatalf("handle changed across calls: %q then %q", h1, h2)
	}
	if h1 != "E1" {
		t.Fatalf("expected first handle to be E1, got %q", h1)
	}
	other := s.HandleFor(model.EntityID("def456"))
	if other != "E2" {
		t.Fatalf("expected second distinct entity to get E2, got %q", other)
	}
}

func TestState_MarkDelivered_OnlyUpgrades(t *testing.T) {
	s := New()
	id := model.EntityID("abc123")
	s.MarkDelivered(id, LevelSignature)
	if s.DeliveredAt(id) != LevelSignature {
		t.Fatalf("expected LevelSignature, got %v", s.DeliveredAt(id))
	}
	s.MarkDelivered(id, LevelName) // lower level, must not downgrade
	if s.DeliveredAt(id) != LevelSignature {
		t.Fatalf("MarkDelivered downgraded: got %v, want LevelSignature", s.DeliveredAt(id))
	}
	s.MarkDelivered(id, LevelBody)
	if s.DeliveredAt(id) != LevelBody {
		t.Fatalf("expected upgrade to LevelBody, got %v", s.DeliveredAt(id))
	}
}

func TestLoadSave_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.json")
	s := New()
	id := model.EntityID("abc123")
	s.HandleFor(id)
	s.MarkDelivered(id, LevelSkeleton)

	if err := Save(path, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.HandleFor(id) != "E1" {
		t.Errorf("handle did not round-trip: got %q", loaded.HandleFor(id))
	}
	if loaded.DeliveredAt(id) != LevelSkeleton {
		t.Errorf("delivered level did not round-trip: got %v", loaded.DeliveredAt(id))
	}
}

func TestLoad_MissingFile_ReturnsEmptyState(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load on a missing file should not error, got: %v", err)
	}
	if len(s.Handles) != 0 || len(s.Delivered) != 0 {
		t.Fatalf("expected a fresh empty state, got %+v", s)
	}
}
