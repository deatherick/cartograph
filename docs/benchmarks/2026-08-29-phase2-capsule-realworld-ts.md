# Capsule — Phase 2 — real repo `typescript-node-express-realworld-example-app`

- **Date**: 2026-08-29
- **Fixture**: same real-repo clone used since Phase 0b/1 (`~/code/_ref/realworld-ts`)
- **Task set**: `fixtures/tasks/realworld-ts.json` (same 12 tasks)
- **Command**: `./bin/ctxbench --capsule --budget 2500 --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json`
- **Compiler config**: identical to the synthetic-fixture run — no real-repo-specific tuning
  was applied (see docs/adr/0007's "Alternatives considered" for why not)

## Result

| Task | Capsule tokens | Items | recall@gold | precision@gold |
|---|---:|---:|---:|---:|
| R01 | 125 | 5 | 0.33 | 0.60 |
| R02 | 106 | 5 | 0.67 | 0.60 |
| R03 | 116 | 5 | 0.33 | 0.60 |
| R04 | 130 | 5 | 0.67 | 0.80 |
| R05 | 138 | 6 | 0.33 | 0.50 |
| R06 | 138 | 6 | 0.50 | 0.67 |
| R07 | 168 | 7 | **0.00** | 0.00 |
| R08 | 153 | 7 | 0.50 | 0.57 |
| R09 | 132 | 5 | 1.00 | 0.80 |
| R10 | 149 | 6 | **0.00** | 0.00 |
| R11 | 131 | 6 | 0.33 | 0.33 |
| R12 | 138 | 6 | 1.00 | 0.17 |

**Total capsule tokens: 1,624 · Average recall@gold: 0.47 · Average precision@gold: 0.47**

Compared against the Phase 0b baseline (`2026-08-29-phase0b-realworld-ts.md`): oracle 26,398 /
traced 34,006.

**Reduction vs traced baseline: 95.2% · recall@gold: 0.47**

**Phase 2 exit criterion (≥70% reduction AND recall@gold ≥0.85): FAIL** (reduction far exceeds
target; recall does not)

## Lectura — root cause, not a ranker defect

95.2% reduction is not a win here — it means the capsule is nearly empty, which trivially
minimizes tokens while missing most of what the tasks actually need. Two tasks (R07, R10) scored
**zero** recall: the seeder found nothing relevant at all.

The cause is upstream in Phase 1's extraction coverage on this repo's specific idioms, not the
Phase 2 ranker: this repo resolves only **9 edges total** (vs 54 on the synthetic fixture — see
`ctx index` measurements throughout Phase 1), because Mongoose model objects
(`const User = model('User', UserSchema)`) are never extracted as entities — only a `const`
declaration, outside the current `Kind` taxonomy. With so little indexed and so few edges to
expand along, the capsule is close to seed-matches-only regardless of how the ranker is tuned.

**Deliberately not chased by re-tuning the ranker** — see docs/adr/0007-context-compiler-vertical-slice.md's
"Alternatives considered." The fix is extracting object/schema-style `const` declarations as
entities (extending the `methodassign` pattern already built for this exact repo's `.methods.`
idiom) — a Phase 1 extraction-coverage task, tracked as a new backlog item, not attempted here.
