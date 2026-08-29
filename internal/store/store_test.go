package store

import (
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/graph"
	"github.com/deatherick/cartograph/internal/model"
)

func buildTestGraph() (*graph.Graph, model.EntityID, model.EntityID) {
	g := graph.New()
	a := model.Entity{
		ID:   model.NewEntityID("repo", model.KindFunction, "a.ts#foo", "arity:0"),
		Kind: model.KindFunction, Lang: model.LangTS, Repo: "repo",
		Qualified: "a.ts#foo", Name: "foo", Signature: "foo(): void",
		Anchor: model.Anchor{File: "a.ts", StartByte: 0, EndByte: 10, StartLine: 1, EndLine: 3, ContentHash: "hash1"},
	}
	b := model.Entity{
		ID:   model.NewEntityID("repo", model.KindFunction, "b.ts#bar", "arity:1"),
		Kind: model.KindFunction, Lang: model.LangTS, Repo: "repo",
		Qualified: "b.ts#bar", Name: "bar", Signature: "bar(x: number): void",
		Anchor: model.Anchor{File: "b.ts", StartByte: 5, EndByte: 20, StartLine: 2, EndLine: 5, ContentHash: "hash2"},
	}
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEdge(model.Edge{
		ID: "edge1", Src: a.ID, Dst: b.ID, Kind: model.EdgeCalls,
		Confidence: 0.95, Provenance: model.ProvenanceDeterministic, Evidence: "same-file declaration",
	})
	return g, a.ID, b.ID
}

func TestWriteOpen_RoundTrip(t *testing.T) {
	g, aID, bID := buildTestGraph()
	path := filepath.Join(t.TempDir(), "graph.bin")

	if err := Write(path, "test-repo", g); err != nil {
		t.Fatalf("Write: %v", err)
	}

	snap, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if snap.Repo != "test-repo" {
		t.Errorf("Repo = %q, want %q", snap.Repo, "test-repo")
	}

	all := snap.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d entities, want 2", len(all))
	}

	a, ok := snap.Lookup(aID)
	if !ok {
		t.Fatal("Lookup(aID) not found")
	}
	if a.Name != "foo" || a.Qualified != "a.ts#foo" || a.Signature != "foo(): void" {
		t.Errorf("entity a round-tripped wrong: %+v", a)
	}
	if a.Anchor.File != "a.ts" || a.Anchor.StartLine != 1 || a.Anchor.EndLine != 3 || a.Anchor.ContentHash != "hash1" {
		t.Errorf("entity a anchor round-tripped wrong: %+v", a.Anchor)
	}

	b, ok := snap.Lookup(bID)
	if !ok {
		t.Fatal("Lookup(bID) not found")
	}
	if b.Name != "bar" {
		t.Errorf("entity b round-tripped wrong: %+v", b)
	}

	out := snap.FanOut(aID)
	if len(out) != 1 || out[0].Dst != bID || out[0].Kind != model.EdgeCalls || out[0].Src != aID {
		t.Fatalf("FanOut(aID) = %+v, want one CALLS edge to bID", out)
	}
	if out[0].Confidence != 0.95 || out[0].Provenance != model.ProvenanceDeterministic || out[0].Evidence != "same-file declaration" {
		t.Errorf("edge metadata round-tripped wrong: %+v", out[0])
	}

	in := snap.FanIn(bID)
	if len(in) != 1 || in[0].Src != aID || in[0].Dst != bID {
		t.Fatalf("FanIn(bID) = %+v, want one edge from aID", in)
	}

	if len(snap.FanOut(bID)) != 0 {
		t.Errorf("expected bID to have no outgoing edges, got %+v", snap.FanOut(bID))
	}
}

func TestOpen_MissingFile(t *testing.T) {
	_, err := Open(filepath.Join(t.TempDir(), "does-not-exist.bin"))
	if err == nil {
		t.Fatal("expected an error opening a nonexistent snapshot")
	}
}

func TestOpen_CorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.bin")
	if err := writeAtomic(path, []byte("not a snapshot")); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path)
	if err == nil {
		t.Fatal("expected an error opening a corrupt snapshot")
	}
}

func TestRelated_TraversesAcrossSnapshotBoundary(t *testing.T) {
	g, aID, bID := buildTestGraph()
	path := filepath.Join(t.TempDir(), "graph.bin")
	if err := Write(path, "test-repo", g); err != nil {
		t.Fatal(err)
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	related := snap.Related(aID, 2)
	if len(related) != 1 || related[0].Entity.ID != bID || related[0].Depth != 1 {
		t.Fatalf("Related(aID) = %+v, want one hop to bID at depth 1", related)
	}
}

func TestUpstream_OnlyFollowsIncomingEdges(t *testing.T) {
	// a CALLS b. Upstream(b) must find a (b's caller); Upstream(a) must
	// find nothing — a has no callers, only a callee — unlike Related,
	// which would find b from either direction.
	g, aID, bID := buildTestGraph()
	path := filepath.Join(t.TempDir(), "graph.bin")
	if err := Write(path, "test-repo", g); err != nil {
		t.Fatal(err)
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	up := snap.Upstream(bID, 0)
	if len(up) != 1 || up[0].Entity.ID != aID || up[0].Depth != 1 {
		t.Fatalf("Upstream(bID) = %+v, want one hop to aID at depth 1", up)
	}
	if up := snap.Upstream(aID, 0); len(up) != 0 {
		t.Fatalf("Upstream(aID) = %+v, want empty — a has no callers", up)
	}
}

func TestUpstream_UnlimitedDepthFollowsFullChain(t *testing.T) {
	// c CALLS b CALLS a (transitively) — Upstream(a, 0) must reach both b
	// (depth 1) and c (depth 2), unlike a depth-limited call.
	g := graph.New()
	a := model.Entity{ID: model.NewEntityID("repo", model.KindFunction, "a.ts#a", "arity:0"), Kind: model.KindFunction, Name: "a"}
	b := model.Entity{ID: model.NewEntityID("repo", model.KindFunction, "b.ts#b", "arity:0"), Kind: model.KindFunction, Name: "b"}
	c := model.Entity{ID: model.NewEntityID("repo", model.KindFunction, "c.ts#c", "arity:0"), Kind: model.KindFunction, Name: "c"}
	g.AddEntity(a)
	g.AddEntity(b)
	g.AddEntity(c)
	g.AddEdge(model.Edge{ID: "e1", Src: b.ID, Dst: a.ID, Kind: model.EdgeCalls, Provenance: model.ProvenanceDeterministic})
	g.AddEdge(model.Edge{ID: "e2", Src: c.ID, Dst: b.ID, Kind: model.EdgeCalls, Provenance: model.ProvenanceDeterministic})
	path := filepath.Join(t.TempDir(), "graph.bin")
	if err := Write(path, "test-repo", g); err != nil {
		t.Fatal(err)
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	up := snap.Upstream(a.ID, 0)
	byID := map[model.EntityID]int{}
	for _, r := range up {
		byID[r.Entity.ID] = r.Depth
	}
	if byID[b.ID] != 1 {
		t.Errorf("expected b at depth 1, got %+v", up)
	}
	if byID[c.ID] != 2 {
		t.Errorf("expected c at depth 2 (unlimited depth must reach it), got %+v", up)
	}
	// Depth-limited to 1 must NOT reach c.
	limited := snap.Upstream(a.ID, 1)
	for _, r := range limited {
		if r.Entity.ID == c.ID {
			t.Fatal("depth-limited Upstream(a, 1) must not reach c (2 hops away)")
		}
	}
}

func TestWrite_DanglingEdgeDropped(t *testing.T) {
	g := graph.New()
	a := model.Entity{ID: model.NewEntityID("repo", model.KindFunction, "a.ts#foo", "arity:0"), Kind: model.KindFunction, Name: "foo"}
	g.AddEntity(a)
	// An edge to an entity never added to the graph (e.g. cross-file
	// resolution to something outside this snapshot's scope) must be
	// dropped, not corrupt the written index.
	g.AddEdge(model.Edge{Src: a.ID, Dst: model.NewEntityID("repo", model.KindFunction, "ghost.ts#ghost", "arity:0"), Kind: model.EdgeCalls})

	path := filepath.Join(t.TempDir(), "graph.bin")
	if err := Write(path, "test-repo", g); err != nil {
		t.Fatalf("Write should not fail on a dangling edge: %v", err)
	}
	snap, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.FanOut(a.ID)) != 0 {
		t.Errorf("expected the dangling edge to be dropped, got %+v", snap.FanOut(a.ID))
	}
}
