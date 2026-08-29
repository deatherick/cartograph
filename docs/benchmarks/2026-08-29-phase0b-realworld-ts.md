# Baseline — Phase 0b — real repo `typescript-node-express-realworld-example-app`

- **Date**: 2026-08-29
- **Fixture**: TypeScript/Express/Mongoose implementation of the RealWorld spec
  (github.com/skopekreep/typescript-node-express-realworld-example-app), 20 TS files,
  1,174 lines. Cloned into `~/code/_ref/realworld-ts`, **not vendored** in this repo (same
  treatment as Grafel: read-only external reference).
- **Task set**: `fixtures/tasks/realworld-ts.json` (12 tasks, authored by hand against the
  real code read — not mechanically generated)
- **Command**: `./bin/ctxbench --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json`
- **Capsule tokens**: N/A — the Context Compiler doesn't exist yet (arrives in Phase 2)

## Result

| Task | Gold files | Oracle tokens | Traced tokens | char/4 ratio |
|---|---:|---:|---:|---:|
| R01 | 3 | 3204 | 3616 | 1.07 |
| R02 | 3 | 3493 | 4879 | 1.06 |
| R03 | 3 | 1555 | 2322 | 1.02 |
| R04 | 3 | 3139 | 3439 | 1.07 |
| R05 | 3 | 2610 | 4013 | 1.09 |
| R06 | 2 | 1220 | 2558 | 1.00 |
| R07 | 1 | 1946 | 2026 | 1.11 |
| R08 | 2 | 1682 | 1787 | 1.01 |
| R09 | 2 | 1193 | 1647 | 1.01 |
| R10 | 3 | 3120 | 4156 | 1.09 |
| R11 | 3 | 2688 | 2892 | 1.09 |
| R12 | 1 | 548 | 671 | 1.02 |

**Total oracle: 26,398 · Total traced: 34,006.**

## Finding that produced this run: real exclusions

Cloning a real repo (not a hand-written fixture) immediately exposed a gap: this repo's
working tree includes `conduit/`, a MongoDB data directory with binary `.wt`
pages — `ctxbench`'s naive `filepath.Walk` was reading them as text. `internal/exclude`
(dependency/build directories, lockfiles, and binary detection via NUL byte) was created
**before** running the baseline; without it, the number above would be inflated with
binary garbage and would mean nothing. See `internal/exclude/exclude_test.go` for the
regression case that locks in this finding.

## Reading

~3.4x more tokens than the synthetic fixture in the traced baseline (34,006 vs 10,569) with the
same number of tasks (12) — real code has more surface area per file and the tasks touch
larger modules. The traced/oracle ratio (1.29x) is lower than in the synthetic fixture
(1.36x): in real code, the hand-authored `grep_steps` tend to converge faster
because the names are more specific (`toProfileJSONFor`, `generateJWT`) than in a small
fixture where several tasks share generic vocabulary.

This number — not the synthetic fixture's — is the one that matters most as a thermometer
for the project: it measures against code nobody wrote with this benchmark in mind.
