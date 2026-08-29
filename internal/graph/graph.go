// Package graph is the in-memory adjacency structure the Context Compiler
// (Phase 2) traverses. Entities, resolved edges, and a depth-limited BFS
// — fan_in/fan_out and pre-baked centrality
// (docs/research/04-storage-and-graph-format.md, adopted from Grafel's
// ADR-0005) are noted as follow-ups once the graph has enough real edges
// to make them meaningful.
//
// Since ADR-0020 (true per-file incremental indexing), Graph also tracks
// which entities came from which file (byFile) and supports removing them
// — the operation incremental re-extraction needs: before re-adding a
// changed file's fresh entities, its OLD ones (and every edge touching
// them, in either direction) must be cleanly removed first, or a renamed/
// deleted entity would linger forever as a phantom node.
package graph

import (
	"github.com/deatherick/cartograph/internal/model"
)

// Graph holds every entity and every resolved edge for one repo.
type Graph struct {
	Entities map[model.EntityID]model.Entity
	out      map[model.EntityID][]model.Edge
	in       map[model.EntityID][]model.Edge
	byFile   map[string][]model.EntityID
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		Entities: map[model.EntityID]model.Entity{},
		out:      map[model.EntityID][]model.Edge{},
		in:       map[model.EntityID][]model.Edge{},
		byFile:   map[string][]model.EntityID{},
	}
}

// AddEntity registers an entity node, keyed by its ID. Re-adding an
// already-known ID is safe and idempotent — including when its
// Anchor.File has changed (a moved/renamed file whose entity kept the
// same ID, since EntityID deliberately excludes file — model.go): the
// stale byFile bucket for the OLD file is cleaned up first, so an entity
// never appears to belong to two files at once.
func (g *Graph) AddEntity(e model.Entity) {
	if old, exists := g.Entities[e.ID]; exists && old.Anchor.File != e.Anchor.File {
		g.byFile[old.Anchor.File] = removeID(g.byFile[old.Anchor.File], e.ID)
		if len(g.byFile[old.Anchor.File]) == 0 {
			delete(g.byFile, old.Anchor.File)
		}
	}
	g.Entities[e.ID] = e
	if !containsID(g.byFile[e.Anchor.File], e.ID) {
		g.byFile[e.Anchor.File] = append(g.byFile[e.Anchor.File], e.ID)
	}
}

// AddEdge registers a resolved edge. Edges with an empty Src (a module-
// scope reference, not attributed to any function/method — see
// internal/parser/ts's enclosingScope) are still recorded but never appear
// in traversal from a specific entity, since there is no specific entity
// to traverse from.
func (g *Graph) AddEdge(e model.Edge) {
	if e.Src != "" {
		g.out[e.Src] = append(g.out[e.Src], e)
	}
	if e.Dst != "" {
		g.in[e.Dst] = append(g.in[e.Dst], e)
	}
}

// FanOut returns e's outgoing edges — what it calls/extends/implements/uses.
func (g *Graph) FanOut(e model.EntityID) []model.Edge { return g.out[e] }

// FanIn returns e's incoming edges — who calls/extends/implements/uses it.
func (g *Graph) FanIn(e model.EntityID) []model.Edge { return g.in[e] }

// EntitiesInFile returns every entity currently registered under file —
// the "old state" incremental re-extraction needs to read BEFORE calling
// RemoveFile, e.g. to compare each entity's Anchor.ContentHash against a
// freshly re-extracted version and skip work entirely when nothing
// actually changed (docs/research/edge-case-backlog.md's F8: a revert to
// already-indexed content must be a no-op).
func (g *Graph) EntitiesInFile(file string) []model.Entity {
	ids := g.byFile[file]
	if len(ids) == 0 {
		return nil
	}
	out := make([]model.Entity, 0, len(ids))
	for _, id := range ids {
		if e, ok := g.Entities[id]; ok {
			out = append(out, e)
		}
	}
	return out
}

// RemoveEntity deletes id from the graph, along with every edge touching
// it in either direction — spliced out of whichever NEIGHBOR's out/in
// slice held that edge, not just id's own (an edge lives in two places:
// the source entity's out list and the destination entity's in list; both
// must be cleaned or a removed entity leaves a dangling edge behind that
// still resolves via FanOut/FanIn on the other endpoint). Cost is
// proportional to the number of edges touching id, not the whole graph.
func (g *Graph) RemoveEntity(id model.EntityID) {
	e, ok := g.Entities[id]
	if !ok {
		return
	}
	for _, edge := range g.out[id] {
		g.in[edge.Dst] = removeEdge(g.in[edge.Dst], edge)
	}
	for _, edge := range g.in[id] {
		g.out[edge.Src] = removeEdge(g.out[edge.Src], edge)
	}
	delete(g.out, id)
	delete(g.in, id)
	delete(g.Entities, id)
	g.byFile[e.Anchor.File] = removeID(g.byFile[e.Anchor.File], id)
	if len(g.byFile[e.Anchor.File]) == 0 {
		delete(g.byFile, e.Anchor.File)
	}
}

// RemoveFile removes every entity currently attributed to file (and,
// transitively via RemoveEntity, every edge touching any of them) and
// returns the IDs that were removed. A no-op returning nil if file has no
// registered entities — deleting an already-gone or never-indexed file is
// not an error.
func (g *Graph) RemoveFile(file string) []model.EntityID {
	ids := append([]model.EntityID(nil), g.byFile[file]...) // copy: RemoveEntity mutates g.byFile[file] as it goes
	for _, id := range ids {
		g.RemoveEntity(id)
	}
	return ids
}

func removeEdge(edges []model.Edge, target model.Edge) []model.Edge {
	kept := edges[:0]
	for _, e := range edges {
		if e != target {
			kept = append(kept, e)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func removeID(ids []model.EntityID, target model.EntityID) []model.EntityID {
	kept := ids[:0]
	for _, id := range ids {
		if id != target {
			kept = append(kept, id)
		}
	}
	return kept
}

func containsID(ids []model.EntityID, target model.EntityID) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

// Related does a depth-limited BFS from start, following edges in both
// directions (an entity's callers and callees are both "related" to it).
// maxDepth <= 0 defaults to 2, a reasonable interactive default before the
// Context Compiler's ranker (Phase 2) takes over relevance decisions.
func (g *Graph) Related(start model.EntityID, maxDepth int) []model.RelatedEntity {
	if maxDepth <= 0 {
		maxDepth = 2
	}
	visited := map[model.EntityID]bool{start: true}
	var out []model.RelatedEntity

	type frontierItem struct {
		id    model.EntityID
		depth int
	}
	frontier := []frontierItem{{start, 0}}

	for len(frontier) > 0 {
		cur := frontier[0]
		frontier = frontier[1:]
		if cur.depth >= maxDepth {
			continue
		}
		neighbors := append(append([]model.Edge{}, g.out[cur.id]...), g.in[cur.id]...)
		for _, edge := range neighbors {
			next := edge.Dst
			if next == cur.id || next == "" {
				next = edge.Src
			}
			if next == "" || visited[next] {
				continue
			}
			visited[next] = true
			ent, ok := g.Entities[next]
			if !ok {
				continue // dangling edge to an entity we never registered
			}
			out = append(out, model.RelatedEntity{Entity: ent, Depth: cur.depth + 1, Via: edge})
			frontier = append(frontier, frontierItem{next, cur.depth + 1})
		}
	}
	return out
}
