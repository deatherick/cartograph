# ADR-0017: Persist the disposition breakdown / `bug_rate` into the snapshot

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: `docs/MVP.md`'s "easy win" backlog review (this closes the "Quality" item, alongside
  ADR-0016's "Paths"), `docs/research/02-refs-and-dispositions.md` (the disposition model this
  persists), ADR-0003/format.go (the snapshot binary format this bumps to version 2)

## Context

`index.Stats` (files/entities/edges/duration/disposition breakdown, and the `BugRate()` this
breakdown drives) has always been computed at index time and printed once by `ctx index`, then
discarded — `internal/store`'s persisted snapshot never wrote it. Anyone who wanted to know a
repo's current `bug_rate` had to re-run a full index, even though the graph itself was already
sitting on disk. This was a known, named gap (`docs/MVP.md`'s "Quality" item), picked as the second
half of the same weighted-backlog batch that produced ADR-0016 ("Paths").

## Decision: bump the snapshot format to version 2, add a disposition section

`store.Write` now takes a third argument, `store.Meta{Files, Dispositions}` — populated from
`index.Stats` at the one real call site (`service.Index`). The on-disk format
(`internal/store/format.go`) grows two new header fields (`filesCount`, `dispositionCount`) and one
new trailing section: `dispositionCount` fixed-width `(stringTableOffset, count)` records, reusing
the existing string-interning table (a `Disposition` string like `"bug-extractor"` costs one string
copy, same as any `Kind`/`EdgeKind`/`Provenance` value already does). `formatVersion` moves from 1
to 2 — an old snapshot fails to open with the existing, already-clear "unsupported format version …
reindex with `ctx index`" error; there is no silent version-1 fallback, matching the project's
existing "never guess" discipline for this file (`Open` already refused files it couldn't fully
trust).

`Snapshot` gains three read accessors: `Files() int`, `Dispositions() map[model.Disposition]int`,
and `BugRate() float64` — the last computed with the exact same formula as `index.Stats.BugRate()`
(bug-extractor + bug-resolver over total), now runnable from a snapshot alone. `service.Stats`
(backing `ctx stats`) gained `Files`, `Dispositions`, `BugRate` fields sourced from these, and
`render.Stats` prints them in the same disposition order `render.IndexStats` already used (factored
into a shared `writeDispositions` helper so the two never drift). A new `context_stats` MCP tool
(there was no MCP equivalent of `ctx stats` at all before this) exposes the same data to an agent.

## What this is explicitly NOT

- **Not a change to when dispositions are computed.** The resolver still classifies every ref
  exactly once, at index time, in `internal/resolve`. This ADR only stops discarding that result
  before it reaches disk.
- **Not `Duration`.** `index.Stats.Duration` (how long the last index run took) is not persisted —
  it answers "was indexing itself slow", a question about the last *run*, not the repo's current
  state, and has no natural "still true" meaning once time has passed. `Files`/`Dispositions`/
  `BugRate` all describe the graph as it stands now; `Duration` does not, so it stays run-scoped
  in `index.Stats` only.
- **Not a schema migration path.** A version-1 snapshot on disk is not auto-upgraded in place; the
  fix is the same one every prior format-incompatible change has required — reindex.

## Verification

`internal/store`: two new tests (`TestWriteOpen_PersistsFilesAndDispositions`,
`TestWriteOpen_EmptyMeta_YieldsZeroFilesAndBugRate`) verifying round-trip Files/Dispositions/
BugRate, including the zero-meta case every pre-existing store test now exercises (all of them pass
`Meta{}`, since none cared about this data). `internal/service`:
`TestService_Stats_SurfacesPersistedBugRate` verifies `Stats` returned after a real `Index` run
matches that same run's `index.Stats.BugRate()` exactly — persisted, not recomputed differently.
`internal/mcpserver`: `TestMCPServer_Stats` plus `context_stats` added to the existing
`TestMCPServer_ListTools` and `TestMCPServer_StructuredContentIsAlwaysAnObject` parity suites.
`go build/vet/test -race/lint` clean; CI green (macOS + Linux + lint).
