# ADR-0023: C# extractor — Phase 3b, and an explicit anti-inference guard

- **Status**: Accepted (done, measured on a real external repo)
- **Date**: 2026-09-04
- **Related**: ADR-0010 (Go extractor — the template this follows), ADR-0011 (plug-and-play
  language architecture — the interface this plugs into unchanged), ADR-0002 (Grafel reuse
  protocol), `docs/research/edge-case-backlog.md` section D (C#)

## Context

`docs/MVP.md`'s "Immediate next steps" (item 19) named C#/Python as the next languages after Go,
at the user's explicit request, with an explicit methodology requirement: validate against a
real, non-trivial open-source repo, not synthetic fixtures alone — the same discipline that
found the schema-const gap (`edge-case-backlog.md` I11) and the route-handler gap (ADR-0022),
neither of which would have appeared in an invented fixture. The user chose **eShopOnWeb**
(`dotnet-architecture/eShopOnWeb`, Microsoft's own official ASP.NET Core reference app — layered
architecture, dependency injection, MediatR/CQRS, EF Core, 244 real `.cs` files across six
projects) over a RealWorld-style clone, for a more "enterprise-y" real-world shape.

**Mid-design, the user raised a direct concern** that reframed two design decisions already in
progress: *"siento que estamos dejando que las inferencias tomen el mando y asuman cosas con
pesos... me gustaría que nuestro motor de cartografía sea preciso y no dé espacio a duplicidad o
arruinar el código."* Two heuristics under active design at that moment were exactly what this
warned against:

1. Resolving a qualified call's receiver by assuming "starts with an uppercase letter" means "this
   identifier is a type/class reference" when no real type annotation exists.
2. Mapping a `using X.Y.Z;` directive to a directory by matching a partial/suffix of the namespace
   against a directory path, when no exact project configuration confirms it.

Both were paused and put to the user directly (`AskUserQuestion`) rather than assumed; the user
picked the conservative option for both, described below. This ADR is written under that explicit
constraint, not as an afterthought.

## Decision: same plug-and-play pattern as Go (ADR-0011), zero core changes to add the language

`internal/parser/csharp` (extractor + `queries/entities.scm`, tree-sitter-c-sharp v0.23.1),
`internal/resolve/lang_csharp.go` (the `LanguagePolicy`), one registration in
`internal/index/languages.go`. `TestArchitectureBoundary_CoreNeverBranchesOnLang` was extended to
also grep for `model.LangCSharp` in `resolve.go` — the same discipline enforced for Go, now
enforced for a third language, verified never to appear.

**Directory-scoped, like Go** — qualified names are `<repo-relative-dir>#<Name>`, not derived from
each file's own `namespace` declaration. This is a deliberate, honest approximation (documented in
`queries/entities.scm`'s package doc): the standard, tool-enforced C# convention is that a
project's folder structure mirrors its namespace (`dotnet new`/Visual Studio both default to this),
which held for every real file this extractor was validated against. A namespace that
intentionally diverges from its folder is a known gap, the same category as Go's own
directory-scoping approximation.

## Decision: the two guardrails the user asked for, applied exactly as chosen

**Guard 1 — never guess a receiver's type from capitalization.** A qualified call
(`SomeName.Method()`) only resolves when `SomeName`'s type is DETERMINISTICALLY known: a
constructor-injected field/property (a typed `field_declaration`/`property_declaration` — the
dominant ASP.NET Core DI pattern), a typed parameter, a typed local variable, or a `var x = new
Foo()` construction. When none of these apply, the ref is `DispositionUnimplemented` — the exact
same "best-effort, never guess" gap already documented for TypeScript/Go's own receiver-type
tiers, not a new kind of gap. No naming-convention fallback was built, full stop.

**Guard 2 — `using` directives resolve to a directory only on an EXACT namespace match.**
`internal/index/csproj.go` discovers every `.csproj` in the repo (a multi-project solution can
have several — eShopOnWeb has six) and each one's `<RootNamespace>` (falling back to the
`.csproj`'s own file name, the real MSBuild default — never a guess). `lang_csharp.go`'s
`resolveImportPath` requires an EXACT prefix match against a known project's root namespace before
mapping to a directory; a namespace that doesn't exactly match any known project's root is treated
as external, never approximated by a partial/suffix match. This was the literal choice put to the
user, with the tradeoff named explicitly (less recall on non-conventional layouts, zero risk of
mapping two different namespaces that merely share a trailing segment to the wrong directory).

**A third, related decision made independently but in the same spirit**: C#'s `base_list` syntax
cannot distinguish "extends a class" from "implements an interface" — both appear in the same
comma-separated list with no keyword marking which is which (unlike TypeScript's separate
`extends_clause`/`implements_clause`). The naming-convention fix ("starts with I") was never even
proposed to the user, having already internalized the guard from the two questions above: every
`base_list` entry is extracted as `RefExtends`, and `internal/resolve.reclassifyHeritageEdge` (new,
generic, language-agnostic core logic — checked by the architecture-boundary test like everything
else in `resolve.go`) corrects the edge to `RefImplements` only once the target actually RESOLVES
and its real `Kind` is known to be `KindInterface`. Deterministic, based on resolved data, never a
guess — the same discipline applied a third time without being asked again.

## What was found ONLY by validating against real code (not anticipated, not in a synthetic fixture)

- **A constructor's declared name always equals its class's own name** in C#. The first working
  version's `ctx find OrderController` reported a spurious `DispositionAmbiguous` between the class
  and its own constructor, because both shared the bare name `"OrderController"` in the resolver's
  index. Fixed by reporting a constructor's `Entity.Name` as `ctor` (dot-free, so
  `Entity.Qualified` reads as `Dir#Class.ctor`, not `Dir#Class..ctor`) — every class's constructors
  now share one bare name repo-wide, an acceptable trade since nobody looks up a constructor by
  bare name expecting a unique match, and `Qualified` (what callers actually use) stays unique per
  class.
- **`new Foo(...)` was not tracked as a reference at all** in the first working version. eShopOnWeb's
  dominant pattern is MediatR/CQRS: `_mediator.Send(new GetMyOrders(...))` — the real, internal
  dependency (which request type a controller dispatches) is entirely inside the `new` expression;
  the outer call (`_mediator.Send`) is correctly unresolvable (`IMediator` is an external MediatR
  interface). Fixed by emitting a `RefTypeUse` from `object_creation_expression`, mirroring
  TypeScript's `new.target` exactly. **Measured impact**: resolved edges on eShopOnWeb went from
  172 to 337 — nearly double — from this one fix alone.
- **Tree-sitter query field order must match the grammar's own declared child order**, or the query
  compiler rejects the pattern outright ("Impossible pattern"), even though field order carries no
  semantic meaning in tree-sitter's matching model itself. `property_declaration`'s actual node
  order is `type` before `name`; a query written `name: ... type: ...` (the order this ADR's author
  first wrote, matching how a human reads the declaration) fails to compile. Same for `parameter`
  (`type` before `name`) and `variable_declarator`, which additionally has **no `value:` field at
  all** — an initializer is an unnamed positional child after the `=` token, not a named field.
  Undocumented until now because Go/TS's own query patterns happened to already list fields in
  grammar order by coincidence. Recorded in `edge-case-backlog.md` D9 for the next language added.

## What was tried, considered, and REJECTED — per explicit user instruction, not a benchmark regression

Unlike ADR-0022 (where two ranker changes were built, measured, and reverted after a measured
regression), the two rejections here were never built beyond a design sketch — the user's answer
came before either was implemented, so there is no "before/after" benchmark table to show. Recorded
anyway, because the discipline is the same: an available, benchmark-favorable option was
deliberately not taken.

1. **Capitalization-based receiver-type guessing** (`SomeName.Method()` → assume `SomeName` is a
   type because it starts with a capital letter). Would have raised recall on eShopOnWeb (static
   factory/helper-class calls are common), at the cost of a real risk of wrong edges (any
   lowercase-first local variable shadowing a same-named-but-differently-cased convention break, or
   a locally-scoped variable that happens to start uppercase per an unusual style, would silently
   bind wrong). Rejected by the user.
2. **Partial/suffix namespace-to-directory matching** for `using` resolution. Would have raised
   recall for namespaces that don't exactly follow the folder convention, at the cost of a real risk
   of two unrelated namespaces sharing a trailing segment (e.g. two different projects each with an
   `Entities` folder) resolving to the wrong directory. Rejected by the user.

## Verification

**Self-contained**: `go build/vet/test -race/lint` all clean; 4 new extractor tests
(`internal/parser/csharp/extractor_test.go`) covering constructor-injected DI resolution,
`this.Method()` as an unqualified call, a local-function bare call correctly tagged `ScopeLocal`,
`using` alias resolution, and property-entity extraction — every one exercises a real design
decision above, not just entity-shape coverage.

**Real repo** (`~/code/_ref/eShopOnWeb`, 254 files, 777 entities):

| Metric | Value |
|---|---:|
| bug_rate | **0.0%** (0 of 1,506 dispositions) |
| resolved edges | 337 (up from 172 before the `new`-expression fix, D11) |
| external-known / external-unknown | 45 / 707 |
| ambiguous | 54 |
| unimplemented | 656 |

0.0% bug_rate on real, unmodified production C# code is comfortably under this project's ≤15%
first-language exit criterion (Go measured 0.1% on its own self-hosting run; this is the second
data point, not a fluke). The high `unimplemented`/`external-unknown` share is the honest,
documented cost of the two guardrails above (constructor-DI/typed-field calls resolve; framework
base-class calls, MediatR dispatch through an external interface, and non-conventional-layout
`using` targets correctly do not) — not chased further, per the guardrails' own design.

**`ctxbench`, synthetic fixture** (`fixtures/csharp-basic`, 4 tasks, `fixtures/tasks/csharp-basic.json`):
78.3% token reduction, **0.85 recall@gold** — passes the same exit criterion `fixtures/ts-basic`
passes, at exactly the same threshold.

**`ctxbench`, real repo** (`fixtures/tasks/eshoponweb.json`, 8 tasks): 46.4% reduction, 0.65
recall@gold — **below** the 70%/0.85 exit criterion, reported plainly, not rounded up. Spot-checked
one zero-recall task (C05): both gold entities (`CatalogFilterSpecification`, `CatalogViewModelService`)
are correctly extracted and independently findable via `ctx find` — this is a Context Compiler
seeding/ranking gap, not an extraction gap, the exact same category ADR-0022 already documented and
explicitly left open for TypeScript ("a real ranking function, not another patch"). Consistent with
every prior real-repo measurement in this project (TS started at 0.47-0.50 before two rounds of
targeted extraction fixes reached 0.62, still short of 0.85) — a language's first real-repo
measurement has never yet cleared this bar on the first pass, and this one does not either.

## What this is explicitly NOT

- **Not Python.** Phase 3c, tracked separately in `docs/MVP.md`.
- **Not a Context Compiler change.** `internal/compile` is untouched; the real-repo recall gap
  above is left open on purpose, matching ADR-0022's own scoping.
- **Not xUnit/NUnit/MSTest test detection.** C#'s test frameworks mark a test via an ATTRIBUTE
  (`[Fact]`, `[Test]`) on an ordinary method — this extractor does not parse attributes at all yet.
  Deliberately deferred (`edge-case-backlog.md` D7), not silently dropped: the same attribute-parsing
  work would also unlock ASP.NET routing attributes (`[HttpGet]`, `[Route]`), so it is one real
  follow-up, not two.
- **Not extension methods** (`this` as a first parameter, D5) or **partial classes spanning
  multiple files** — both real, both documented, neither chased in V0.
- **Not project-reference-aware.** A `.csproj`'s actual `<ProjectReference>` edges are not read;
  namespace resolution is exact-match-only across every `.csproj` this run's walk found, regardless
  of whether one project actually references another (D2).
