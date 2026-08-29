# 04 — Graph storage and format

## Problem

Where the graph lives between invocations, and how much it costs to open it. In a tool whose
promise is "responds in under 100ms," the cost of opening the graph *is* the product.

## How Grafel solved it — a three-jump trajectory

**Jump 1 (ADR-0006): in memory + JSON on disk.** They explicitly rejected Neo4j (JVM),
Memgraph (Windows), SQLite with a property-graph schema (*"SQL isn't a graph language and
the storage gain over JSON is small at our scale"*), and DuckDB (analytical,
not point traversal). Central and correct reason: **the only consumer is a single process**, so
the main reason a database exists — shared multi-process access — doesn't apply.

**Jump 2 (ADR-0016): FlatBuffers.** JSON couldn't keep up. Measured numbers on a real fixture
of 11.34 MB / 100k+ entities:

| Operation | Time | Allocs |
|---|---:|---:|
| `json.Unmarshal(graph.json)` | ~132 ms | 50 MB / 640k allocs |
| `fbreader.Open` (mmap, zero-copy) | **~1.6 ms** | 9.9 MB / **8 allocs** |
| Hot lookup by ID (binary search) | **~40 ns** | — |

**~80× faster on cold open.** That 132ms cost was the floor of *every* MCP call.

And the honest disappointment: they expected 3× less disk size and got **1.15×**. The reason
is written in the ADR: *"the dominant cost is string content (entity IDs, qualified names,
paths) which FlatBuffers doesn't compress; only the JSON wrapper is eliminated."*

**Jump 3 (ADR-0026/0027): the 2 GiB cliff and mmap.** Go's FlatBuffers builder
panics with `cannot grow buffer beyond 2 gigabytes`. They designed sharding… and then
**deferred it** after measuring the real corpus:

> 287,091 entities · 1,335,957 relationships · ~2.1M LOC → `graph.fb` of **0.404 GiB**.
> Density ~325 bytes/relationship. Reaching 2 GiB would require ~6.6M relationships (~5× the corpus).

The original estimate overcounted per-record cost by 5× to 13×. They wrote the
full sharding design before measuring.

## The two weaknesses they left open

1. **Edges reference entities by string ID, not by vector index.** Consequence
   written in their own ADR: `IterateRelationshipsFromID` is **O(R)** — a linear scan of
   the whole relationships vector to find a node's neighbors. It's marked as
   "phase-2 should add a parallel vector sorted by from_id."
2. **String IDs are the dominant size driver**, and they repeat in every edge.

Both have the same cause: string-based identity in the on-disk representation.

## How we solve it

The original plan said "SQLite + snapshot with `encoding/gob`." The numbers above invalidate it:
`gob` has the same problem as JSON (O(N) decode, massive allocs). We correct course.

**Snapshot format: custom binary, mmap-able, with integer IDs and CSR adjacency.**

```
[header: magic, version, counts, offsets]
[string table]        interned strings, deduplicated, once
[entities]            fixed-size records; strings are uint32 offsets
[csr_offsets]         uint32[n_entities+1]      row index
[csr_targets]         uint32[n_edges]           neighbor, by entity INDEX
[csr_edge_meta]       kind/confidence/provenance per edge
[id_index]            sorted by EntityID → vector index, for binary search
```

What this buys over their design:
- **Neighbors in O(1)**, not O(R): `targets[offsets[i] : offsets[i+1]]`. It's exactly the
  weakness they documented and didn't close. For a Context Compiler that does graph
  propagation on every call, O(R) per hop is unacceptable.
- **Size**: string IDs appear **once** in the string table, not in every edge.
  It directly attacks the size driver they identified and couldn't attack.
- **No `flatc`, no codegen, no verbose bindings**, and no FlatBuffers builder's 2 GiB
  cliff — we write the buffer ourselves, streaming, without a builder that duplicates.
- Same zero-copy via mmap, same opening speed.

**SQLite yes, but for something else.** The graph doesn't live in SQLite — they're right
that SQL isn't a graph language. SQLite stores what actually is tabular and mutable: projects, repos,
files and their hashes, human decisions about duplicates, learned relationships, the session
ledger, and metrics. That's exactly where SQLite beats a binary file:
small, transactional, queryable writes. And FTS5 gives us text search for free.

**Sidecar for embeddings** (when they arrive, Phase 8): same as them, the vector doesn't go inline
in the entity but in a separate file referenced by content-hash. They already validated that
inlining bloats the main artifact.

**Attributes pre-computed at index time (ADR-0005, adopted).** Centrality, community, and
PageRank are computed at indexing time and stored as node attributes. The compiler reads
them as fields, never recomputes them. They're direct ranker terms, so this isn't an extra:
it's what makes ranking free at query time.

**The rule we take from their deferred ADR-0026:** measure before designing for scale.
The snapshot in a simple format must exist and be measured before optimizing anything.
Their real corpus (287k entities / 1.3M relationships / 2.1M LOC) is an excellent reference
for what "actually large" looks like: much smaller than one fears.
