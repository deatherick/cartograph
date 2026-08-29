# ADR-0019: `ctxd` multi-project + a live project switcher in the Web UI

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: ADR-0016 (`ctx project`, the CLI-only registry this daemon-side feature is
  explicitly distinct from), ADR-0012 (`ctxd`'s original single-project scope — the gap this
  closes), ADR-0015 (the React Web UI this extends), ADR-0018 (`opstatus`, now surfaced per-project
  instead of once)

## Context

ADR-0016 built `ctx project add/list/remove` — a CLI-only name→path registry — and explicitly
named what it was *not*: "the daemon-side multi-project registry a future `ctxd project add` would
need (one process watching/serving several projects at once)." `ctxd` itself, and
`internal/httpserver` behind it, both still took exactly one `root`/`repo` pair fixed at
construction; the Web UI had zero concept that more than one project could exist (no switcher, no
`project` query parameter anywhere in the frontend). The user asked for this gap closed directly,
with a live, visual demonstration: two real projects (this repo, `cartograph`, and the `ts-basic`
fixture) registered and watched simultaneously, switchable in the running Web UI, updating without
a manual reload as either project's source changes.

## Decision: `ctxd` takes N paths; `httpserver.New` takes `[]Project`; the frontend polls and switches

**`cmd/ctxd`** now accepts one or more positional `<path>` arguments (each also resolved through
`internal/project.Resolve`, so a registered short name works exactly like every `ctx` CLI command
already accepts). Each project gets its own goroutine running the same index-once-then-watch
lifecycle as before, its own `opstatus.Tracker`, and all of them run fully concurrently from one
process — closing exactly the gap ADR-0012/ADR-0016 both named as separate and harder than the
CLI-only registry.

**`internal/httpserver.New`** signature changed from `(svc, root, repo, ops)` to `(svc, []Project)`,
where `Project{Name, Repo, Root, Ops}` is the daemon-side analog of one registry row, except live.
Every `/api/` handler now resolves an optional `?project=` query parameter to pick which registered
project it answers for (`resolveProject`), defaulting to `projects[0]` when omitted — so a
single-project caller, still the common case, never needs to pass it. A named-but-unregistered
`?project=` is a clear 400, never a silent fallback to the default (a typo must look like an error,
not a query against the wrong project). A new `/api/projects` endpoint lists what's available
(name, repo, root, watching) for the frontend to populate a switcher from.

**The Web UI**: `web/src/lib/project-context.tsx` adds a `ProjectProvider`/`useProject()` React
context — fetches `/api/projects` once, persists the selection to `localStorage`, and is read by
every page/panel that calls the API (`Overview`, `EntityGraphPanel`, `EntityDetail`,
`EntityImpactPanel`, `ImpactPage`) rather than prop-drilling `project` through components that are
also used standalone with no natural place to receive one. `TopBar` gained the actual switcher (a
plain `<select>`, shown only when more than one project exists) plus an operations badge
(watching/not-watching, "reindexed Ns ago", from `/api/operations`) — the "scope selector" and
"health dot" the original TopBar's own comment had explicitly named as *not* built yet.

**"Real-time" without inventing a push channel**: no WebSocket/SSE infrastructure exists anywhere
in this codebase yet, and building one was more than this change needed. `web/src/hooks/usePoll.ts`
is a small shared hook — run a fetch immediately, then every 3 seconds — used by `AppShell` (stats
+ operations, for the badges) and `Overview` (stats + graph, for the entity table). Editing a
watched project's source and waiting a few seconds visibly updates the browser with no reload,
which is what "verlo en tiempo real" actually required; a real push channel remains a legitimate,
separate future upgrade if polling ever proves too coarse.

## Verification — measured, not assumed

Both `cartograph` (this repo, self-hosted) and `ts-basic` (the fixture) were registered via `ctx
project add` and watched concurrently by one real `ctxd --web` process. Verified live, not just
via unit tests:

- `/api/projects` correctly lists both with the right `watching: true` for each.
- `/api/stats?project=ts-basic` and `?project=cartograph` return different, correct entity/edge
  counts scoped to each project (66 vs. 622 entities at the time of this check).
- A real headless-browser screenshot of the running Web UI shows the `TopBar` switcher listing
  both projects, and selecting `ts-basic` correctly re-scopes the entity table, kind cards, and
  edge count to that project's own data — not cartograph's.
- Editing a source file inside the watched `ts-basic` fixture (adding one function) and waiting,
  with **no page reload and no re-selecting the project**, was picked up by the poll: the entity
  count in the already-open browser tab moved from 66 to 67 on its own. This is the literal
  "tiempo real" behavior asked for, confirmed by screenshot, not just by log output.
- `internal/httpserver`: `TestHTTPServer_MultiProject_ListsBoth`,
  `_StatsAreScopedPerProject`, `_UnknownProject_Is400`, `_OperationsIsPerProject`, plus
  `TestHTTPServer_New_PanicsWithNoProjects` — all new; every pre-existing single-project test
  updated to the new `[]Project` shape and still passing unchanged in behavior.
- `go build/vet/test -race/lint` clean; `npm run build` (`tsc -b && vite build`) clean;
  `npm run lint` (`oxlint`) shows only pre-existing warning classes already present in this
  codebase before this change (soft warnings, not CI-gated — `oxlint` is not run in CI, only
  `golangci-lint` is).

## What this is explicitly NOT

- **Not a push-based live-update channel.** Polling every 3 seconds, not a WebSocket/SSE
  subscription. Correct and simple for this scale; a real upgrade path if ever needed, not
  attempted here since polling already delivered the actual requirement.
- **Not a "Projects" management page in the Web UI.** No add/remove-project UI — projects are
  still only registered via the existing `ctx project add/list/remove` CLI (ADR-0016) and passed to
  `ctxd` as positional arguments. The switcher only *selects among* what's already registered and
  running.
- **Not cross-project search or a combined graph.** Each project's data stays fully scoped and
  separate; there is no "search across all my projects" or merged-graph view. Given how the
  underlying `internal/store` snapshots are already fully independent per repo, this would be a
  real, separate feature, not a side effect of this change.
- **Not a change to `ctx` (the CLI) or MCP.** Both remain single-path-per-call, as they already
  were; this ADR only changes `ctxd` (the daemon) and the Web UI behind it.
