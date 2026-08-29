# Benchmarks

Each file here is the frozen output of `ctxbench --baseline` at a point in the project,
for comparing phases against each other on the same fixture and task set. It is regenerated
(new file, dated) each time a phase introduces the Context Compiler or significantly changes
the indexing pipeline — an old one is never overwritten, so the historical series is preserved.

| File | Phase | Fixture | What it measures |
|---|---|---|---|
| [2026-08-29-phase0b-ts-basic.md](2026-08-29-phase0b-ts-basic.md) | 0b | synthetic (`fixtures/ts-basic`) | Initial baseline, own control repo |
| [2026-08-29-phase0b-realworld-ts.md](2026-08-29-phase0b-realworld-ts.md) | 0b | real (`typescript-node-express-realworld-example-app`, cloned into `~/code/_ref/realworld-ts`) | Initial baseline against real code, not synthetic |
| [2026-08-29-phase2-capsule-ts-basic.md](2026-08-29-phase2-capsule-ts-basic.md) | 2 | synthetic (`fixtures/ts-basic`) | Context Compiler capsule vs the phase0b baseline — **PASS** (70.7% reduction, 0.87 recall@gold) |
| [2026-08-29-phase2-capsule-realworld-ts.md](2026-08-29-phase2-capsule-realworld-ts.md) | 2 | real (`realworld-ts`) | Same capsule config against real code — **FAIL** (0.47 recall@gold), root-caused to Phase 1 extraction coverage, not the ranker — see docs/adr/0007 |

## How to reproduce

```bash
# synthetic fixture (vendored in the repo)
./bin/ctxbench --baseline --capsule --budget 2500

# real repo (clone before running; not vendored)
git clone --depth 1 \
  https://github.com/skopekreep/typescript-node-express-realworld-example-app \
  ~/code/_ref/realworld-ts
./bin/ctxbench --baseline --capsule --budget 2500 --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json
```
