// Package graph is the in-memory adjacency structure the Context Compiler
// (Phase 2) will traverse. Phase 1 keeps it deliberately small: entities,
// resolved edges, and a depth-limited BFS — fan_in/fan_out and pre-baked
// centrality (docs/research/04-storage-and-graph-format.md, adopted from
// Grafel's ADR-0005) are noted as follow-ups once the graph has enough
// real edges to make them meaningful.
package graph

import (
	"github.com/deatherick/cartograph/internal/model"
)

// Graph holds every entity and every resolved edge for one repo.
type Graph struct {
	Entities map[model.EntityID]model.Entity
	out      map[model.EntityID][]model.Edge
	in       map[model.EntityID][]model.Edge
}

// New creates an empty graph.
func New() *Graph {
	return &Graph{
		Entities: map[model.EntityID]model.Entity{},
		out:      map[model.EntityID][]model.Edge{},
		in:       map[model.EntityID][]model.Edge{},
	}
}

// AddEntity registers an entity node.
func (g *Graph) AddEntity(e model.Entity) {
	g.Entities[e.ID] = e
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
