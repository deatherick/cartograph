# 07 — Entity identity, taxonomy and cross-repo

## A. Identity and namespacing (ADR-0009)

### Problem
A function `formatTime` exists in the mobile repo and in the frontend. They are distinct and
real, and cannot share identity in the graph.

### How Grafel solved it — two layers
1. **Index layer (per repo)**: each repo's `graph.json` stores `entity.id` as a **local** ID
   (hash of file + line + name within that repo). They are **not** prefixed at index time. Each
   entity also carries a `repo` attribute.
2. **Composition layer (MCP)**: when serving cross-repo views, IDs are prefixed as
   `<repo>::<localId>`. With a single-repo `repo_filter`, they go unprefixed.
3. **Cross-repo links file**: **always** uses prefixed IDs on both ends, so each entry is
   self-describing.

The intended consequence: `index` is repo-local, with no global state. A watcher can rebuild a
repo's graph without touching the others. And the deterministic hash gives stable IDs across
reindexes, so an agent that cached an ID can still use it.

They rejected: globally unique IDs at index time (pushes the problem onto the indexer and
prevents independent rebuilds), prefixing at index time (bigger files, renaming the repo changes
every ID), and matching by the tuple `(qualified_name, file, line)` (fragile under refactors).

### What the ADR says versus what the code does
ADR-0009 says the local ID is *"a hash of source file + line + name"*. **The code does not
include the line:**

```go
func EntityID(repo, kind, name, sourceFile string) string   // internal/graph/graph.go:259
```

The ADR is stale. Excluding the line is correct — otherwise moving a function twenty lines up
would change its ID — but it has a consequence they document in test #6161 and that must be
addressed head-on:

> *"every construct that declares a name twice in a file collides by construction"*: Java method
> overloads, C#/VB `partial` classes and methods, C++/TypeScript overload declarations,
> `@overload` / `@singledispatch` / `def` under `if TYPE_CHECKING` in Python, reopened Ruby
> classes.

The real bug: `convertExtractedRecords` added each record's entity unconditionally, so two records
with the same `EntityID` produced **two rows** in the document. Half the relations already had a
deduplication guard; entities did not.

Implementation detail worth copying: fields are hashed **separated by a NUL byte**, so that
`("ab","c")` and `("a","bc")` don't collide. And they note that `(from, to, kind)` is **not** a
unique key for an edge: some producers mint distinct IDs for edges that share that triple.

### How we solve it
We adopt both namespacing layers as-is: they're correct and they buy independent per-repo
rebuilds. And we go one step further on identity, **also stripping the file**:

```
EntityID  = hash(repo, kind, qualified_name, disambiguator)
Anchor    = { file, byte_range, content_hash, commit }   re-anchored on reindex
```

- **No line and no file** in identity: moving a function *between files* within the same
  namespace does not break references. With their scheme it does, because `sourceFile` enters
  the hash.
- **`disambiguator` is mandatory for overloadable kinds** (methods, functions): a hash of the
  arity and the normalized parameter types. This attacks the #6161 collision at the root instead
  of patching it with a downstream deduplication guard. For non-overloadable kinds it's empty.
- **A deduplication guard regardless**, at the document-conversion boundary, with a test that
  fails if two entities show up with the same ID. Belt and suspenders: the disambiguator covers
  the known case, the guard covers the one we didn't anticipate.
- **NUL separator** in the hash, copied as-is.
- **Edges carry their own ID**, they aren't identified by `(src, dst, kind)`.

This is the foundation that keeps Context Ledger handles (`E7`) valid across calls while the user
edits. With identity that includes the file, a refactor that moves code invalidates the handles
and the ledger stops working.

## B. Entity taxonomy (ADR-0003)

### How Grafel solved it
A namespaced `SCOPE.*` hierarchy with three conceptual layers: runtime (functions, classes),
framework (controllers, routes, queues, hooks, JSX) and infrastructure (IaC resources).
Kinds: `SCOPE.Operation`, `SCOPE.Component`, `SCOPE.Schema`, `SCOPE.Endpoint`, `SCOPE.Queue`,
`SCOPE.Datastore`, `SCOPE.InfraResource`, etc. **Closed** enum of edge kinds.

Good detail: **the MCP render layer strips the `SCOPE.` prefix** before showing it to the agent.
Internally `SCOPE.Operation`, for the agent `Operation`. Storage keeps the namespaced form so
future namespaces can coexist without collision.

Cost they admit: the extractors have to agree on where each construct falls (is a Rust trait a
`Schema` or a `Pattern`?), and the closed enum makes adding an edge kind a breaking change.

### How we solve it
Their taxonomy is optimized to **navigate** the graph. Ours is optimized to **compile context**,
so it deliberately differs:

- V0 uses concrete, familiar kinds (`Class`, `Interface`, `Function`, `Method`, `Property`)
  instead of three-layer abstractions. For a capsule that a human and an LLM will read, `Class`
  communicates more than `SCOPE.Component`, and there's no need to translate at render time.
- We take the render principle: **the internal vocabulary and the agent-facing vocabulary are
  separate decisions**. What gets serialized and what gets displayed need not match.
- Framework/infra kinds arrive in later phases, once extractors exist that justify each one. We
  don't declare 30 kinds in V0 to have 8 populated: a kind with no extractor is a promise the UI
  breaks.
- The edge enum is also closed, with schema versioning. There they're right.

## C. Cross-repo (ADR-0007)

Their cross-repo bridge rests on documentation and a links file with reduced confidence (0.7
cross-repo versus 0.95 intra-repo). We adopt the principle — **a cross-repo edge never carries the
same confidence as one resolved statically within the repo** — and the separate overlay file,
which lets a repo be rebuilt without recomputing the others' links.

What we add: confidence isn't just a stored number, it's a field the capsule **shows** and the
ranker penalizes by. An inferred cross-repo edge is worth less budget than a deterministic one.
