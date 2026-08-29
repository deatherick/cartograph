# ADR-0003: V0 data model — identity, dispositions, and storage

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: docs/research/04, 07, 09

## Context

The data model determines the correctness of the incremental index (does an edit invalidate too
much or too little?) and the cost of each Context Compiler query (how much does it cost to find
an entity's neighbors?). Grafel measured these costs in production; see docs/research/04 and 07
for the numbers.

## Decision

**Entity identity:** `EntityID = hash(repo, kind, qualified_name, disambiguator)`, with no
file or line. The `disambiguator` (arity plus normalized parameter types) is mandatory
for overloadable kinds, so overloads/`partial`/`@overload` don't collide by
construction. Location lives in a separate, mutable `Anchor`
(`file, byte_range, content_hash, commit`), re-anchored on reindex without invalidating identity.

**Dispositions:** any unresolved reference falls into a typed bucket
(`resolved | external-known | external-unknown | dynamic | ambiguous | bug-extractor |
bug-resolver`), never into a parseable string.
`bug_rate = (bug-extractor + bug-resolver) / total` is a CI metric with a blocking regression
gate.

**Storage:** the graph lives in a custom, mmap-able binary snapshot, with integer IDs and
CSR adjacency (O(1) neighbor lookup), written atomically (temp + rename). SQLite is used for
tabular, mutable data (projects, repos, human decisions, session ledger, FTS5), never for
the graph itself.

## Consequences

- Moving code between files or lines within the same namespace does not invalidate `EntityID` —
  Context Ledger handles survive normal edits.
- The snapshot avoids both the O(N) parse cost of JSON/gob and the O(R) neighbor weakness
  that Grafel left documented and unresolved in its own binary format.
- Cost: writing the snapshot and its CSR index is more code than serializing with
  `encoding/gob`; this is accepted because the cost is paid once in the writer and the
  savings are collected on every read.
- The `disambiguator` adds a responsibility to every extractor (normalizing parameter types
  consistently); without it, the overload-collision case (documented as a real bug
  in Grafel, issue #6161) reappears.

## Alternatives considered

- **`encoding/gob`** (original plan): discarded after measurement — same O(N) decode problem
  as JSON, with none of the advantages of mmap.
- **FlatBuffers**: discarded — it brings codegen (`flatc`), verbose bindings, the Go builder's
  2 GiB cliff, and doesn't solve the O(R) neighbor problem on its own (Grafel left it as
  unfinished future work).
- **Graph in SQLite with a property-graph schema**: discarded — SQL is not a graph
  language and the storage gain over a dedicated binary format is small at this scale.
- **ID including file+line** (as Grafel's ADR describes, even though its code doesn't
  actually implement it that way): discarded — invalidates cached references on any code
  movement.
