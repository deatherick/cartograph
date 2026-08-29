# ADR-0018: `ctxd`'s own watch/reindex status (`/api/operations`)

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: `docs/MVP.md`'s "easy win" backlog review (this closes "Operations", the third and
  last item in the same batch as ADR-0016 "Paths" and ADR-0017 "Quality"), ADR-0012 (`ctxd`'s own
  scope: no daemon socket/RPC yet — this stays inside that restriction), ADR-0013/0015 (the
  existing HTTP adapter this reuses rather than inventing new plumbing)

## Context

`ctxd` has run since ADR-0012 with zero externally visible operational state: whether it's still
watching, when it last reindexed, whether that reindex succeeded, and what `bug_rate` the current
snapshot has were all only ever printed once to stdout and then lost. `docs/MVP.md` named this gap
explicitly as "Operations — `ctxd`'s own watch/reindex status," the third of three small,
high-value items picked in the same weighted-backlog batch as Paths and Quality.

## Decision: a new `internal/opstatus` package, read through the existing HTTP adapter

`internal/opstatus.Tracker` is a small, thread-safe struct — `StartedAt`, `Watching`,
`ReindexCount`, `LastReindexAt`, `LastReason`, `LastStats` (an `index.Stats`, so the current
`bug_rate`/disposition breakdown is visible here too, without needing a separate `ctx stats` call
against the same running daemon), `LastError`, `LastWatchError`. `cmd/ctxd`'s own reindex/watch
loop updates it at each of the five points that already existed (initial index, each debounced
reindex, watcher startup, a watch error, shutdown) — no new control flow, just recording what
already happens.

It is deliberately its own package, not folded into `internal/service`: this is daemon *process*
lifecycle, not product data. `internal/service`'s Stats method answers "what does the persisted
snapshot say" (works from any process, including one that never ran `ctxd` at all); `opstatus`
answers "is the specific running daemon healthy" (meaningless outside that one process). Mixing
them would break `internal/service`'s existing "the single layer that owns product logic" framing.

Surfaced through `internal/httpserver`'s existing `/api/` namespace — `httpserver.New` gains a
fourth, nilable parameter (`*opstatus.Tracker`); `/api/operations` serves its JSON snapshot, or 404
when nil (any caller with no daemon to report on, including `httpserver`'s own tests, which all
pass `nil` unchanged). This is the one deliberate exception to that package's stated "every handler
calls straight into `internal/service`" rule, called out explicitly in its doc comment. No new
socket, no new RPC protocol — reusing the HTTP server `ctxd --web` already runs keeps this inside
ADR-0012's still-standing "no daemon socket/RPC yet" restriction rather than quietly expanding it.

## What this is explicitly NOT

- **Not a CLI command.** `ctx` (the CLI) and `ctxd` (the daemon) are separate processes; every
  existing CLI command reads the persisted snapshot directly, never over HTTP. A `ctx operations
  <path>` command would need to know (or guess) which `--web` address a running daemon bound to —
  a real but separate design question, deferred rather than bolted on here.
- **Not surfaced in the Web UI yet.** The React UI (ADR-0015) has no Operations panel reading this
  endpoint. The data exists and is queryable; wiring a UI view is a natural, low-cost follow-up,
  not attempted here to keep this change reviewable, matching how ADR-0016 explicitly deferred MCP
  wiring for the project registry rather than bundling every possible surface into one change.
- **Not a health/liveness probe for orchestration.** No `/healthz`, no process exit code signaling.
  `ctxd` still has no system-service story at all (Phase 9, documented, not built).

## Verification

`internal/opstatus`: 4 new tests (initial state, a recorded success clearing a prior failure's
error and updating stats/count, a failure preserving the prior success's `LastStats`, watching/
watch-error state). `internal/httpserver`: 2 new tests (`nil` tracker → 404; a real tracker's
`Watching`/`ReindexCount`/`LastReason` round-trip through the JSON endpoint). `go build/vet/
test -race/lint` clean; CI green (macOS + Linux + lint).
