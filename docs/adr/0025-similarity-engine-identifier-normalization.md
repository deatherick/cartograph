# ADR-0025: Similarity Engine — identifier normalization closes the "renamed-shape" gap

- **Status**: Accepted
- **Date**: 2026-09-05
- **Related**: ADR-0021 (Similarity/Duplicate Engine V0 — the three scope reductions this ADR
  partially closes one of), ADR-0007/ADR-0017/ADR-0022 (the project's "first value that clears
  the bar," measured-not-assumed methodology this ADR follows again)

## Context

With Go, C#, and Python all shipped (ADR-0010, ADR-0023, ADR-0024), the user picked deepening the
Similarity/Duplicate Engine over closing C#'s open Context Compiler recall gap — ADR-0021's own
three explicitly disclosed scope reductions were the concrete menu:

1. No L2 bounded AST tree-edit distance (token-shingle Jaccard stands in).
2. **The tokenizer does not normalize renamed identifiers to a common placeholder** — a real
   code-clone-detection technique (`tokenize.go`'s own doc named this explicitly).
3. The labeled evaluation set (24 pairs) is far smaller than the master plan's ≥120-pair target.

This ADR closes #2. #1 and #3 remain open, named again at the end.

## Decision: "blind renaming," scoped to a shared keyword list across all four languages

`internal/similar/tokenize.go` gains `normalizeIdentifiers(tokens []string) []string`, applied
AFTER `tokenize()` (which stays a faithful lexer — its own existing tests assert the raw token
stream is unchanged) and BEFORE `shingleHashes()`. Every identifier-looking token that is not in
`structuralKeywords` (a single shared list — control flow, declarations, modifiers, imports, and
value words like `true`/`self`/`this`, spanning TS/JS, Go, C#, and Python, since `tokenize.go` has
always been a generic, not-per-language lexer) is replaced with a placeholder (`ID1`, `ID2`, ...)
numbered by first appearance WITHIN that one entity's own token stream — the standard "blind
renaming" technique real clone-detection tools (NiCad, SourcererCC) use. The same identifier reused
later in the same function keeps the same placeholder, so a real reuse pattern (the loop variable
feeding the accumulator) still produces a shingle match; only the actual chosen name stops
mattering.

Deliberately not scoped to only DECLARED names (vs. names merely referenced/called) — a heuristic
tokenizer, not a real parser, has no declaration/reference distinction to draw on in the first
place; every language's own extractor already accepts this same "heuristic, not a lexer" tradeoff
(`tokenize.go`'s pre-existing doc).

## What was measured, not assumed

On the same 24-pair labeled fixture (`internal/similar/eval_test.go`, unchanged):

| Metric | Before (ADR-0021) | After (this ADR) |
|---|---:|---:|
| Precision | 1.00 (3/3) | **1.00 (5/5)** |
| Recall | 0.50 (3/6) | **0.83 (5/6)** |

The "structural" category (`computeSum`/`computeTotal`/`computeAverageWeight` — same shape,
different variable names, one pair also with a genuinely different inner operation) now passes
completely: both labeled pairs are found, and precision stayed exactly 1.0 — zero new false
positives on the fixture's 18 labeled negative pairs. The remaining miss is "behavioral" (pure
case: near-zero structural token overlap, only the call graph matches) — a true funnel-design
limit already documented in ADR-0021, not something this change targets (the master plan's own
funnel runs behavioral scoring over structural survivors, not as an independent candidate source).

**A real regression this measurement caught before it shipped**: the first working version of this
change turned the eval fixture's own `getX`/`getY` (two trivially short, deliberately near-identical
one-line functions, `return 1;`, meant to be filtered out by `minBodyTokens` before ever being
scored) into a false positive. Both functions' 12-token bodies differ only by their own name and one
type-annotation identifier (`getX`/`getY`, `number`) — after normalization, BOTH become the exact
same 12-token stream, since neither the function's own name nor `number` (not in
`structuralKeywords`) survives normalization. `minBodyTokens` (the trivial-entity floor) was raised
from 12 to 15 to restore the intended behavior — re-measured at 15/18/20/24 to confirm 15 is the
first value that clears the bar (precision/recall identical at every value tried, so the smallest
sufficient one was kept, the same "first value that clears the bar" discipline ADR-0007/ADR-0017
established).

## Verification

7 new tests in `internal/similar/tokenize_test.go`: renamed-variable streams becoming identical
after normalization, keywords staying literal, the same identifier reused keeping the same
placeholder (not collapsing to one blanket placeholder), a genuinely different operation still
producing a different stream (normalization must not erase a REAL structural difference, only a
naming one), and `looksLikeIdentifier`'s own edge cases. Every pre-existing `tokenize()` test still
passes unchanged (the raw lexer's contract is untouched). The eval fixture's `mustFind` set was
tightened to also require "structural" now that it measurably passes.

Spot-checked live against this project's own real, self-hosted source (`ctx duplicates
~/code/cartograph`, 947 entities, 115 undecided pairs, ~70ms): correctly surfaces a genuine,
previously-invisible real duplication — `anchorFrom`/`contentHash`, near-identical helper functions
copy-pasted across all four language extractors (`internal/parser/{golang,ts,csharp,python}`),
exact matches (`Overall=1.00`) since their bodies are byte-identical apart from the language name in
comments. `go build/vet/test -race/lint` all clean.

## What this is explicitly NOT

- **Not L2 (bounded AST tree-edit distance).** Scope reduction #1 remains open — a legitimate,
  more precise future upgrade, not attempted here.
- **Not a larger labeled evaluation set.** Still 24 pairs, not the master plan's ≥120-pair target
  — scope reduction #3 remains open.
- **Not per-language keyword-aware.** One shared `structuralKeywords` list across TS/Go/C#/Python,
  not four separate ones — a real, disclosed scoping choice; a keyword unique to one language and
  absent from the shared list would be normalized away (rare, not observed to cause a problem on
  either eval fixture or the live self-hosting spot-check above).
- **Not integrated into the Context Compiler's capsule or the Web UI's Duplicates view** — both
  named as open in ADR-0021, still open.
