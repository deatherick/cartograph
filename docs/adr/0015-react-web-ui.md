# ADR-0015: Web UI rebuilt on React, reusing Grafel's UI (not just its patterns)

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: ADR-0013 (Web UI V0, vanilla JS — reversed by this ADR), ADR-0014 (impact analysis),
  `NOTICE.md`

## Context

ADR-0013 shipped a V0 web UI in plain HTML/CSS/JS, embedded via `go:embed`, explicitly chosen over
React to avoid a second (Node/npm) build pipeline. Real usage immediately surfaced two kinds of
problems: (1) the UI "se ve horrible, nada que ver con la de Grafel" — the vanilla implementation
looked and felt materially worse than Grafel's own dashboard, and (2) the one-shot canvas graph
was static, not navigable. Asked to fix this by studying Grafel's UI, the first response
(reimplement from scratch, inspired by screenshots only) was corrected directly: Grafel is
MIT-licensed, the original "study and reimplement, never copy" rule (ADR-0002) was the project
owner's own choice about the CORE engine, and they explicitly authorized reusing Grafel's actual
UI code for the web frontend specifically, since it carries none of the project's real
intellectual contribution. `NOTICE.md` records the resulting attribution in full; this ADR
records the technical decisions and the real bugs real usage found along the way.

## Decision: move to React + Vite + Tailwind, reusing real code from Grafel's `webui-v2`

A new `web/` directory (Vite + React + TypeScript) replaces the vanilla JS frontend. Built via
`npm run build`, then copied into `internal/httpserver/web/` (the directory `go:embed` reads —
`go:embed` cannot reference a parent directory, so this copy step, wired into a new `make web`
target, is the bridge between the two build systems). This is a real reversal of ADR-0013's
"no Node dependency" choice, made deliberately once the alternative (a UI that looked and worked
worse than the reference product) was judged the bigger cost. CI (`ci.yml`) now runs `make web`
(via a `setup-node` step) before the Go build/test/lint steps in both the `test` and `lint` jobs,
so CI always exercises a freshly-built frontend, never a possibly-stale committed one.

Design tokens (`tokens.css`, `app.css`), several generic UI primitives (button, card, badge,
pill, tooltip, dialog, popover, tabs, input, kbd, skeleton), and the NavRail/TopBar layout pattern
are adapted directly from Grafel's `webui-v2/src` — see `NOTICE.md` for the exact file list and
what was trimmed. Every adapted file carries a one-line attribution comment.

## Decision: `@cosmos.gl/graph` evaluated and rejected; `@xyflow/react` (React Flow) instead

The first graph implementation used `@cosmos.gl/graph` (WebGL, GPU-accelerated) — the same engine
Grafel's own `graph-canvas.tsx` uses, matching Grafel's actual dependency rather than inventing
something new. Real usage rejected it immediately: cosmos.gl is a GPU point-renderer built for
graphs with thousands of nodes, and it renders **no per-node text at all** — every node showed as
an unlabeled circle ("los grafos no tienen títulos ni nada distinguible, son solo círculos sin
sentido"). This is not a configuration gap; it is what the library is for. Cartograph's graphs are
always a bounded, `Related`-limited neighborhood (typically 10-40 nodes) — exactly React Flow's
scale, where each node is a real DOM element that can show a name, a Kind badge, and a color.
Switched to `@xyflow/react` with `dagre` for a stable, deterministic (non-physics) layout —
`dagre` is also one of Grafel's own listed dependencies (alongside `elkjs`), so this is still
"the same class of tool Grafel uses," just independently wired up, not ported code (see
`NOTICE.md`).

This also incidentally fixed an earlier cosmos.gl-specific instability: hand-tuned
`simulationRepulsion`/`Gravity`/`Friction` values caused the force simulation to fly apart faster
than a fixed `setTimeout`-based `fitView` could catch up, reported as "hace zoom y desaparece
todo" (the camera fit to a bounding box that had already exploded off-screen). Dagre's layout has
no such failure mode — it is a single deterministic pass, not an iterative simulation.

## Decision: Overview is one integrated workspace, not three disconnected pages

The first cut kept Overview (stats), a separate Explore page (search + inspector), a separate
Graph page, and a separate Impact page — reached by full navigation. Direct feedback rejected
this twice: first, that a stats-only Overview with no way to reach an entity's detail was "una
página estática inútil" (fixed by merging Explore's search+inspector into Overview as a real,
paginated, Kind-filterable table); second, that reaching Graph/Impact via separate page
navigations made the experience feel like "cada acción lleva a páginas diferentes" instead of one
place. The final shape: Overview's Kind-count cards are clickable (filter the table), and
selecting a row shows **Detail / Graph / Impact as tabs** in the same panel
(`EntityGraphPanel`/`EntityImpactPanel`, extracted into components so the exact same
implementation also backs the standalone `/graph` and `/impact` routes for free-form,
not-yet-selected-a-row exploration — one implementation, two entry points, never duplicated).

A further request — "el grafo es perfecto, pero me gustaría también un árbol o algo en texto" —
added a Graph/Tree view toggle inside `EntityGraphPanel`: the tree view renders the same
fan-in/fan-out data already fetched for the graph (`internal/service.Inspect`'s result, kept in
state alongside the graph's 2-hop `Related` data) as an indented, clickable text list — a
different reading of the same relationships, not a second dataset.

The standalone `/impact` route was simplified to git-diff analysis only, after "el impact
analysis quedó obsoleto, es un buscador que no se sabe lo que busca" — its free-text "by entity"
search mode had no visible context (no Kind, no file, nothing to recognize while typing), and is
now properly served by Overview's Impact tab instead (a resolved row, no search step at all).
Git-diff analysis keeps its own page since it starts from a ref, not a selectable row.

## Bugs found by real usage, fixed in the same pass

- **Ambiguous names failing raw, not gracefully.** A name matching more than one entity (a
  same-named struct/function across two files — real examples hit during testing:
  `frontierItem` in both `internal/store/reader.go` and `internal/graph/graph.go`; `New` across
  several packages) made `internal/service.Inspect`/`Impact` return their literal error string
  (`service: "X" is ambiguous across [...] — disambiguate with --file <substring>`) straight to
  the UI. Fixed with a disambiguation picker: `EntityGraphPanel`'s and the standalone Impact
  page's search first call `/api/find`; 0 matches errors clearly, 1 navigates directly, >1 shows
  a clickable candidate list (Kind + file per option). The graph panel's data-loading effect also
  catches the raw ambiguous-error string as a fallback (recovering into the same picker) for
  entry via a direct URL with no file hint, not just the search box.
- **A resolved entity's own file being dropped when linking between views.** Once
  `EntityDetail`'s "Graph"/"Impact" buttons only passed `?name=`, not `?file=`, a name that
  happened to be ambiguous elsewhere in the repo could still hit the raw error above even though
  the calling view already knew the exact file. Fixed by passing `&file=` through every such link
  and reading it back on the receiving page.
- **Self-navigation growing history without bound.** The center entity is itself one of the
  rendered graph nodes (so its own relationships are visible around it); clicking it — or an
  entity with no relationships at all, `fileEntry`, whose only clickable node in an otherwise
  empty graph is itself — re-invoked `navigateTo` with the SAME name/file as the current center,
  pushing an identical entry onto the breadcrumb history every time ("fileEntry / fileEntry /
  fileEntry / …", reported as an "infinite loop"). Fixed with a guard in `navigateTo` itself: if
  the target equals the current center, return the previous state unchanged (a true no-op, no
  history growth) — the single choke point both the graph's node-click handler and the tree
  view's link-click handler go through, so one fix covers both surfaces. Self-loop edges
  (an entity referencing itself) are also filtered out before reaching dagre's layout, since they
  add no navigational value in a top-down layout.

## Verification

`go build ./...`, `go vet ./...`, `go test ./... -race`, `golangci-lint run ./...` all clean
after every change in this ADR. `npm run build` (TypeScript strict-mode compile + Vite bundle)
clean. Manually verified end-to-end against this project's own self-hosted source after each fix
— including the exact failure cases reported (`frontierItem`, `New`, `fileEntry`) — via a
running `ctxd` instance, not just unit tests, matching this project's standing self-hosting
discipline.
