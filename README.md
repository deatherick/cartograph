# Cartograph

Local Agent Context Manager + Code Intelligence Engine.

A daemon that keeps a deterministic structural map of your codebase and compiles the
**minimum useful context** for a task, under an explicit token budget — for AI coding agents
and humans alike.

Status: early scaffolding (Phase 0b). Not yet functional.

- `docs/research/` — discovery notes on Grafel (MIT, studied as reference, not copied)
- `docs/adr/` — architecture decision records for this project
- `docs/benchmarks/` — frozen `ctxbench` baseline results per phase, for comparing across phases
- `docs/requirements/` — user requirements captured ahead of the phase that will implement them
- `cmd/ctxbench/` — token-economy benchmark harness (the metric the project is measured against)

See the project plan for the full phased roadmap.

## Running the benchmark

```bash
make bench       # synthetic fixture, vendored in this repo
make bench-real  # real external repo, auto-cloned to ~/code/_ref, never vendored
```

See `docs/benchmarks/README.md` for how to read and reproduce the frozen results.
