# ADR-0013: Web UI V0 — HTTP adapter + embedded vanilla-JS frontend

- **Status**: Accepted (V0 slice done)
- **Date**: 2026-08-29
- **Related**: ADR-0012 (daemon), `docs/requirements/phase6-web-ui.md`

## Context

Immediately after ADR-0012 (the daemon watcher), the user asked to continue toward Phase 6 — "quiero
que logremos llegar al punto de la visualización web de este proyecto." Phase 4 (impact analysis)
and Phase 5 (duplicate/similarity engine) — which `docs/requirements/phase6-web-ui.md` originally
listed as ideal preconditions for a *complete* Phase 6 — are not built. Rather than block on them,
this ships the slice of Phase 6 that today's real data (entities, edges, fan-in/fan-out, related
subgraphs) already supports: Overview, Search, Entity Inspector, and a bounded Graph view.
Duplicates and Impact stay explicitly deferred — there is nothing real to show in them yet.

## Decision: HTML/JS embedded in the Go binary, not React + TypeScript

`docs/requirements/phase6-web-ui.md`'s original capture described Grafel's own dashboard as
React + cosmos.gl + React Flow + elkjs, and the master plan's Phase 6 section assumed "React + TS
servida por el daemon." Asked directly, the user chose the alternative: plain HTML/CSS/JS,
embedded into the `ctxd` binary via `go:embed`, with no Node.js/npm/bundler toolchain at all.

Reasoning that made this the right default for this project specifically: every other interface
this project has (CLI, MCP) requires nothing beyond `go build`. A React frontend would introduce
a second, entirely separate build pipeline (`npm install`, a bundler, a `dist/` step ahead of
`go:embed`) for a project whose entire pitch is "a single Go tool." A vertical slice this small
(four views, one force-directed graph) does not yet need componentized state management, a
router, or a type system of its own — the complexity React solves does not exist yet at this
scale. If the UI grows enough that vanilla JS becomes the bottleneck, migrating is still possible
later; the reverse (ripping out a build pipeline once it's entrenched) is not.

## What was built

- **`internal/service.Graph`**: a new method returning every entity and edge in the persisted
  snapshot — nothing existing (CLI, MCP) previously needed the whole graph at once; only Related's
  bounded neighborhood. Edges are derived by concatenating every entity's `FanOut` once (every
  `model.Edge` has exactly one `Src`, so no dedup logic is needed) rather than adding a new
  accessor to `internal/store`.
- **`internal/httpserver`**: the thin HTTP adapter over `internal/service` — the same "adapters
  never duplicate service logic" rule `internal/mcpserver` already follows. Six endpoints
  (`/api/stats`, `/api/graph`, `/api/find`, `/api/inspect`, `/api/related`, `/api/source`), each a
  direct call into the exact `internal/service` method the CLI/MCP already use, plus
  `http.FileServer` over an embedded `web/` directory for the frontend itself.
- **The frontend** (`internal/httpserver/web/`): `index.html` + `style.css` (light/dark via
  `prefers-color-scheme`) + `app.js`. Search does client-side substring filtering over the
  already-fetched `/api/graph` entity list for a responsive typing feel, but the actual
  jump-to-entity always goes through the server's exact-match `/api/find`/`/api/inspect` — no
  resolution logic is duplicated client-side, only a filter over data the server already
  produced. The bounded Graph view runs a hand-written ~40-line force-directed layout (repulsion +
  spring simulation, fixed iteration count, no external library) over one entity's `/api/related`
  neighborhood — never the whole repo at once, which both this project's own scoping judgment and
  `docs/requirements/phase6-web-ui.md`'s original "Graph is built last on purpose, least value"
  note agree is the right call.
- **Wired into `cmd/ctxd`**: a new `--web` flag (default `127.0.0.1:7420`, matching the master
  plan's permanent "bind to localhost by default" restriction; empty disables it) starts the HTTP
  server alongside the existing watcher, so one daemon process does both jobs — indexing/watching
  and serving the UI — for the one project it was started against (no multi-project registry yet,
  ADR-0012's own documented gap).

## Verification

`internal/httpserver`'s 8 tests are integration tests against the real `internal/service` layer
(a temp-dir TypeScript fixture, indexed for real, queried through real HTTP requests via
`httptest.Server`) — not handler-level mocks, matching `internal/mcpserver`'s own testing
convention. Beyond unit tests: started `ctxd` against this project's own real source (63 files,
457 entities after this ADR's own new code), confirmed via `curl` that `/`, `/app.js`,
`/style.css` serve with correct content types, `/api/stats` reports real per-Kind counts,
`/api/inspect?name=Run` returns real fan-in/fan-out data, and `/api/related?name=Run&depth=1`
returns a real bounded neighborhood — the same self-hosting discipline every prior phase in this
project has applied to itself.

## What's still missing, and why

- **Duplicates and Impact views**: not built. They need Phase 5 (similarity engine) and Phase 4
  (impact analysis) respectively, neither of which exist. Not a UI gap — there is no backend data
  to show.
- **Manual entity classification/tagging**, **pattern identification as a first-class UI
  surface**, **filtering as a persistent cross-cutting capability**, and **Projects/Settings**:
  all explicitly named in `docs/requirements/phase6-web-ui.md`'s original ask, all still deferred
  — tracked there, not silently dropped.
- **No live updates**: the web UI does not currently know when `ctxd`'s watcher triggers a
  reindex — a browser tab open during a reindex shows stale data until manually refreshed. A
  real-time push (SSE/WebSocket) is a natural next refinement once this is used enough to justify
  it.
