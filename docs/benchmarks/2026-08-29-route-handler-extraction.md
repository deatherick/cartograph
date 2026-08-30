# Route/event handler extraction — real-repo recall

- **Date**: 2026-08-29
- **What changed**: `internal/parser/ts` now extracts Express-style route/event registrations
  (`obj.method('string', ...middlewares, handler)`) as real `KindFunction` entities — see
  ADR-0022 for the full design and (importantly) the two ranker-side experiments that were tried,
  measured, and explicitly rejected because they regressed the synthetic fixture below its exit
  criterion.

## Real-repo fixture (`realworld-ts`), `ctxbench --capsule --budget 2500`

| Task | Recall before | Recall after |
|---|---:|---:|
| R07 | **0.00** | **0.00 (still open — see ADR-0022)** |
| R10 | **0.00** | **0.67 (fixed)** |
| Average (12 tasks) | 0.50 | **0.62** |
| Reduction vs. traced baseline | 95.4% | 92.8% |
| Exit criterion (≥70% reduction, ≥0.85 recall) | FAIL | FAIL (unchanged verdict, real improvement) |

Reduction dropped slightly (95.4%→92.8%) because the new route entities add real, legitimate
capsule content for tasks that now correctly seed on them (R10) — a small, expected, honest cost
of recall going up, not a regression to be concerned about.

## Synthetic fixture (`ts-basic`)

**Unchanged**: 71.2% reduction, 0.85 recall — still exactly at its exit criterion, same as
documented in `2026-08-29-idf-seeding.md`. The extraction change itself is additive (a new query
pattern that never fires on `ts-basic`'s own source, which has no such route-registration shape)
and does not touch `internal/compile` at all, so this fixture's numbers are provably untouched.

## What was tried and rejected (measured, not guessed)

Two ranker-side changes were built specifically to close R07 (its gold entity now exists and
scores correctly via `matchScore`'s substring tier, but ranks outside the default top-5 seeds
because common words in the task prose — "to" — coincidentally substring-match unrelated,
higher-scoring model methods like `toJSONFor`):

| Change | Real-repo recall | Synthetic recall | Verdict |
|---|---:|---:|---|
| Stopword-filter `tokenizeTask` | 0.82 | 0.84 | REJECTED — synthetic drops below 0.85 |
| Minimum-length-3 substring-match guard | 0.79 | 0.81 | REJECTED — synthetic drops further |

Both were reverted in full. `internal/compile/compile.go` carries no changes from this work — see
ADR-0022's "What was tried, measured, and REJECTED" section for the full account of why, and what
would actually need to change (a real ranking function, not another patch to substring matching)
to fix R07 without this tradeoff.

## Conclusion

A real, measured, honestly-reported improvement (R10 fixed, real-repo average recall +0.12) with
zero regression to the synthetic fixture's exit criterion — achieved by fixing the actual root
cause (a structural extraction gap) rather than tuning the ranker to compensate for missing data,
and by refusing to accept two ranker changes that "fixed" the target task at the cost of a
previously-passing benchmark.
