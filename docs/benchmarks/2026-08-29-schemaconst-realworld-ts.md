# Capsule — post-schema-const-extraction — real repo `typescript-node-express-realworld-example-app`

- **Date**: 2026-08-29
- **Fixture**: same real-repo clone as every prior run (`~/code/_ref/realworld-ts`)
- **Task set**: `fixtures/tasks/realworld-ts.json` (same 12 tasks)
- **Command**: `./bin/ctxbench --capsule --budget 2500 --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json`
- **What changed since `2026-08-29-phase2-capsule-realworld-ts.md`**: `internal/parser/ts` now
  extracts object/schema-style `const` declarations (`const User = model('User', UserSchema)`)
  as `KindClass` entities — edge-case-backlog.md's I11, closed this session, gated to module scope
  only (`isModuleScope`).

## What actually changed, measured

- **Resolved edges**: 9 → 14 (`ctx index ~/code/_ref/realworld-ts`) — a real, verified structural
  improvement. `User`, `Article`, and every other Mongoose model in this repo now resolve as real
  entities (`ctx find ~/code/_ref/realworld-ts User` → a real `Class` hit, previously nothing).
- **recall@gold / precision@gold on this 12-task set: unchanged (0.47 / 0.58 avg)** — reported
  honestly, not rounded up. The extraction fix is real and independently verified
  (`TestExtract_SchemaStyleConst`, plus the live `ctx find`/`ctx index` check above); it simply
  does not move THIS benchmark's aggregate number, because neither zero-recall task (R07, R10)
  has anything to do with model entities:

  | Task | Gold files | Root cause |
  |---|---|---|
  | R07 | `src/routes/articles-routes.ts` | Pagination-limit validation — a route-file/request-handling concern |
  | R10 | `src/routes/{articles,users,profiles}-routes.ts` | Auth-payload trust across routes — also route-file, not model |

  The original I11 finding ("with so little indexed... graph expansion has almost nowhere to go")
  was accurate about the STRUCTURAL problem (9 resolved edges is genuinely sparse) but this run
  shows the aggregate recall@gold regression on this specific 12-task set was not, in fact,
  dominated by the model-const gap — R07/R10's actual blockers are elsewhere (route-handler
  extraction/seeding, not investigated in this session).

- **Two individual tasks moved, canceling out in the average**: R02 dropped 0.67 → 0.33, R11 rose
  0.33 → 0.67. Adding more entities changed the seeder's ranking dynamics for both — a real,
  non-monotonic side effect of extraction changes, worth knowing about even though it nets to
  zero here. Not chased further this session (a ranking-quality investigation, not an extraction
  bug).

## What this means for the backlog

`edge-case-backlog.md`'s I11 is marked closed (the extraction gap itself is fixed and tested), but
the underlying "why does this real repo's recall stay at 0.47" question is NOT closed — it needs
its own investigation into route-file extraction/seeding, tracked as a new, separate item rather
than assumed solved by this fix.
