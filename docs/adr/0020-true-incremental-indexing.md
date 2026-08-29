# ADR-0020: True per-file incremental indexing

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: ADR-0012 (`ctxd`'s original full-reindex-on-every-change V0, explicitly scoped as
  "not the true per-file incremental indexing the project plan's Phase 3 ultimately wants" — this
  ADR delivers that), `docs/research/edge-case-backlog.md`'s F1-F9 ("Incremental indexing (Phase
  3)"), ADR-0019 (multi-project `ctxd`, which this composes with unchanged — each project gets its
  own live `Indexer`)

## Context

Since ADR-0012, `ctxd` re-indexed the **whole** project on every filesystem change — measured
acceptable at this project's own scale (well under a second on ~58 files), but explicitly named as
a scoping choice, not a solution: `internal/resolve`'s same-file/same-package/import-table tiers
mean one file's export list changing can affect edges the resolver computed for OTHER files that
import it, and correctly invalidating only what actually changed needs real dependency tracking —
work ADR-0012 deferred with a named list of edge cases (F1-F9) it would have to satisfy. The user
picked this item explicitly (after a weighted review of the remaining large backlog items) as the
next thing to build for real, not defer again.

## Decision: a live `Indexer`, a real reverse-dependency lookup, and remove-then-re-add at every layer

Four packages changed together, each adding exactly the operation the layer above it needed and no
more:

**`internal/watch`**: `Events()` changed from `<-chan struct{}` (a bare "something changed" pulse)
to `<-chan []string` — the actual changed (absolute) paths, deduplicated and debounced the same
way as before, but now carrying data a slow consumer must never silently lose: a full batch send
that would previously have been dropped (buffer full, `select`/`default`) now instead merges into
the next attempt, retried on a timer until the consumer catches up.

**`internal/graph`**: gained `RemoveEntity`/`RemoveFile` (backed by a new `byFile` index) and
`EntitiesInFile`. Removing an entity means finding every edge that touches it in **either**
direction and splicing it out of the **neighbor's** slice, not just the removed entity's own —
`Graph`'s adjacency was append-only before this; cost is proportional to the edges touching the
removed entity, not the whole graph.

**`internal/resolve`**: `Index.AddFile` is now idempotent (re-adding an already-known file
transparently calls the new `RemoveFile` first, so `byBareName`/`filesByDir` never accumulate
stale or duplicate entries from a file's previous version). The genuinely hard new piece is
`Index.Dependents(file) []string` — "who else needs re-resolving if this file changes": every file
whose import table (or barrel re-export chain) resolves to `file`, plus — for a directory-scoped
language like Go — its package siblings (`LanguagePolicy.SameScopeFiles`). Built without the core
resolver ever branching on language (the same "plug and play" invariant `TestArchitectureBoundary_
CoreNeverBranchesOnLang` already enforced): a new `ResolveImportTarget(idx, file, source)
(files []string, ok bool)` method on `LanguagePolicy` exposes each language's existing internal
import-path resolution standalone, uniformly (TypeScript resolves to at most one file; Go resolves
to every file in a package directory). Implemented as a scan over every registered file's own
(small) import table, not a maintained reverse index — cheap relative to the extraction/resolution
work incremental indexing exists to avoid repeating; a real reverse index is a legitimate future
optimization if profiling ever shows this scan is the bottleneck, not attempted without a
measurement to justify it.

**`internal/index`**: a new `Indexer` type (`indexer.go`) holds a graph and a resolver alive across
many updates. `FullIndex` behaves exactly like the pre-existing package-level `Run` (now a thin
wrapper over it — no duplicated logic between the one-shot and incremental paths). `UpdateFiles`
does the real work, in three phases per batch of changed paths:

1. Compute `Dependents` for every changed file **before** touching anything — this is what
   correctly captures a **deleted** file's importers/siblings, since that information would be
   gone from the resolver once the file is removed.
2. Re-extract every directly changed file (`reindexOneFile`): a file that no longer exists on disk
   is removed (graph + resolver + cached facts); a file whose freshly-extracted entities are
   byte-for-byte identical to what's already indexed (same IDs, same per-entity `ContentHash`) is
   left alone entirely — a real no-op, not just a cheap re-write.
3. Compute `Dependents` **again**, now that changed files are registered — this is what correctly
   captures a **newly created** file's importers, who only resolve to it now that it exists; a
   lookup made before phase 2 would have found nothing. Every impacted file is then re-resolved —
   a changed file with its just-extracted fresh facts, a dependent-only file (never itself edited)
   with its **cached** `model.FileFacts` (no re-extraction, no I/O, no parse — resolution is the
   only work repeated for it, exactly the "genuinely harder work" this ADR exists to make
   proportional, not skip).

A running `bug_rate`/disposition breakdown is maintained incrementally (each file's own disposition
counts cached, the repo-wide total adjusted by subtracting a file's old counts and adding its new
ones whenever it's re-resolved or removed) — so `UpdateFiles` reports the same repo-wide `Stats`
shape a full reindex would, without re-resolving the whole repo to get it.

**`cmd/ctxd`**: `watchProject` now builds one live `Indexer` per project (`FullIndex` once at
startup, `UpdateFiles` on every subsequent watch batch), persisting via `store.Write` after each —
the snapshot format/persistence layer is completely unchanged; only how the graph is *computed*
before that final write changed.

## The F1-F9 edge cases, addressed one by one

- **F1** (a failed extraction must not silently wipe a file's entities): `reindexOneFile` only
  calls `removeFileState` on a successful re-extraction; an extraction error leaves the file's
  prior graph/resolver state completely untouched and is surfaced as a returned error, not hidden.
- **F2** (stale-manifest infinite reindex loop): does not apply by construction — an `Indexer`'s
  bookkeeping is entirely in-memory and rebuilt fresh by `FullIndex` at every `ctxd` startup; there
  is no persisted manifest to go stale across restarts.
- **F3** (a failed pass must not corrupt the graph; last good snapshot preserved): `store.Write`'s
  existing atomic temp-file-then-rename is untouched; `cmd/ctxd`'s `finish` closure only records a
  failure (never overwrites a good snapshot with a bad one) when persistence itself errors.
- **F4** (a rename with unchanged content should re-anchor, not reindex): handled correctly, though
  not for free — `EntityID` deliberately excludes file (model.go), so a pure rename (old path
  removed, new path created, same entity content) produces the exact same ID before and after; the
  general remove-then-re-add path lands on the correct end state without any rename-specific
  matching logic. The cost is one file's worth of re-extraction rather than a true zero-cost
  re-anchor — a real, documented, and honestly reported optimization opportunity, not a
  correctness gap.
- **F5** (a large batch, e.g. a branch checkout touching hundreds of files, must not fall back to
  a full reindex): `UpdateFiles` processes exactly the accumulated changed-file set plus their
  computed dependents, regardless of size — no size-based fallback to full reindex exists or was
  added; verified live against this project's own real, self-hosted source (105 files, 676
  entities): a single-file rename with one cross-file dependent completed in ~4ms, against ~960ms
  for the full initial index.
- **F6** (`.git/HEAD` branch-change poller): explicitly out of scope — orthogonal to this ADR's
  correctness problem (watcher-lifecycle robustness, not resolve/graph invalidation); still not
  built.
- **F7** (delete-then-recreate-identical within one debounce window): `reindexOneFile` always
  reads current on-disk state at processing time, never reacts to individual intermediate fsnotify
  events — by the time a batch is processed, a file that ended up unchanged is caught by the same
  content-hash comparison as F8, a correct no-op regardless of how many events fired in between.
- **F8** (a revert to already-indexed content must be a no-op): the per-entity `ContentHash`
  comparison in `reindexOneFile`/`unchanged` is exactly this — verified by
  `TestIndexer_UpdateFiles_RevertToIdenticalContent_IsNoop`.
- **F9** (daemon down while changes happened, needs a reconcile pass on restart): satisfied by
  existing structure, not new code — `ctxd` always runs a fresh `FullIndex` at startup regardless
  of how long it was down, before ever switching to incremental `UpdateFiles`.

## What this is explicitly NOT

- **Not a reverse-import index.** `Dependents` scans every registered file's own import table on
  every call — fast at today's scale, a real optimization opportunity once profiling (not
  intuition) says otherwise.
- **Not zero-cost rename handling.** See F4 above — correct, not optimal.
- **Not a `.git/HEAD` branch poller (F6), not a churn quarantine, not exclusion-layer hardening** —
  unrelated watcher-robustness concerns, unchanged by this ADR.
- **Not a change to the persisted snapshot format.** `store.Write`/`store.Open` are untouched; only
  how the in-memory `graph.Graph` handed to `Write` is built incrementally changed.
- **Not wired into `ctx index` (the one-shot CLI) or MCP.** Both only ever run a single `FullIndex`
  (via the unchanged `Run` wrapper) and exit; there is no long-lived process to hold an `Indexer`
  across calls in either case.

## Verification

New tests at every layer: `internal/watch` (changed-path reporting, burst coalescing merges every
distinct path, a slow consumer's changes are merged into the next batch rather than dropped),
`internal/graph` (9 tests: entity/edge removal in both directions, self-loops, unknown IDs/files,
same-ID-moved-to-new-file, `Related` after removal), `internal/resolve` (`RemoveFile` pruning,
`AddFile` re-add idempotency/no-duplication, `Dependents` for TS imports/barrel re-exports/no-
importers and Go package-siblings-plus-importer), `internal/index` (8 `Indexer`-specific tests,
including the two strongest correctness checks: a real cross-file rename that only touches file A
correctly invalidates file B's stale import **without B ever being re-extracted**, and a full
sequence of incremental updates produces `Stats` — Files/Entities/`Dispositions`/`BugRate` —
**identical** to a completely fresh `FullIndex` of the same end state). Live-verified against this
project's own real, self-hosted, currently-running `ctxd` (two concurrent projects, ADR-0019): a
real cross-file rename in the `ts-basic` fixture correctly flipped the importing file's disposition
from `resolved` to `bug-resolver` and back on revert, confirmed via `/api/inspect`/`/api/find`
against the live daemon, not just unit tests. `go build/vet/test -race/lint` clean; CI green
(macOS + Linux + lint).
