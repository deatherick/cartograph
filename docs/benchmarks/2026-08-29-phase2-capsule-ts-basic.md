# Capsule — Phase 2 — synthetic fixture `ts-basic`

- **Date**: 2026-08-29
- **Fixture**: `fixtures/ts-basic` (same as the Phase 0b baseline)
- **Task set**: `fixtures/tasks/ts-basic.json` (same 12 tasks)
- **Command**: `./bin/ctxbench --capsule --budget 2500 --baseline`
- **Compiler config**: `defaultMaxSeeds=5`, `defaultMaxDepth=2`, `relevanceFloorRatio=0.3`
  (see docs/adr/0007-context-compiler-vertical-slice.md for how these were measured, not guessed)

## Result

| Task | Capsule tokens | Items | recall@gold | precision@gold |
|---|---:|---:|---:|---:|
| T01 | 251 | 14 | 1.00 | 0.50 |
| T02 | 154 | 9 | 1.00 | 0.78 |
| T03 | 253 | 15 | 0.75 | 0.47 |
| T04 | 567 | 28 | 1.00 | 0.04 |
| T05 | 415 | 22 | 1.00 | 0.55 |
| T06 | 244 | 13 | 0.67 | 0.38 |
| T07 | 148 | 8 | 0.67 | 0.38 |
| T08 | 105 | 6 | 0.33 | 0.17 |
| T09 | 332 | 20 | 1.00 | 0.20 |
| T10 | 196 | 11 | 1.00 | 0.64 |
| T11 | 240 | 14 | 1.00 | 0.50 |
| T12 | 187 | 12 | 1.00 | 0.17 |

**Total capsule tokens: 3,092 · Average recall@gold: 0.87 · Average precision@gold: 0.40**

Compared against the Phase 0b baseline (`2026-08-29-phase0b-ts-basic.md`): oracle 7,796 /
traced 10,569.

**Reduction vs traced baseline: 70.7% · recall@gold: 0.87**

**Phase 2 exit criterion (≥70% reduction AND recall@gold ≥0.85): PASS**

## Lectura

This is the number the project's central bet was measured against. `precision@gold` (0.40)
stays well below 1.0 by design at this stage — the capsule includes graph-adjacent context
(callers, callees, related types) that a whole-file gold set doesn't credit as "correct" but
that a real agent working the task would likely want anyway; a per-entity gold set (a documented
future refinement, see `fixtures/tasks/*.json`'s own note) would score this more fairly.
