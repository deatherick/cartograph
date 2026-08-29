# 08 — Process architecture, handoff and the residuals loop

## A. The writer/reader handoff (ADR-0024) — the most elegant pattern in the repo

### Problem
The indexer writes and the query server reads. Coordinating the two usually brings locks,
notifications, cache invalidation, and an entire class of concurrency bugs.

### How Grafel solved it
It coordinates nothing. The entire notification protocol is **a new file**:

- The graph cache keeps lazy `mmap` handles in a concurrent LRU.
- On every `Get`, it `stat`s the file and compares `ModTime().UnixNano()` against the mtime
  captured when it was opened. If the engine wrote a newer `graph.fb`, it transparently reopens
  the mmap.
- Old handles stay **pinned until their readers finish**, and only then is `Munmap` called.
- Their exact phrase: *"a reader process needs no notification from the writer at all — a fresh
  atomic file is the entire handoff"*.

That let them split the daemon into two processes (serve / engine) cheaply, driven by a real
structural failure: **any engine panic took down the MCP connection**. An fbwriter panic, a
`grafel update` that only touches engine code, or a full reindex shared a process — and a fate —
with latency-sensitive queries.

### How we solve it
We adopt the full pattern from V0, even while starting as a single process:

- The snapshot is **always** written atomically (write to temp + `rename`).
- The reader side opens via `mmap`, revalidates by mtime, and reopens transparently.
- Old handles are pinned until their readers finish.

The cost of doing it this way from the start is nearly zero, and it buys two things: a reindex
never blocks a query, and splitting the daemon later is a deployment decision, not a redesign.
They arrived here after the incident; we start here.

**Corollary for our Phase 3:** a panic during indexing must never take down an agent's MCP
session. Extraction runs with recovery and fail-soft: if an index pass fails, the last good
snapshot is preserved instead of leaving the graph half-built. (They learned this with the 2 GiB
cliff: their fix was to abort cleanly while preserving the previous `graph.fb`.)

## B. The residuals repair loop (ADR-0015)

### The most useful data point in the whole discovery: how much residual is normal

Their measured bug-rate across real corpora:

| Corpus | Bug-rate |
|---|---|
| Ship-gate synthetic corpus | 12% |
| `django-realworld` | 7.83% |
| `client-fixture` group (3 repos) | 11.34% |

**Between 8% and 12% of cross-symbol references go unresolved**, after eighteen sprints of
per-language work. It's an honest calibration of what to expect from a mature static resolver,
and it needs to be stated in the documentation: they note as a negative consequence that
*"users see 12% on a fresh index and think the tool is broken"*.

They also document the cost of chasing that residual with rules: `internal/external/synth.go` is
**11,333 lines** and `internal/resolve/refs.go` is **3,196**. Each wave of per-framework
allowlists lowers the bug-rate a bit. It works for popular, stable stacks, and it does not scale
to the long tail.

### Their solution
Turn the residual into a **work queue for an agent**:
1. The indexer emits candidates with enough context to decide without rereading the repo
   (`from_entity`, `relation`, `original_stub`, `disposition_reason`, `candidates`,
   `context_window`).
2. The agent writes resolutions with `resolution` / `confidence` / `reasoning` / `source`.
3. An application pass merges them in **before** disposition classification, as overrides.

Safety rules worth copying wholesale:
- **Allowlist-based trust model, not blocklist**: the indexer enumerates *which* resolutions it
  accepts and rejects anything else with a logged reason.
- **Repairs never mutate the graph directly**: they mutate the resolver's table before
  classification, so the graph emitter remains the sole writer.
- **Every edge carries a `source`** — *"turns the graph from 'trust the binary' into 'audit the
  binary'"*.
- **Full reversibility**: deleting the repairs file and reindexing returns to pure static.
- **Determinism**: applied in `edge_id` order, so identical source + identical repairs → byte-
  identical output.

### How we solve it

This maps exactly onto our "learned relationships" (Phase 7), and improves it in one respect:

1. **We adopt all five safety rules as-is.** They're good and they're free.
2. **Centrality-based prioritization from day one.** They deferred it to a future phase 3, after
   noticing that a graph with 5,000 residuals isn't interactive with one LLM round-trip per edge.
   Since we already compute centrality at index time (note 04), ordering the queue by impact is
   free: repairing the 50 most central residuals is worth more than repairing 5,000 peripheral
   ones.
3. **The residual doesn't wait for an agent: it's shown in the capsule.** This is the real
   difference. When the compiler assembles context for a task and there's a residual in the
   impact radius, the capsule states it:

   ```
   RESIDUALS (unresolved, 2)
     ?  RestrictionApi.fetch → 3 candidates: E12, E31, E44
   ```

   The agent already working on that task has the context to disambiguate it, and its decision
   can be captured. The repair loop stops being a separate batch pass and becomes a byproduct of
   normal use. They have the loop and the capsule separately; joining them is our product.
4. **The bug-rate is a CI metric with a blocking regression gate**, and it's reported **separately**
   from legitimate residual (dynamic / external), so as not to repeat their problem of a user
   seeing 12% and thinking it's broken. The UI shows three distinct numbers: resolved,
   unresolvable by design, and our bug.

## C. What we do NOT copy

- **The V0 process split.** They arrived at it because of scale incidents we don't have. We adopt
  the handoff pattern that makes it cheap, and we split only if it hurts.
- **The 11,333 lines of external-package synthesis.** It's the long tail of frameworks chased by
  hand. Our bet is that the residuals loop plus in-capsule disambiguation cover that long tail
  without that code. If the bug-rate doesn't drop below ~12% for V0's three languages with this
  approach, the bet failed and it needs reconsidering.
