package graph

import (
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

func entity(id model.EntityID, name, file string) model.Entity {
	return model.Entity{ID: id, Kind: model.KindFunction, Name: name, Anchor: model.Anchor{File: file}}
}

func TestAddEntity_ThenFanOutFanIn(t *testing.T) {
	g := New()
	a := entity("a", "a", "a.ts")
	b := entity("b", "b", "b.ts")
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEdge(model.Edge{ID: "e1", Src: a.ID, Dst: b.ID, Kind: model.EdgeCalls})

	if out := g.FanOut(a.ID); len(out) != 1 || out[0].Dst != b.ID {
		t.Fatalf("FanOut(a) = %+v, want one edge to b", out)
	}
	if in := g.FanIn(b.ID); len(in) != 1 || in[0].Src != a.ID {
		t.Fatalf("FanIn(b) = %+v, want one edge from a", in)
	}
}

func TestEntitiesInFile_ReturnsOnlyThatFilesEntities(t *testing.T) {
	g := New()
	g.AddEntity(entity("a", "a", "a.ts"))
	g.AddEntity(entity("b", "b", "b.ts"))
	g.AddEntity(entity("c", "c", "a.ts"))

	got := g.EntitiesInFile("a.ts")
	names := map[string]bool{}
	for _, e := range got {
		names[e.Name] = true
	}
	if len(got) != 2 || !names["a"] || !names["c"] {
		t.Fatalf("EntitiesInFile(a.ts) = %+v, want [a, c]", got)
	}
	if got := g.EntitiesInFile("nonexistent.ts"); got != nil {
		t.Fatalf("expected nil for a file with no entities, got %+v", got)
	}
}

func TestRemoveEntity_CleansEdgesInBothDirections(t *testing.T) {
	// a CALLS b, c CALLS a — removing a must clean up both a's own
	// out/in AND the neighbor-side slices (b's in, c's out) that
	// otherwise still reference a's now-deleted edges.
	g := New()
	a := entity("a", "a", "a.ts")
	b := entity("b", "b", "b.ts")
	c := entity("c", "c", "c.ts")
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEntity(c)
	g.AddEdge(model.Edge{ID: "e1", Src: a.ID, Dst: b.ID, Kind: model.EdgeCalls})
	g.AddEdge(model.Edge{ID: "e2", Src: c.ID, Dst: a.ID, Kind: model.EdgeCalls})

	g.RemoveEntity(a.ID)

	if _, ok := g.Entities[a.ID]; ok {
		t.Error("expected a to be removed from Entities")
	}
	if out := g.FanOut(a.ID); len(out) != 0 {
		t.Errorf("expected a's own FanOut to be empty, got %+v", out)
	}
	if in := g.FanIn(b.ID); len(in) != 0 {
		t.Errorf("expected b's FanIn (from the now-removed a->b edge) to be empty, got %+v", in)
	}
	if out := g.FanOut(c.ID); len(out) != 0 {
		t.Errorf("expected c's FanOut (the now-removed c->a edge) to be empty, got %+v", out)
	}
	// b and c themselves must survive — only a and its edges are gone.
	if _, ok := g.Entities[b.ID]; !ok {
		t.Error("expected b to still exist")
	}
	if _, ok := g.Entities[c.ID]; !ok {
		t.Error("expected c to still exist")
	}
}

func TestRemoveEntity_SelfLoop_DoesNotPanicOrLeak(t *testing.T) {
	g := New()
	a := entity("a", "a", "a.ts")
	g.AddEntity(a)
	g.AddEdge(model.Edge{ID: "e1", Src: a.ID, Dst: a.ID, Kind: model.EdgeCalls}) // a calls itself

	g.RemoveEntity(a.ID) // must not panic

	if _, ok := g.Entities[a.ID]; ok {
		t.Error("expected a to be removed")
	}
	if out := g.FanOut(a.ID); len(out) != 0 {
		t.Errorf("expected no dangling self-edge in FanOut, got %+v", out)
	}
}

func TestRemoveEntity_Unknown_IsNoop(t *testing.T) {
	g := New()
	g.AddEntity(entity("a", "a", "a.ts"))
	g.RemoveEntity("does-not-exist") // must not panic or affect anything
	if len(g.Entities) != 1 {
		t.Errorf("expected the unrelated removal to be a no-op, got %d entities", len(g.Entities))
	}
}

func TestRemoveFile_RemovesEveryEntityFromThatFileAndTheirEdges(t *testing.T) {
	// a and c live in a.ts; b lives in b.ts. a CALLS b, c CALLS b.
	// RemoveFile("a.ts") must remove both a and c (and their edges into
	// b), leaving b (in b.ts) untouched.
	g := New()
	a := entity("a", "a", "a.ts")
	b := entity("b", "b", "b.ts")
	c := entity("c", "c", "a.ts")
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEntity(c)
	g.AddEdge(model.Edge{ID: "e1", Src: a.ID, Dst: b.ID, Kind: model.EdgeCalls})
	g.AddEdge(model.Edge{ID: "e2", Src: c.ID, Dst: b.ID, Kind: model.EdgeCalls})

	removed := g.RemoveFile("a.ts")

	removedSet := map[model.EntityID]bool{}
	for _, id := range removed {
		removedSet[id] = true
	}
	if len(removed) != 2 || !removedSet[a.ID] || !removedSet[c.ID] {
		t.Fatalf("expected RemoveFile to return [a, c], got %+v", removed)
	}
	if _, ok := g.Entities[a.ID]; ok {
		t.Error("expected a removed")
	}
	if _, ok := g.Entities[c.ID]; ok {
		t.Error("expected c removed")
	}
	if _, ok := g.Entities[b.ID]; !ok {
		t.Error("expected b (a different file) to survive")
	}
	if in := g.FanIn(b.ID); len(in) != 0 {
		t.Errorf("expected b's FanIn to be empty (both its callers were removed), got %+v", in)
	}
	if got := g.EntitiesInFile("a.ts"); got != nil {
		t.Errorf("expected a.ts to have no entities left, got %+v", got)
	}
}

func TestRemoveFile_UnknownFile_IsNoop(t *testing.T) {
	g := New()
	g.AddEntity(entity("a", "a", "a.ts"))
	removed := g.RemoveFile("never-indexed.ts")
	if removed != nil {
		t.Errorf("expected nil for a file with no entities, got %+v", removed)
	}
	if len(g.Entities) != 1 {
		t.Errorf("expected the unrelated file's entity to be untouched, got %d entities", len(g.Entities))
	}
}

func TestAddEntity_SameIDMovedToNewFile_UpdatesByFileCleanly(t *testing.T) {
	// The same EntityID re-added under a different Anchor.File (a file
	// rename that preserves the entity's qualified name, since EntityID
	// deliberately excludes file — model.go) must not leave a phantom
	// entry in the OLD file's bucket.
	g := New()
	g.AddEntity(entity("a", "a", "old.ts"))
	g.AddEntity(entity("a", "a", "new.ts"))

	if got := g.EntitiesInFile("old.ts"); got != nil {
		t.Errorf("expected old.ts to have no entities after the move, got %+v", got)
	}
	got := g.EntitiesInFile("new.ts")
	if len(got) != 1 || got[0].ID != "a" {
		t.Errorf("expected new.ts to have exactly entity a, got %+v", got)
	}
}

func TestRelated_StillWorksAfterRemoval(t *testing.T) {
	g := New()
	a := entity("a", "a", "a.ts")
	b := entity("b", "b", "b.ts")
	c := entity("c", "c", "c.ts")
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEntity(c)
	g.AddEdge(model.Edge{ID: "e1", Src: a.ID, Dst: b.ID, Kind: model.EdgeCalls})
	g.AddEdge(model.Edge{ID: "e2", Src: b.ID, Dst: c.ID, Kind: model.EdgeCalls})

	g.RemoveEntity(b.ID)

	related := g.Related(a.ID, 2)
	if len(related) != 0 {
		t.Errorf("expected a to have no related entities left (b removed, c only reachable through b), got %+v", related)
	}
}
