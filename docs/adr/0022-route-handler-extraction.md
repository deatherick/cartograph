# ADR-0022: Extract Express-style route/event handlers as entities

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: `docs/benchmarks/2026-08-29-idf-seeding.md` (the real-repo recall gap this closes part
  of, and whose own "thin margin, watch it" warning this ADR's rejected ranker experiments confirm
  was accurate), ADR-0007 (`relevanceFloorRatio`'s "first value that clears the bar" methodology —
  the same discipline this ADR's REJECTED changes were held to), the schema-const extraction fix
  (`docs/research/edge-case-backlog.md` I11 — the same "validate against a real repo, find what's
  actually missing" methodology this ADR follows)

## Context

`ctxbench`'s real-repo fixture (`typescript-node-express-realworld-example-app`) had two tasks
(R07, R10) stuck at `recall@gold = 0.00` — not a ranking problem, a **structural extraction gap**:
their gold files (`articles-routes.ts`, `users-routes.ts`, `profiles-routes.ts`) produced **zero
entities at all**. Every route in that codebase is registered as
`router.get('/path', ...middlewares, function (req, res) {...})` — an anonymous callback with no
declared name — which this project's TS extractor had no query pattern for at all.

## Decision: a new, deliberately generic query pattern — extraction only, not ranking

`queries/entities.scm` gained one new pattern: `<obj>.<method>('<string>', ...middlewares,
<handler>)` where `<handler>` is the LAST argument and is a `function_expression` or
`arrow_function`. The handler becomes a real `KindFunction` entity
(`routeEntityFromMatch`, `internal/parser/ts/extractor.go`), registered in the extractor's FIRST
capture pass (not alongside `testEntityFromMatch`'s second pass) — its scope must already be
registered before `refsFromMatch` runs, so a call made *inside* the handler
(`Article.findOne(...)`) correctly attributes to it as `Ref.Src`, not left at module scope.

**Deliberately generic, not Express-specific**: no allowlist of receiver names (`router`, `app`)
or verb names (`get`, `post`) — any `obj.method('string', ..., handler)` call matches. This also
naturally covers `emitter.on('error', handler)` and similar registration-style APIs, the same
"don't maintain a per-framework name list" choice `methodassign`/`schemaconst` already made.

**Name synthesis**: since there is no declared identifier, the entity's `Name` is built from the
HTTP verb/event name + path/event string (e.g. `"GET /articles"`), further enriched
(`reqAccessedFields`) with every `req.<x>.<field>` access found in the handler's body (e.g.
`"GET / (limit, offset, tag)"` for a handler reading `req.query.limit`) — real business vocabulary
a task prompt is actually likely to use ("pagination limit"), embedded in the entity's own name
rather than relying on file-path matching (see "what was tried and rejected" below for why that
specific alternative was explicitly avoided).

## What was tried, measured, and REJECTED — and why this matters

Extraction alone raised the real-repo fixture's average recall@gold from 0.50 to only 0.62 — R10
now passes (0.00→0.67), **R07 still fails (stuck at 0.00)**. Debugging showed why: the new route
entity (`"GET / (author, favorited, id, limit, offset, tag)"`) DOES contain "limit" and scores
correctly via `matchScore`'s substring tier — but ranks outside the top-5 seeds, because several
model methods (`toJSONFor`, `toAuthJSON`, `toProfileJSONFor`) score even higher: the task prose
contains the word "to", which is *also* a substring of every one of those camelCase names.

Two ranker-side fixes were built and measured to close this specific gap:
1. A stopword list dropping purely functional words ("to", "the", "so", ...) from `tokenizeTask`.
2. A minimum-length-3 guard on `matchScore`'s substring-match tiers (a 1-2 character term is too
   short to mean anything as a substring match).

**Both were rejected.** Each fixed R07 (recall reaching 0.79-0.82 on the real-repo fixture) but
**regressed the synthetic `ts-basic` fixture's recall@gold from 0.85 to 0.81-0.84** — below its
exit criterion, a previously-passing benchmark that would start failing. Per this project's
explicit rule (never silently regress a previously-passing exit criterion to improve another
number), both changes were reverted in full; `internal/compile/compile.go` carries no changes from
this ADR. This is exactly the risk `docs/benchmarks/2026-08-29-idf-seeding.md` already flagged
about this fixture ("a thinner recall margin... should be watched, not forgotten, the next time
seeding is touched") — now confirmed accurate by two independent, measured attempts, not just a
theoretical worry.

**R07 remains open, honestly.** Fixing it without a real regression needs either a smarter
seeding signal than raw substring containment (e.g. word-boundary-aware matching, or a proper
BM25/FTS5 ranking function — already named as a deferred follow-up, ADR-0006) or a much larger,
more representative synthetic fixture whose thresholds have real margin instead of sitting exactly
at 0.85. Neither is a small change; both are legitimate future work, not attempted here.

## Net honest result

| Fixture | Recall before | Recall after (this ADR) | Exit criterion |
|---|---:|---:|---|
| Synthetic (`ts-basic`) | 0.85 | **0.85 (unchanged)** | PASS (unchanged) |
| Real repo (`realworld-ts`) | 0.50 | **0.62** | Still FAIL (target 0.85) — R07 open, R10 fixed |

Token reduction on the real-repo fixture stayed ~93% (unchanged in character, the new route
entities are small). This closes R10 for real, moves R07 partway (its gold entity now exists and
scores correctly, just doesn't reach the top-5 seeds), and — just as importantly — surfaces a real,
measured limit of the current substring-based seeding approach, now documented instead of
theoretical.

## What this is explicitly NOT

- **Not a ranker/seeding change.** `internal/compile` is untouched by this ADR — see the rejected
  experiments above.
- **Not Express-specific**, despite the motivating case — see the generic pattern description
  above.
- **Not a fix for R07.** Still open, honestly reported, with the two things that would actually
  fix it (word-boundary-aware matching, or a real ranking function) named as legitimate future
  work, not silently deferred.
- **Not extended to Go** — this is a TS/JS-only query pattern (Go has no equivalent anonymous-
  callback-registration idiom in its own std-library HTTP routing conventions this project has
  validated against yet).

## Verification

3 new tests in `internal/parser/ts` (route handler extraction — both function-expression and
arrow-function handler shapes, middleware-in-between skipped correctly; inner calls correctly
attribute `Ref.Src` to the handler entity; a non-route member call with no trailing function
argument does NOT spuriously extract). Measured against the real `typescript-node-express-
realworld-example-app` fixture via `ctxbench` before/after, for both the extraction change and
each rejected ranker experiment (numbers above). `go build/vet/test -race/lint` clean; CI green
(macOS + Linux + lint).
