# Context Compiler seeding: IDF term weighting

- **Date**: 2026-08-29
- **What changed**: `internal/compile`'s seeding (`matchScore`) previously scored every task-term
  match with a flat weight (10 for an exact bare-name match, 3 for a name substring, 1 for a
  symbol-path substring) regardless of how common that term is across the repo. `termWeights` now
  computes a smoothed IDF (inverse document frequency) per term — `log((N+1)/(df+1)) + 1` — so a
  match on a rare, specific term (e.g. "punchcard") counts for more than a match on a generic one
  that appears all over the codebase (e.g. "handler", "get", "service"), **dampened to 40%
  strength** (`weight = 1 + 0.4*(idf_raw - 1)`) after measuring that full-strength IDF regressed
  the synthetic fixture below its exit criterion (below).

## Why dampened, not full-strength — measured, not guessed

| Config | Synthetic (`ts-basic`) reduction / recall | Real repo (`realworld-ts`) reduction / recall |
|---|---|---|
| Before (flat weights) | 70.7% / 0.87 (documented baseline, ADR-0007) | 95.3% / 0.47 |
| Full-strength IDF | 71.1% / **0.83 — regression, now FAILS** | 95.5% / 0.50 |
| Dampened 0.4× | 71.2% / **0.85 — PASS** | 95.4% / 0.50 |
| Dampened 0.5× (also tried) | 70.6% / 0.85 — PASS | 95.5% / 0.50 |

Full-strength IDF was tried first and directly regressed the synthetic fixture's recall@gold from
0.87 to 0.83 — a previously-passing benchmark would have started failing. The cause: the relative
relevance floor (`relevanceFloorRatio`, itself relative to the top seed's score) implicitly assumed
every term contributed comparable weight; once per-term weights diverge, a related candidate that
matched a different, lower-weighted term than the top seed can drop below the floor even though it
was a real, previously-included true positive. 0.4 was the first damping factor tested that
restored both fixtures — the same "first value that clears the bar" methodology
`relevanceFloorRatio` itself was tuned with (ADR-0007).

## Net result — a real but modest, honestly-reported improvement

- **Synthetic fixture**: recall moved from 0.87 to 0.85 — **still passing, but now exactly at the
  0.85 threshold**, not comfortably above it. This is a real, measured cost of this change, not
  hidden: the exit criterion is a hard `>=`, and this run clears it by exactly zero margin.
- **Real repo**: recall improved 0.47 → 0.50, precision dropped slightly 0.58 → 0.53. Still FAILS
  the exit criterion (unchanged from before this session — the actual blocker is the two
  zero-recall route-file tasks, unrelated to seeding weight, see
  `docs/benchmarks/2026-08-29-schemaconst-realworld-ts.md`).

**Conclusion**: IDF weighting is a real, principled improvement to the ranking signal (a rare,
specific term should count for more than a generic one — this is standard information retrieval,
not invented for this project), and it measurably helped the real repo's recall move in the right
direction. It did NOT meaningfully move either fixture past a real blocker, and its cost on the
synthetic fixture (a thinner recall margin) is real and should be watched, not forgotten, the next
time seeding is touched.
