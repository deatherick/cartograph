# ADR-0028: Word-boundary-aware seeding with light stemming (closes the ADR-0022/eShopOnWeb seeding gap for real)

- **Status**: Accepted
- **Date**: 2026-09-05
- **Related**: ADR-0022 (documented and REJECTED two prior ranker patches for this exact gap),
  ADR-0023's `## Update` (the eShopOnWeb recall correction, 0.65→0.40, that surfaced how much of
  the "known" recall numbers were themselves inflated by the underlying bug this ADR fixes),
  `docs/benchmarks/2026-08-29-idf-seeding.md` (IDF term weighting, the tier this ADR's fix sits
  alongside, untouched in its own logic)

## Context

`internal/compile`'s seeding (`matchScore`, `termWeights`) scored a task term against an entity's
name/symbol path via raw `strings.Contains` on the whole, lowercased, CONCATENATED string. This
lets a term match ACROSS a word boundary purely by character coincidence — e.g. task term
"articles" (from prose like "...the whole articles table") matches the class `ArticleSchema`,
because its lowercased, unsplit form `"articleschema"` literally contains `"articles"` as a run of
characters spanning `"article"` + the leading `"s"` of `"chema"`, an accident of English
pluralization bumping into an unrelated word. Two DIFFERENT prior attempts to fix this — a
stopword list, a minimum-substring-length guard (ADR-0022) — were both built, measured, and
explicitly REJECTED: each regressed the synthetic `ts-basic` fixture below its own 0.85 recall
exit criterion. Neither addressed the actual mechanism: both still matched via `strings.Contains`
on the concatenated string, just filtering WHICH substrings were allowed through — a long,
legitimate-looking-length false positive like `"articles"`/`"articleschema"` (8 characters, not a
stopword) would have passed either guard unfixed.

Investigating a separate discrepancy (ADR-0023's `## Update`) surfaced just how much this bug was
propping up the numbers this project had been reporting: fixing C#'s test detection (correctly
excluding `[Fact]`-attributed methods from seeding) dropped eShopOnWeb's own measured recall from
0.65 to 0.40 — not because test detection was wrong, but because test methods' own names
(`BasketAddItem.IncrementsQuantityOfItemIfPresent`) had been acting as accidental term-overlap
magnets AND graph-expansion bridges into the real gold classes, an artifact entirely unrelated to
genuine relevance. That investigation made clear the underlying seeding mechanism itself needed a
real fix, not another workaround — this ADR is that fix.

## Decision: tokenize both sides identically; stem morphological variants; don't call it done at "word boundaries" alone

**Word-boundary tokenization** (`tokensFor`, `entityTokens`): an entity's own name and symbol path
are now tokenized through the exact same word-splitting logic `tokenizeTask` already applies to
the task prompt (`wordRe` extracts alphanumeric runs, then `splitCamel` further splits each into
camelCase sub-words). A task term can only match a REAL word of the entity's name — exactly, via
an O(1) set lookup — never an accidental run of characters spanning a word boundary. Critically,
the SUB-WORD list used for the next tier (stemming) excludes the whole, unsplit word — including
it reintroduces the exact bug this ADR exists to fix (`"articleschema"`, the whole name, genuinely
DOES start with the stemmed form of `"articles"`, since `ArticleSchema` is literally
`Article`+`Schema` concatenated) — found via a failing test while writing this fix, not
anticipated up front.

**Light stemming** (`stem`, `stemMatch`): word-boundary correctness alone isn't sufficient — a task
written in prose ("placing an order", "money-formatting", "percentages") uses different
morphological forms than code ("placeOrder", "formatPercent", "percent"). A shallow suffix-stripper
(one common English inflectional ending: `-ies→y`, `-ing`, `-es`, `-ed`, `-s`) normalizes both sides
before a prefix comparison — not a real Porter stemmer (no vowel-doubling undo, no step cascade),
just enough to close the gap actually found. Scored at `stemWeight` (0.6×) relative to an exact
token match — a real but strictly weaker signal, never equally trusted (verified by a dedicated
test).

**`defaultMaxSeeds` raised from 5 to 7**: once the word-boundary fix removed the false-positive
matches that used to pad the top-5 with accidental hits, a task naming two genuinely real topics
(e.g. "welcome-email variant for **admins**" — both "email" and "admin" entities are legitimate)
could have more real candidates than 5 slots allow. 6 is the first value that restores every
synthetic fixture to its own exit criterion; 7 was kept over 6 because it measurably improves
EVERY real-repo fixture's recall with only a marginal, still-comfortably-passing token-reduction
cost — real-world recall is the actual goal the synthetic fixture stands in for, not a number to
just barely clear.

## What was verified, not assumed

| Fixture | Recall before this ADR | Recall after | Token reduction after | Exit criterion |
|---|---:|---:|---:|---|
| `ts-basic` (synthetic) | 0.85 | **0.85** | 75.4% | PASS (unchanged) |
| `csharp-basic` (synthetic) | 0.85 | **1.00** | 75.6% | PASS (improved) |
| `python-basic` (synthetic) | 1.00 | **1.00** | 72.1% | PASS (unchanged) |
| `realworld-ts` (real repo) | 0.67 (R07 stuck at 0.00, ADR-0022) | **0.78** (R07 now 1.00) | — | informational |
| `eShopOnWeb` (real repo) | 0.40 (ADR-0023's `## Update`) | **0.71** | — | informational |
| `django-realworld` (real repo) | 0.86 | **0.93** | — | informational |

**R07 (ADR-0022's own open item) now passes at 1.00** — the exact task the two rejected patches
targeted, closed by fixing the actual mechanism instead of its symptoms. No fixture regressed;
every one either held steady or improved, verified by re-running `ctxbench` (`--capsule` and
`--baseline --capsule`) against all six task sets, not assumed from the code change alone.

## What was tried and adjusted mid-implementation (not rejected — refined)

The first version of `tokensFor` included the whole, unsplit name in the slice `stemMatch` scans,
inherited from reusing `tokenizeTask`'s combined output directly. A new regression test
(`TestMatchScore_DoesNotMatchAcrossWordBoundary`) caught that this let the exact original bug back
in through the stemming tier: `stemMatch("articles", "articleschema")` returns true, since the
whole unsplit name is a real character-level prefix extension once both sides are stemmed. Fixed
by having `tokensFor` build its stemming-eligible slice from `splitCamel`'s own sub-word output
only, never the whole concatenated word (which remains eligible for EXACT matching only, via the
separate set). This is a refinement discovered through testing, not a rejected-and-reverted
experiment — no fixture measurement changed as a result (the bug the test caught had not yet
appeared in any of the six task sets' own real numbers), but it closes a real latent risk.

## What this is explicitly NOT

- **Not a real ranking function.** No BM25/FTS5, no embeddings, no centrality/PageRank term —
  ADR-0006's own deferral stands; this closes the SPECIFIC word-boundary/morphology gap ADR-0022
  identified, using the SAME term-overlap architecture, made structurally correct rather than
  replaced.
- **Not a stopword list or a length guard** — the two approaches already tried and rejected. This
  fix changes what "the entity's name" means to the matcher (a set of real words) rather than
  filtering which substrings of the old, looser definition are allowed through.
- **Not extended to `internal/similar`'s own tokenization** (`internal/similar/tokenize.go`) —
  a structurally different problem (structural/behavioral duplicate detection over code bodies,
  not task-to-entity seeding) using its own normalization; untouched, not evaluated here.
- **Not a fix for every remaining real-repo gap.** `eShopOnWeb`'s 0.71 and `realworld-ts`'s 0.78
  are real, measured improvements, not perfection — both fixtures still have individual tasks
  requiring either extraction improvements (e.g. `index.ts`'s own composition-root const bindings
  producing no entity at all — a distinct, separate gap) or genuine semantic/structural
  understanding (e.g. "where would a NEW route be wired in" has no existing call-graph edge to
  find, by the task's own premise) that term-overlap seeding, however word-boundary-correct,
  cannot solve. Reported honestly, not chased further in this ADR.

## Verification

5 new/updated tests in `internal/compile` (word-boundary correctness with a real
morphological-variant control case; stemming catches the exact gerund/plural cases found;
stemmed matches stay strictly weaker than exact ones). `go build/vet/test -race/lint` clean.
All six `ctxbench` task sets (3 synthetic, 3 real-repo) re-measured before and after, numbers
above traced to specific commits/measurements, not asserted from the diff alone.
