# ADR-0027: Web UI — the Duplicates view

- **Status**: Accepted
- **Date**: 2026-09-05
- **Related**: ADR-0021 (Similarity/Duplicate Engine V0 — the data this closes the loop on), ADR-0025
  (identifier normalization), ADR-0015 (React Web UI — the frontend architecture this extends,
  unchanged), ADR-0026 (Phase 9 — the live system-service `ctxd` this was verified against)

## Context

With Go/C#/Python extraction (Phase 3), the Similarity Engine's identifier-normalization follow-up
(ADR-0025), and Phase 9 (ADR-0026) all done, the user picked the Web UI's Duplicates view over
closing C#'s open Context Compiler recall gap or hardening the watcher further —
`docs/MVP.md`'s own long-standing note ("its data now exists, ADR-0021, but no UI panel reads it
yet") named exactly this gap.

## Decision: thin HTTP adapters, exactly the CLI's own service calls, nothing new

`internal/httpserver` gains three endpoints, none of which compute anything
`internal/service.Duplicates`/`Similar`/`Decide` (already built for `ctx duplicates`/`similar`/
`decide`) doesn't already answer — this package's own standing "thin adapter, never duplicate
logic" rule, unchanged since ADR-0013:

- `GET /api/duplicates?threshold=N` — every undecided pair repo-wide.
- `GET /api/similar?name=X` — every undecided pair involving one entity.
- `POST /api/decide?nameA=&nameB=&decision=` — record a human's decision.

`/api/decide` is this server's **first mutating endpoint** — deliberately `POST`, not `GET`, unlike
every other handler here: recording a decision is a real state change (a decided pair never
resurfaces again, in the browser or the CLI), and a `GET` a browser or a proxy might prefetch or
cache must never carry that risk. Rejects anything but `POST` with 405, and an unrecognized
`decision` value with 400 (`similar.ParseDecision`, the exact same validation `ctx decide` already
uses).

`web/src/pages/DuplicatesPage.tsx` renders every pair with its full score breakdown — Exact (a red
badge when true), Structural, Behavioral, Overall — never collapsed to one number, matching every
other surface this project reports a similarity score on (`ctx duplicates`'s own CLI output,
ADR-0021's own standing rule). A decision dropdown (the same five values `ValidDecisions()`
exposes) plus a "Record" button call `/api/decide`, then remove the pair from local state
immediately (a decided pair reappearing for up to 5 seconds — the poll interval — until the next
poll would read as broken, not just slow).

## A real, pre-existing bug found and fixed along the way — not something this ADR's page caused

Screenshotting the new page live (see Verification) found: opening
`http://127.0.0.1:7420/duplicates` directly — a fresh page load, exactly what a bookmark or a
browser refresh does — returned a bare **404 from the Go server itself**, not the React page.
Checking the two PRE-EXISTING client routes confirmed this was never specific to the new page:

```
GET /graph       -> 404
GET /impact      -> 404
GET /duplicates  -> 404
GET /            -> 200
```

`http.FileServer` over the embedded React build has no concept of a react-router client-side
route — it only knows about real files in the embedded filesystem, so any path that isn't a real
file 404s. This was invisible before because every route was always reached by loading `/` first
and clicking through client-side navigation (react-router intercepts that, never asking the
server); a fresh load or a bookmark of a deep route never happened to get exercised until this
session's own live verification actually tried it.

Fixed with `spaFallback` (`internal/httpserver`): wraps the file server so a request for a path
with no matching file AND no file extension (a route, not a missing asset — `/assets/typo.js`
still 404s for real) serves `index.html` instead, letting react-router take over client-side
exactly as it does from `/`. A standard, well-known pattern for any SPA served by a non-SPA-aware
file server — not invented here, just applied.

## Verification

5 new tests in `internal/httpserver` (duplicates listing, similar-for-one-entity, decide removing
a pair from later listings, decide rejecting GET, decide rejecting an invalid decision value) plus
2 regression tests for the SPA-fallback fix (`/graph`/`/impact`/`/duplicates`/an arbitrary nested
route all serve `index.html`; a genuinely missing asset still 404s). TypeScript compiles clean
(`tsc -b`); `go build/vet/test -race/lint` all clean.

**Live-verified against the real running system-service `ctxd`** from ADR-0026 (not a test
fixture): a real Playwright screenshot of `/duplicates?project=cartograph` (this project's own
self-hosted source, 995 entities across four languages) rendered real, previously-invisible
duplication — `text()`/`anchorFrom()` helper functions copy-pasted near-identically across all
four language extractors (`internal/parser/{golang,ts,csharp,python}`), scored Exact/Overall=1.00.
Clicking "Record" in the actual browser reduced the visible pair count by exactly one, and reading
`~/.cartograph/cartograph-<hash>/duplicate-decisions.json` afterward confirmed the decision was
genuinely persisted to disk — not assumed from the UI's own optimistic update alone.

## What this is explicitly NOT

- **Not a new similarity algorithm.** Zero changes to `internal/similar` — this ADR is entirely an
  adapter (HTTP) and a renderer (React) over what ADR-0021/ADR-0025 already built.
- **Not a Projects/Settings management page.** `docs/MVP.md`'s own remaining Web UI gap (add/
  remove a project from the UI itself) is untouched — still `ctx project add` only.
- **Not filtering/tagging/pattern-identification as a cross-cutting UI primitive** — `docs/MVP.md`'s
  own longer-standing Web UI wishlist items, unrelated to this one view.
