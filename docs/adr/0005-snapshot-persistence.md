# ADR-0005: Snapshot persistence — binary format, ReadFile not mmap yet

- **Status**: Accepted (implemented)
- **Date**: 2026-08-29
- **Related**: ADR-0003 (data model), docs/research/04-storage-and-graph-format.md

## Context

ADR-0003 already decided the shape of the graph's on-disk format: binary, not JSON/gob/
FlatBuffers, with integer-indexed adjacency (CSR) so edges reference entities by array index
instead of by string ID — closing the O(R) neighbor-lookup weakness Grafel's own ADR-0016 left
open. This ADR records the concrete implementation and one additional scoping decision made
while building it.

Before this landed, every `ctx find`/`related`/`stats` invocation re-ran the full pipeline
(parse every file, resolve every ref) from scratch — there was nothing to persist between CLI
invocations. Phase 1's exit criterion (`ctx related` responds in <100ms) cannot hold under that
model once a repo is large enough that parsing dominates.

## Decision

`internal/store` implements the format: a header, a NUL-terminated string table (interning every
unique string once — Kind/Provenance/EdgeKind values, qualified names, everything), a
fixed-width entity record array sorted by `EntityID` bytes for binary search, and two
fixed-width edge-record arrays (out-edges grouped by source, in-edges grouped by destination) so
both traversal directions are O(1) once the start entity's index is known via one binary search.
`Write` is atomic (temp file + `os.Rename`).

**Scoping decision made during implementation, not in ADR-0003**: the reader (`Open`) loads the
whole file into memory with `os.ReadFile` rather than a real `mmap`. True zero-copy mmap earns
its complexity at Grafel-scale — a long-lived daemon holding many large repos' graphs
concurrently (docs/research/04's `~80×` cold-open number is specifically about avoiding a
JSON *parse*, which this format already avoids by construction; the *mmap-vs-read* choice on top
of that is a separate, smaller optimization that matters once files get large and a daemon holds
many of them resident). Cartograph has no daemon yet (Phase 3) and today's snapshots are single-
digit KB to low hundreds of KB. The on-disk layout is deliberately mmap-ready (fixed-width
records, sorted ID index, index-based CSR) specifically so swapping `ReadFile` for a real mmap
later is a localized change inside `reader.go`, not a format redesign.

`internal/service` was rewritten around this: `Index` runs the pipeline and persists a snapshot;
`Find`/`Related`/`Stats` read the persisted snapshot and return a clear error
(`no index found — run 'ctx index <path>' first`) rather than silently falling back to a full
reindex — an implicit fallback would quietly defeat the point of building this layer.

Snapshot location (provisional, since real project management — `ctx project add` — doesn't
exist yet): `~/.cartograph/<repo>-<hash8(abs path)>/graph.bin`. The path hash exists so two
different repos that happen to share a directory name don't silently collide.

## Consequences

**Positive**:
- `ctx find`/`related`/`stats` no longer reparse or re-resolve anything; measured wall time
  dropped from ~45-60ms (full pipeline) to ~5-13ms (mostly process startup) on both the
  synthetic fixture and the real-repo validation clone, with identical entity/edge/bug_rate
  numbers before and after — confirming persistence changed nothing about extraction/resolution
  correctness, only when it runs.
- Round-trip tests (`internal/store/store_test.go`) passed on the first implementation attempt,
  which is a reasonable signal the fixed-offset layout was designed correctly up front rather
  than debugged into correctness.
- A dangling edge (target entity outside the snapshot's scope) is dropped during `Write`, not
  written as a corrupt index entry — verified by `TestWrite_DanglingEdgeDropped`.

**Negative**:
- No staleness detection. If source files change after `ctx index` ran, `find`/`related`/`stats`
  silently serve the stale snapshot — there is no mtime or content-hash check yet. This is
  explicitly Phase 3 scope (the watcher, incremental indexing, content-hash-based re-anchoring
  per docs/adr/0003) and is a known gap until then, not a hidden one.
- No cross-invocation caching beyond the snapshot file itself — every `find`/`related`/`stats`
  call still re-opens and re-reads the whole file (no daemon holding it resident). Cheap at
  today's scale; revisit once Phase 3's daemon exists.
- `internal/search` (FTS5/qualified-name lookup) is still unbuilt; `Find` does a linear scan of
  `snap.All()` by exact bare-name match. Fine at current scale (tens of entities); will not scale
  to a large real repo without an index.

## Alternatives considered

- **Real mmap now** — rejected for this increment: adds platform-specific code
  (`mmap_unix.go`/`mmap_windows.go`, matching what Grafel needed) for a win that doesn't matter
  until Phase 3's daemon exists and snapshots are large. The chosen format defers this cheaply.
- **SQLite for the graph itself** — rejected again, consistent with ADR-0003 and
  docs/research/04: SQL is not a graph traversal primitive, and the win over a purpose-built CSR
  layout is negative for hop-by-hop traversal. SQLite's role remains what ADR-0003 already
  scoped it for — projects, decisions, ledger, FTS5 — none of which exist as features yet.
