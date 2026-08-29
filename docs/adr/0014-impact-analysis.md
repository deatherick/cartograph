# ADR-0014: Impact analysis (Phase 4)

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: ADR-0013 (Web UI V0, whose Impact view this unblocks), docs/MVP.md

## Context

The Web UI's requirements capture (`docs/requirements/phase6-web-ui.md`) named an Impact view as
part of the original ask, explicitly blocked on Phase 4 not existing. Asked to continue toward
Phase 4/5, this ADR is Phase 4: `ctx impact <name>` (blast radius by entity) and `ctx impact
--git-diff [ref]` (blast radius of whatever a real `git diff` touched) — both named explicitly in
the master plan's Phase 4 section.

## Decision: `Upstream`, a new directional traversal — not a reuse of `Related`

`Related` (existing since Phase 1) walks BOTH directions from a start entity — reasonable for "what's
near this," wrong for impact analysis. If X changes, what matters is who **depends on** X (calls
it, extends it, implements it), transitively — never what X itself calls. `internal/store` gained
`Upstream(start, maxDepth)`, following only incoming edges (`FanIn`), with the same cycle-safe
visited-set BFS `Related` already uses. Its depth convention deliberately differs from `Related`'s:
`maxDepth<=0` means **unlimited** (the full transitive closure), not `Related`'s interactive
default of 2 — a real blast radius should not silently truncate at an arbitrary hop count.

## Decision: "tests that cover this entity" without a dedicated TESTS edge

`model.go` has defined `EdgeTests` since ADR-0003, but no extractor (TypeScript's or Go's) has ever
emitted it — nothing links a test entity to what it tests as a first-class edge. Rather than add
that now, impact analysis reuses what already exists: a `KindTest` entity that calls the changed
entity, directly or transitively, is already inside `Upstream`'s closure (a test IS a caller,
however indirect). `service.impactFor` just filters the closure by `Kind == KindTest` to get
`CoveringTests`. This is not a workaround — it is arguably the more honest signal: a test "covers"
an entity exactly when it actually exercises it through a real call path, which is precisely what
the resolved graph already encodes.

## Decision: git-diff parsing is a small, dependency-free regex, not a diff library

`internal/gitdiff` shells `git diff --unified=0 <ref>` (zero context lines — only actually-changed
lines matter here) and parses hunk headers (`@@ -old +new @@`) with one regex, mapping to changed
line RANGES on the new side only. The old side is never used: `internal/impact` maps against the
CURRENT snapshot, which reflects new-side line numbers. A pure-deletion hunk (new count explicitly
0) has no real new-side range; it is approximated as a single-line anchor at the deletion point —
documented as an approximation, not silently treated as exact. No diff/patch library was added:
git's hunk-header format is small and stable, and this project has no other use for a general
patch parser (the same "no dependency until proven needed" discipline every prior phase has
followed).

## What was built

- `internal/store.Upstream` — the directional traversal described above.
- `internal/gitdiff` — `Diff` (shells git) + `ParseChangedRanges` (parses hunk headers) +
  `Range.Overlaps` (the line-range test `internal/service.ImpactFromGitDiff` uses to map a changed
  range onto an entity's `Anchor`).
- `internal/service.Impact` (by entity name) and `ImpactFromGitDiff` (by diff), sharing one core
  (`impactFor`) so both entry points compute a blast radius identically — never two different
  answers for the same underlying question depending on how it was asked.
- `ctx impact <path> <name> [--depth N] [--file <substring>]` and `ctx impact <path> --git-diff
  [ref]` (ref optional, defaults to `HEAD` — working tree vs the last commit).
- MCP's `context_impact` tool, one handler covering both modes (name vs. `git_diff` argument) —
  the same "one tool, one clear job" pattern every other MCP tool here follows.
- The Web UI's Impact panel: direct callers, the full transitive list (each clickable, jumping to
  that entity), and covering tests — reusing `/api/impact`, a thin wrapper over the same
  `service.Impact`/`ImpactFromGitDiff` calls the CLI and MCP already use.

## Verification

- `internal/store`: two new `Upstream` tests — confirms it follows only incoming edges (unlike
  `Related`) and that unlimited depth actually reaches a 2-hop transitive caller a depth-1 call
  would miss.
- `internal/gitdiff`: parses a hand-written multi-file, multi-hunk-shape diff correctly (an
  added-lines hunk, an implicit-count-1 hunk, a pure-deletion hunk, a deleted file), plus a test
  that runs **real** `git diff` against a real temporary git repository end to end — not just
  parsing canned text.
- `internal/service`: a real call-chain fixture (`c` calls `b` calls `a`, with a test calling `c`)
  confirms `Impact("a")` finds `b` at depth 1 and `c` at depth 2, and that the test is surfaced as
  a covering test. A second real-git-repo test confirms `ImpactFromGitDiff` correctly maps an
  uncommitted single-function change to exactly that entity, and that entity's caller shows up in
  the aggregated impact set.
- `internal/httpserver` and `internal/mcpserver`: integration tests over the real HTTP/MCP
  transports (not mocked handlers), plus `context_impact` added to the existing
  `TestMCPServer_StructuredContentIsAlwaysAnObject` regression suite (ADR-0009's schema-validation
  bug class).
- Manually verified end-to-end against this project's own real, self-hosted source: `ctx impact
  <path> Run` shows real direct callers and a real 2-hop transitive chain; `ctx impact <path>
  --git-diff` against this session's own uncommitted work correctly listed every changed entity
  and their aggregated blast radius. `bug_rate` unaffected (still 0.1%) — this was additive
  capability, not a change to extraction/resolution.

## What's still missing

- **No historical batch validation** — the master plan's own Phase 4 exit criterion ("across 20
  historical commits, the proposed test set actually contains the tests that commit touched in
  ≥80% of cases") was not run. That needs a real repo with meaningful test coverage and history to
  validate against meaningfully; this project's own test suite is Go's `go test`, not yet indexed
  in a way `CoveringTests` would find real signal in without Go extraction maturing further on
  test-to-target linkage specifically. Tracked as a follow-up measurement, not silently skipped.
- **No cross-repo impact** — `Upstream` only ever traverses within one repo's snapshot, matching
  every other traversal in this project today.
