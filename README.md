# Cartograph

Local Agent Context Manager + Code Intelligence Engine.

A tool that keeps a deterministic structural map of a codebase and compiles the
**minimum useful context** for a task, under an explicit token budget — for AI coding agents
and humans alike.

**Status**: Phase 2 core done (TypeScript extraction, resolution, and the Context Compiler all
built and measured — see `docs/MVP.md`). MCP wiring is the one remaining piece before MVP.
Functional via CLI today; not yet daemonized, not yet multi-language.

**Start here**: [`docs/MVP.md`](docs/MVP.md) — what's done, what's left, what's deliberately
deferred, so work stays scoped to shipping v0.1 instead of open-ended iteration.

## Quick usage (CLI)

```bash
make build
./bin/ctx index <path-to-a-typescript-repo>
./bin/ctx find <path> <name>
./bin/ctx inspect <path> <name>
./bin/ctx related <path> <name> --depth 2
./bin/ctx source <path> <name>
./bin/ctx context <path> "<task description>" --budget 2500 [--session <id>]
```

`index` runs the full pipeline and persists a snapshot; every other command reads that snapshot
instead of re-indexing. Re-run `index` after the source changes (no staleness detection yet —
see `docs/MVP.md`'s known-issues list).

## Documentation map

- [`docs/MVP.md`](docs/MVP.md) — **consolidated status, MVP definition, known issues, roadmap**
- `docs/adr/` — architecture decision records, one per real design decision made
- `docs/research/` — discovery notes on Grafel (MIT, studied as reference, not copied) and the
  85-entry edge-case backlog derived from it
- `docs/benchmarks/` — frozen `ctxbench` results per phase, for comparing across phases
- `docs/requirements/` — user requirements captured ahead of the phase that will implement them

## Running the benchmark

```bash
make bench        # synthetic fixture, vendored in this repo — baseline only
make bench-real   # real external repo, auto-cloned to ~/code/_ref, never vendored

./bin/ctxbench --baseline --capsule --budget 2500          # + Context Compiler measurement
```

See `docs/benchmarks/README.md` for how to read and reproduce the frozen results.
