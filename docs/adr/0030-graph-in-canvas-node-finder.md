# ADR-0030: In-canvas node finder for the Graph view

- **Status**: Accepted
- **Date**: 2026-09-15
- **Related**: ADR-0029 (the redesign this branches from), `internal/httpserver`'s existing
  `/api/related`-driven Graph view (no backend change here — purely client-side)

## Context

The user's own direct feedback after using the redesigned Graph view: "hace falta un buscador
dentro de los nodos" (a search is needed inside/among the nodes). The Graph view already has a
search input, but it does something different: it re-centers the ENTIRE graph on a new entity
(`api.related`, a fresh 2-hop fetch + re-layout). Once a neighborhood is loaded (typically 10-40
nodes, per `EntityGraphPanel`'s own doc comment on why React Flow was chosen over a GPU
point-renderer), there was no way to find a specific node already on screen without either
scanning visually or re-centering the whole graph on it (losing the current context).

## Decision: a second, purely client-side finder — no new API surface

A small floating input, positioned top-right of the canvas itself (distinct from the toolbar's
"center the graph on" search above it), filters among the nodes ALREADY loaded:

- Matching is a case-insensitive substring on the node's own label (its bare `Name`) — the same
  rule `EntityTable`'s own search box already uses, kept consistent rather than inventing a second
  matching convention.
- A match gets a highlighted ring (`--accent-ring`) and full opacity; every non-match dims to 35%
  opacity — both nodes and, implicitly, the edges between them (since a dimmed node's edges read
  as de-emphasized alongside it).
- The view pans/zooms to the current match automatically (`useReactFlow().fitView({ nodes: [{id}]
  })`), with a small counter (`2/5`) and up/down controls to cycle through the rest without
  retyping.
- Enter/Shift+Enter cycle forward/backward; Escape clears.

**Zero new backend work**: this operates entirely on the `nodes` array `EntityGraphPanel` already
holds in React state from the last `api.related` call — no new endpoint, no re-fetch, no
new latency. It answers "where, among what I already loaded, is X" — a different question from
the toolbar search's "load a new neighborhood around X".

## A real architectural wrinkle, found while building this

`useReactFlow()` (needed to pan/zoom programmatically) only works inside a `<ReactFlowProvider>`
— and critically, `<ReactFlow>` itself only provides that context to ITS OWN children, not to
whatever component renders it. The finder's logic could not simply live in `EntityGraphPanel`
alongside the existing `<ReactFlow>` JSX. Fixed by extracting a new `GraphCanvas` component (the
`<ReactFlow>` element plus the finder overlay) and wrapping only that in `<ReactFlowProvider>` —
`EntityGraphPanel` itself is otherwise unchanged; the Tree view branch, which never needs React
Flow's context, is unaffected.

## Verification

Live-tested against the real running production `ctxd` (via a separate, throwaway `npm run dev`
process — read-only, the production system service was never modified) before considering this
done: confirmed the finder correctly identifies and cycles through multiple real matches in the
actual Cartograph self-hosted graph, dims/highlights as designed, and doesn't collide visually with
the existing MiniMap/Controls in the canvas's other corners. `go build/vet/test -race`,
`golangci-lint run`, and the web project's own `npm run build`/`oxlint` all clean — no new
warnings beyond the pre-existing ones already present before this change.

## What this is explicitly NOT

- **Not a change to the toolbar's own "center the graph on" search** — that still re-fetches a new
  neighborhood, unchanged, and remains the way to navigate somewhere NOT already on screen.
- **Not a backend feature.** No new `/api/` route; purely a client-side overlay on data already in
  memory.
