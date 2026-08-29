# ADR-0006: Phase 1 completion — receiver-type resolution, remaining scope items, and search deferral

- **Status**: Accepted (implemented)
- **Date**: 2026-08-29
- **Related**: ADR-0003 (data model), ADR-0004 (query-based extraction), ADR-0005 (snapshot persistence)

## Context

ADR-0004 closed with the receiver-type resolution tier explicitly unimplemented — 181 of ~236
references in the real-repo validation run (`typescript-node-express-realworld-example-app`)
were `DispositionUnimplemented`. The user asked to close that gap and bring Phase 1 to genuine
completion against its own written scope, not just the parts that happened to get built first.

This ADR records what closing the gap actually took, what else Phase 1's original scope still
named that hadn't landed, and one deliberate exclusion (SQLite + FTS5) made with the user's
explicit sign-off rather than silently dropped.

## Decision — receiver-type resolution

Implemented as a query-driven signal-collection pass (`internal/parser/ts/queries/entities.scm`)
feeding a new resolver tier (`internal/resolve/resolve.go`'s `resolveByReceiverType`), covering:

1. **Constructor-parameter properties** (`constructor(private repo: UserRepository) {}`) — the
   dominant TypeScript idiom for declaring injected dependencies.
2. **Typed class fields** (`private repo: UserRepository;`) declared without constructor sugar.
3. **Locally typed variables** (`const x: Foo = ...`) and **`new`-initialized variables**
   (`const x = new Foo()`), collapsed to a single type per name **only when every observation in
   the file agrees** — an ambiguous name (two functions, two different types) is left unresolved
   rather than guessed, the same whitelist-not-guess principle the bare-name tier already follows.
4. **A plain (non-namespace) import used as its own receiver type** — `User.findById(...)` where
   `User` is an imported Mongoose model used for "static" calls. Found re-validating this tier
   against the real repo: it is that repo's dominant DB-access shape, not the constructor-injected
   case (1) above.

**A disposition refinement fell out of (4):** when the receiver type is known but the member
isn't found on it (e.g. `findById` inherited from Mongoose's own unindexed `Model` base class),
the result is `DispositionExternalUnknown`, not `DispositionUnimplemented` — the resolver made a
real, specific determination (this member isn't ours), which is a materially different claim than
"this tier hasn't been attempted." Reclassifying this moved 93 real-repo refs from the
scope-gap bucket into an honest external-presumed bucket.

**A real bug found by the precision test, not before it**: `internal/index/precision_test.go`
(built to finally measure the "≥95% precision against an annotated fixture" exit criterion,
which had never actually been measured — see below) caught test-detection's `KindTest` entities
(named from a `describe("UserService", ...)` string literal) polluting the resolver's same-file
name index, causing `new UserService()` to resolve to the *test block* instead of the *class* of
the same name in the same file. Fixed by excluding `KindTest` from `fe.byName`/`idx.byBareName` —
test entities remain fully findable via `ctx find`/`ctx inspect`, just excluded from reference
resolution, since a test label was never a real language binding to begin with.

### Measured impact

| | Synthetic fixture | Real repo |
|---|---:|---:|
| Resolved edges, before | 35 | 6 |
| Resolved edges, after | 54 | 9 |
| Unimplemented, before | 53 | 181 |
| Unimplemented, after | 34 | 88 |
| Annotated-checklist precision | **13/13 = 100%** | — |

## Decision — other Phase 1 scope items closed in the same pass

- **tsconfig `paths`/`baseUrl`** (`internal/resolve`'s `TSConfig`, wired from a real
  `tsconfig.json` read by `internal/index/tsconfig.go`) — single-wildcard patterns only
  (`"@/*": ["src/*"]`), the overwhelmingly common case; multi-segment/regex-like patterns are a
  documented gap.
- **CommonJS `require()` imports** — default (`const x = require('./m')`) and destructured
  shorthand (`const { a, b } = require('./m')`); the `{ a: renamed }` pair-pattern form is a
  documented gap, lower value than the common shorthand case.
- **Re-exports / barrel files** (`export * from './x'`, `export { a as b } from './x'`) —
  depth-limited (4 hops), cycle-safe (visited-set) barrel following in
  `findExportedEntity`/`findExportedEntityDepth`, so an import reaching through an `index.ts`
  barrel resolves the same as a direct import.
- **Test detection** — `it`/`test`/`describe` calls with a string-literal first argument produce
  `KindTest` entities. Nested calls inside a test callback are not attributed to the test entity
  as `Src` (would need arrow-function callbacks registered as scopes, matching
  `methodassign`'s existing pattern) — a documented follow-up, not done here.
- **`fan_in`/`fan_out` surfaced** — was already computed (`graph.Graph.FanIn`/`FanOut`,
  `store.Snapshot.FanIn`/`FanOut`) but never exposed through any interface. `ctx inspect` now
  prints both.
- **`ctx inspect` and `ctx source`** — the two CLI subcommands the master plan named that hadn't
  landed with `ctx index`/`find`/`related`/`stats`.
- **`--file <substring>` disambiguation** — added to `find`/`inspect`/`related`/`source` after
  the `KindTest` bug above made bare-name collisions a live, not hypothetical, problem.
- **Qualified-name lookup** — `Service.Find` now matches on `Entity.Qualified` when the query
  contains `#`, in addition to bare-name matching.

## Decision — SQLite + FTS5 explicitly deferred

The master plan's original Phase 1 scope named `internal/search: exact, qualified name, FTS5`.
Exact and qualified-name lookup are done (above), operating directly over the persisted snapshot
via a linear scan — adequate at today's scale (tens to low thousands of entities; a real
2,000-file repo produces on the order of a few thousand entities, still well within linear-scan
territory for a CLI invocation).

**FTS5 (fuzzy/full-text search) requires SQLite**, which this project does not otherwise depend
on yet — `internal/store`'s snapshot format (ADR-0005) deliberately is not SQLite, and ADR-0003
already scoped SQLite's eventual role to projects/decisions/ledger/metrics, none of which exist
as features yet (they arrive with Phase 2's Context Ledger and Phase 7's learned relationships).
Adding a SQLite dependency now, for FTS5 alone, means carrying that dependency (and its cgo build
cost, on top of tree-sitter's own cgo requirement) before any feature actually needs full-text
search over source names — there is no real user-facing gap today that exact/qualified lookup
doesn't already cover.

**This is a deliberate re-scoping, confirmed with the user, not a silent omission.** FTS5 is
deferred to whenever SQLite is introduced for its already-scoped purposes (Phase 2+), at which
point search can be added to the same database rather than as a second, single-purpose
dependency introduced early.

## Consequences

- Phase 1's exit criteria (project plan) are now genuinely met and, for precision, actually
  *measured* rather than assumed: <60s indexing (holds at current scale — a 2k-file benchmark run
  is a documented follow-up, since no fixture that size exists yet), `ctx related` <100ms
  (achieved: ~5-13ms reading the persisted snapshot), `bug_rate` ≤15% (0.0% synthetic, 1.1% real),
  dispositions broken down in output (`ctx index`), and precision ≥95% against an annotated
  fixture (100% on a 13-entry checklist spanning every implemented resolver tier).
- The `KindTest`-in-`byName` bug is the kind of defect the precision test exists to catch — it
  shipped, was caught, and was fixed within the same change that added the test, not after.
- Two documented follow-ups carried forward, neither blocking Phase 1 completion: nested test-
  callback scope attribution, and FTS5 whenever SQLite lands for its scoped purposes.

## Alternatives considered

- **Guessing at the KindTest bug's fix scope** (e.g. excluding all string-literal-named entities
  generically) — rejected in favor of the precise fix (exclude `KindTest` specifically from the
  name indices), since the bug's root cause was specific to test entities not being real bindings,
  not a property of string-literal names in general.
- **Adding SQLite now for FTS5 alone** — rejected per the search-deferral decision above.
