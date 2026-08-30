# ADR-0021: Similarity/Duplicate Engine (Phase 5) — V0

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: the master plan's own Phase 5 section (the funnel diagram and evaluation taxonomy
  this ADR implements a scoped subset of), ADR-0007 (`relevanceFloorRatio`'s "first value that
  clears the bar" methodology, reused here), ADR-0017 (persisted quality stats — the same "measure
  honestly, disclose the gap" discipline)

## Context

The master plan named a Duplicate/Similarity Engine as its own phase, designed as a funnel (never
all-pairs comparison — O(N²) is infeasible past a few thousand entities): exact fingerprint → LSH
candidate generation → structural (bounded AST tree-edit distance) → behavioral (CALLS/USES sets)
→ semantic (embeddings, deferred to Phase 8). Scores always fully decomposed, never one opaque
number; never prescriptive ("evidence, a human decides"); a labeled ≥120-pair evaluation dataset
targeting precision ≥0.85 / recall ≥0.75. The user picked this item explicitly after a weighted
review of the remaining large backlog items (C#/Python extractors, true incremental indexing —
already done, ADR-0020 — this, and Phase 9).

## Decision: a real, working V0 — narrower in three specific, disclosed ways

`internal/similar` implements the funnel exactly as designed, with three deliberate scope
reductions from the full master-plan ask, each stated here rather than silently absorbed:

1. **No L2 bounded AST tree-edit distance.** Token-shingle Jaccard (via MinHash+LSH, see below)
   stands in as the structural signal instead. Real tree-edit distance is a legitimate, more
   precise future upgrade — not attempted here given the effort it represents on its own.
2. **The tokenizer does not normalize renamed identifiers to a common placeholder** (a real
   code-clone-detection technique). Two structurally identical functions with different local
   variable names score lower than they ideally would as a result — see tokenize.go's doc for
   the exact reasoning and the eval's own measured effect.
3. **The evaluation set is honestly much smaller than the master plan's ≥120-pair target**: 8 real
   functions, 24 labeled pairs (`fixtures/similarity-eval/`, `internal/similar/eval_test.go`) —
   built and hand-verified within this session's scope, not claimed to meet the larger target.

**The funnel, concretely:**
- **L1 — exact fingerprint**: entities sharing an identical `Anchor.ContentHash` (already computed
  per entity by every extractor) are an exact match, `Overall=1.0`, no further scoring needed.
  Note this only catches a declaration that is byte-identical INCLUDING its own name — a function
  renamed-but-otherwise-identical is NOT an L1 exact match; it is exactly what the MinHash path
  below catches (see "A real bug this exposed" below for how this was actually verified, not
  assumed).
- **Candidate generation — MinHash + LSH** (`minhash.go`): each Function/Method entity's source
  (read via its `Anchor`, tokenized and stripped of comments/normalized literals — `tokenize.go`)
  becomes a set of k=5-token shingles, hashed into a 64-value MinHash signature (64 independent
  FNV-1a hashes, deterministically seeded — not Go's randomized `maphash`, since reproducible
  signatures matter for testability), banded into 16 LSH buckets. Two entities sharing any bucket
  become a scored candidate — turning "compare every pair" into "compare only entities that
  hashed together somewhere," the whole point of a funnel.
- **Scoring**: `Structural` is the MinHash-estimated Jaccard (fraction of matching signature
  positions — a standard, unbiased estimator). `Behavioral` is the exact Jaccard of each entity's
  outgoing-edge fingerprint (`(EdgeKind, target bare name)` pairs from its `FanOut` — independent
  of which exact `EntityID` a call resolved to, so a helper renamed elsewhere doesn't break an
  otherwise-real behavioral match). `Overall` blends them (0.6/0.4, see "first value that clears
  the bar" below) — UNLESS neither entity has any resolved outgoing edges at all, in which case
  `Overall` is structural alone (see the bug below for why).
- **Never prescriptive**: `Pair` always carries every component score, never just `Overall`.
  `Decisions` (`decisions.go`) persists a human's disposition (`ignore`, `intentional`,
  `same-pattern`, `should-share-abstraction`, `false-positive`) per repo (JSON file under the same
  `~/.cartograph/<repo>-<hash>/` directory snapshots and session ledgers already use) — a decided
  pair never resurfaces in a later `ctx duplicates`/`ctx similar` run.

**Interfaces**: `ctx similar <path> <name>`, `ctx duplicates <path> [--threshold N]`, `ctx decide
<path> <nameA> <nameB> <decision>` (CLI); `context_similar`, `context_duplicates`,
`context_decide` (MCP) — both thin adapters over `service.Similar/Duplicates/Decide`, the same
"one service layer, no duplicated logic" rule every other feature follows.

## A real bug this eval exposed — found and fixed, not assumed correct

The very first eval run found **zero** of five labeled true-duplicate pairs, including the exact
pair — a real bug, not a calibration nuance. Two functions differing only by name (identical body)
scored `Structural=0.86` (correctly high) but `Overall=0.6*0.86 + 0.4*0 = 0.52`, below the default
0.6 threshold, because neither had any resolved outgoing calls (both called only `Math.round`, a
builtin that produces no graph edge) — so `Behavioral` was computed as `jaccard(∅, ∅) = 0`, and the
weighted blend silently treated "no signal on either side" as "0% match," penalizing exactly the
small, self-contained functions real duplicate detection cares about most. Fixed
(`combinedScore`): when neither entity has any behavioral signal, `Overall` is `Structural` alone —
absent evidence is neutral, not a penalty. This is documented here, not just in a commit message,
because it is exactly the kind of measured, disclosed correction this project's whole discipline is
built around (ADR-0007, ADR-0017's `IDF` damping story, and now this).

## Measured result — honest, not inflated

On the 24-pair eval fixture (`internal/similar/eval_test.go`): **precision = 1.00 (3/3)**, **recall
= 0.50 (3/6)**. Zero false positives. Both "easy" categories (exact, renamed) are found completely.
Two categories are logged but not required to pass, both real funnel-design limits, not
implementation bugs:
- **"structural"** (same shape, different variable names AND a different inner operation) — harder
  for a non-identifier-normalizing tokenizer, exactly scope reduction #2 above.
- **"behavioral" as tested (pure case, near-zero token overlap)** — the master plan's own funnel
  runs behavioral scoring over L2's (structural) survivors, not as an independent candidate source;
  a pair with no structural overlap at all never becomes an LSH candidate in the first place, in
  this design or the plan's own diagram.

This does **not** meet the master plan's ≥0.85 precision / ≥0.75 recall bar as an unconditional
claim — it meets it on precision, falls short on recall, on a fixture 5x smaller than the target.
Reported exactly as measured; not rounded up, not hidden.

## What this is explicitly NOT

- **Not L2 (bounded AST tree-edit distance) or L4 (semantic/embeddings)** — see scope reduction #1
  above and the master plan's own Phase 8 framing for L4.
- **Not identifier-renaming-aware** — scope reduction #2.
- **Not validated against a ≥120-pair dataset** — scope reduction #3.
- **Not integrated into the Context Compiler's capsule** (the master plan's "EXISTING
  IMPLEMENTATIONS" section idea) or **the Web UI** (a Duplicates view, already named as blocked on
  this phase in `docs/MVP.md`) — both real, natural follow-ups, not attempted here to land a
  reviewable, working core first.
- **Not scoped beyond Function/Method entities** — comparing two Interfaces or Classes
  structurally is a different, unattempted problem.

## Verification

24 new tests across `internal/similar` (tokenizer, MinHash/LSH, the funnel end-to-end against real
indexed TS fixtures via `internal/index`, decision persistence) plus the labeled eval fixture
above; 4 new tests in `internal/service` (`Similar`/`Duplicates`/`Decide`, including that a decided
pair stops resurfacing); 5 new tests in `internal/mcpserver` (including the
`StructuredContentIsAlwaysAnObject` parity suite). Manually verified end-to-end via the real CLI
against a temp fixture: `ctx duplicates` found the pair, `ctx decide ... same-pattern` recorded it,
a second `ctx duplicates` correctly showed none. `go build/vet/test -race/lint` clean; CI green
(macOS + Linux + lint).
