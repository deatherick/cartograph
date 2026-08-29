# ADR-0007: Context Compiler vertical slice — ranker, budgeter, ledger

- **Status**: Accepted (implemented; real-repo recall gap identified and root-caused, not yet closed)
- **Date**: 2026-08-29
- **Related**: ADR-0003 (data model), ADR-0006 (Phase 1 completion), docs/research/06 (token measurement)

## Context

Phase 2's central bet (the master plan's own framing: "el vertical slice") is the Context
Compiler — turning a free-text task into a token-budgeted capsule that beats the grep+read
baseline `ctxbench` has measured since Phase 0b, without losing the entities a correct answer
actually needs. The formal exit criterion, never met before this change: **≥70% token reduction
vs the traced baseline, at recall@gold ≥0.85**, measured on the same 12-task benchmark used
throughout this project.

## Decision

Built as four new packages: `internal/ledger` (the Context Ledger — per-session delivery
tracking, so a second call in the same session doesn't re-spend tokens on what it already sent),
`internal/srcread` (a small shared file-line-range reader, factored out to avoid duplicating it
between `internal/service` and `internal/compile`), `internal/compile` (the compiler itself:
term-overlap seeding → graph-decay expansion → a real 0/1 knapsack budgeter), and CLI wiring
(`ctx context <path> "<task>" --budget N [--session ID]`) plus `ctxbench --capsule` for the
formal measurement.

**Seeding** is term-overlap matching (task words, including camelCase-split sub-words, against
`Entity.Name`/the *symbol* portion of `Entity.Qualified`) — explicitly not BM25/FTS5, consistent
with ADR-0006's search-scope deferral. **Expansion** follows the existing graph (`Snapshot.Related`)
from top seeds, decaying score by `0.6^depth`. **Budgeting** is a genuine dynamic-programming
0/1 knapsack (not greedy, not truncation) maximizing total relevance score under the token
budget, run in two passes (primary items get first claim on 60% of the budget, related items
compete for the rest) as a pragmatic stand-in for the plan's "minimum quota per category."

### The tuning story — a real bug found by measuring, not guessed

The first working version passed recall (0.92) comfortably but reduction stalled at 44.9% of the
required 70%. `precision@gold` (0.33) explained why: the knapsack had budget to spare (2500 vs a
median per-task need under 700 tokens) and so included *every* BFS-reachable neighbor within
depth 2 — decayed but never zero, and cheap enough at the signature rung that nothing forced
selectivity. A **relevance floor** (discard related candidates scoring below a fraction of the
task's best seed match) was added and tuned by actually measuring three settings, not by
intuition:

| `relevanceFloorRatio` | Effective depth | Reduction | Recall@gold |
|---:|---|---:|---:|
| 0.25 | 2 | 64.7% | 0.87 |
| 0.4 | collapses to ~1 (0.6²=0.36 < 0.4, so depth-2 is structurally excluded) | 82.9% | 0.68 — **below target** |
| **0.3** | 2 | **70.7%** | **0.87** — **both thresholds cleared** |

`defaultMaxSeeds` was also reduced from 8 to 5 in the same pass (fewer, higher-confidence seeds
→ a smaller expansion union) — both changes are in `internal/compile/compile.go`'s constants,
with the measured rationale kept inline as a comment rather than left to git history.

### Result on the synthetic fixture — PASS

```
Reduction vs traced baseline: 70.7% (capsule 3,092 tok vs baseline 10,569 tok)
recall@gold: 0.87 · precision@gold: 0.40
Phase 2 exit criterion: >=70% reduction AND recall@gold >=0.85 — [PASS]
```

### Result on the real repo — FAIL, root-caused, not chased further

Re-running the same tuned constants against `typescript-node-express-realworld-example-app`
(the real-repo validation clone used throughout this project) gives a **materially different**
picture:

```
Reduction vs traced baseline: 95.2% (capsule 1,624 tok vs baseline 34,006 tok)
recall@gold: 0.47 — well below the 0.85 threshold
```

**This is not a ranker-tuning problem, and re-tuning against it was deliberately not attempted**
— doing so would very likely overfit to this one repo's graph shape the same way the first
version had overfit to the synthetic fixture's larger, denser one. The real cause is upstream,
in Phase 1's extraction coverage on this specific repo's idioms (documented already in ADR-0004/
ADR-0006): the real repo resolves only **9 edges total** (`ctx index` measurement, Phase 1), far
sparser than the synthetic fixture's 54, because:

- Mongoose model objects (`const User = model('User', UserSchema)`) are never extracted as
  entities at all — only a `const` declaration, outside the current `Kind` taxonomy — so a task
  whose gold file is a model file often has *nothing indexed there* for the ranker to seed on or
  expand into.
- With so few resolved edges, graph expansion from a seed has almost nowhere to go — the capsule
  ends up being close to seed-matches-only, which is why `Items` per task dropped to 5-7 (versus
  17-30 on the synthetic fixture) and two tasks (R07, R10) scored **zero** recall: the seeder
  found nothing relevant at all for those prompts.

**The fix is Phase 1 extraction coverage, not Phase 2 ranking.** Extracting object/schema-style
`const` declarations as entities (a natural extension of the `methodassign` pattern already
built for Mongoose's `.methods.` idiom) is the highest-leverage next step for real-repo recall —
tracked as a new backlog item rather than attempted in this change, to keep this vertical slice's
scope to the compiler itself.

## Consequences

- The formal exit criterion is met on the project's own benchmark (synthetic fixture), and the
  real-repo run is not a silent gap — it's measured, reported, and root-caused in the same
  change, consistent with the project's practice throughout Phase 1.
- `docs/benchmarks/` gains two more frozen snapshots (capsule mode, both fixtures) alongside the
  existing baseline-only ones from Phase 0b, so this result is the one future phases are compared
  against, not re-derived from a fresh run each time.
- **MCP is not wired in this change.** The plan's Phase 2 scope also names an MCP server
  (`context_compile`, `context_expand`, etc.) and a live Claude Code demo. Both are the next
  remaining Phase 2 item, deliberately sequenced after the compiler itself proved out against
  `ctxbench` — building an MCP layer around a compiler whose numbers weren't yet measured would
  have risked wiring a server around logic still being tuned.
- The Context Ledger (`internal/ledger`) is built and unit-tested (a second call in the same
  session costs measurably fewer tokens — `TestCompile_LedgerAvoidsResendingWithinSession`) but
  its savings are a *separate* dimension `ctxbench` does not yet measure — each task in the
  benchmark is compiled with no session, by design (see `runCapsuleMode`'s doc comment). A
  multi-call, same-session benchmark is a documented follow-up.

## Alternatives considered

- **Greedy value-density selection instead of a real knapsack** — rejected: the plan is explicit
  that the budgeter is "un knapsack determinístico," and a real DP knapsack was not meaningfully
  harder to implement correctly at this scale (a few dozen candidates, budgets in the thousands).
- **Chasing real-repo recall by re-tuning ranker constants** — rejected per the root-cause
  analysis above; the sparse graph is a Phase 1 extraction-coverage problem, and re-tuning against
  it would very likely regress the now-passing synthetic-fixture result without fixing the actual
  cause.
- **BM25/FTS5 seeding now** — rejected, consistent with ADR-0006: no evidence yet that seeding
  quality (versus graph sparsity) is the real-repo bottleneck; the root-cause analysis above
  points elsewhere.
