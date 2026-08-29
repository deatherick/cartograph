# ADR-0002: Grafel reuse protocol — study and reimplement

- **Status**: Accepted
- **Date**: 2026-08-29

## Context

Grafel (`cajasmota/grafel`, MIT) already resolved, across 9,063 files and 27 ADRs, a
substantial number of hard static-indexing problems: import resolution, bare names,
watcher behavior across different OSes, on-disk graph format, entity taxonomy. Copying would be
legal (MIT), but copying drags along its architecture and turns this project into "Grafel plus a
dedupe button," with no real differentiation.

## Decision

Grafel is cloned into `~/code/_ref/grafel` (outside this repo, never as a submodule or
dependency) and read thoroughly in a dedicated discovery phase (Phase 0a, completed). The
knowledge is extracted into `docs/research/` as our own notes (*problem → how they solved it
→ how we solve it → why it's different*) and as a backlog of edge cases that become
test fixtures.

We do **not** vendor or import any package from `github.com/cajasmota/grafel`. We do **not**
copy code verbatim — if some specific algorithm is closely adapted, it is marked inline
(`// adapted from grafel (MIT), see docs/research/...`) and credited in `NOTICE.md`; it must be
the documented exception, not the norm. We do **not** copy MCP tool names, on-disk file formats,
or the entity schema — this project's model revolves around the Context
Compiler, not graph navigation.

## Consequences

- We avoid months of work rediscovering edge cases that are already solved (see
  `docs/research/edge-case-backlog.md`, 80 cases).
- Every architectural decision in this project is justified on its own merits in its own ADR,
  never with "that's how Grafel does it" — this avoids inheriting assumptions without scrutiny.
- The cost is that discovery is an explicit phase that produces no code directly
  (Phase 0a, ≈1-2 sessions) before the visible project starts moving.

## Alternatives considered

- **Fork of Grafel**: faster to get started, but drags along the entire architecture and
  history; high risk of ending up as a shallow fork.
- **Total clean-room** (without even cloning, just the public README): maximum design freedom,
  but wastes already-validated knowledge (bare-name allowlists, per-OS descriptor cost
  model, graph format) that took real sprints to discover.
