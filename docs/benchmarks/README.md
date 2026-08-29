# Benchmarks

Each file here is the frozen output of `ctxbench --baseline` at a point in the project,
for comparing phases against each other on the same fixture and task set. It is regenerated
(new file, dated) each time a phase introduces the Context Compiler or significantly changes
the indexing pipeline — an old one is never overwritten, so the historical series is preserved.

| File | Phase | Fixture | What it measures |
|---|---|---|---|
| [2026-08-29-phase0b-ts-basic.md](2026-08-29-phase0b-ts-basic.md) | 0b | synthetic (`fixtures/ts-basic`) | Initial baseline, own control repo |
| [2026-08-29-phase0b-realworld-ts.md](2026-08-29-phase0b-realworld-ts.md) | 0b | real (`typescript-node-express-realworld-example-app`, cloned into `~/code/_ref/realworld-ts`) | Initial baseline against real code, not synthetic |

## How to reproduce

```bash
# synthetic fixture (vendored in the repo)
./bin/ctxbench --baseline

# real repo (clone before running; not vendored)
git clone --depth 1 \
  https://github.com/skopekreep/typescript-node-express-realworld-example-app \
  ~/code/_ref/realworld-ts
./bin/ctxbench --baseline --fixtures-root ~/code/_ref --tasks fixtures/tasks/realworld-ts.json
```
