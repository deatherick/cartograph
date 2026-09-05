# ADR-0024: Python extractor — Phase 3c, the third and final "add a language" pass

- **Status**: Accepted (done, measured on a real external repo)
- **Date**: 2026-09-05
- **Related**: ADR-0010 (Go extractor), ADR-0023 (C# extractor — the immediately preceding
  language, whose guardrails this ADR inherits unchanged), ADR-0011 (plug-and-play language
  architecture), `docs/research/edge-case-backlog.md` section E (Python)

## Context

`docs/MVP.md`'s "Immediate next steps" (item 20) named Python as the last of the three
originally-planned languages (Go, C#, Python), at the user's explicit request to continue
straight from ADR-0023, with the same real-repo-validation requirement. The user chose
**gothinkster/django-realworld-example-app** — the official RealWorld/Conduit implementation in
Django + Django REST Framework — over a more modern FastAPI-style alternative, explicitly to
compare methodologies apples-to-apples against the TypeScript RealWorld clone already used for
Phase 1/2 validation (`realworld-ts.json`): same domain (articles, users, profiles, favorites,
comments), different language and framework style.

Built via `internal/parser/python` (tree-sitter-python v0.25.0) + `internal/resolve/lang_python.go`
+ one registration in `internal/index/languages.go` — the exact same plug-and-play pattern ADR-0010
and ADR-0023 established, zero changes to `resolve.go`'s core pipeline beyond the architecture
test's grep list (now also checking for `model.LangPython`).

## Decision: file-scoped, not directory/namespace-scoped — a genuine language difference, not a narrower approximation

Go (ADR-0010) and C# (ADR-0023) both scope qualified names by DIRECTORY, because both languages
have real implicit same-package visibility: a Go file in the same directory sees every sibling's
package-level declarations with no import; C#'s folder-mirrors-namespace convention gives the same
effect in practice. **Python has neither** — even two files in the same directory need an explicit
`from .other import Name` to use anything from each other. `pyPolicy.SameScopeFiles` returns `nil`,
exactly like TypeScript's, and `Entity.Qualified` is `file#Name` (or `file#Owner.Name` for a
method), never directory-based. This is the correct answer to a real difference in how the three
languages' own visibility rules work, not a weaker version of the Go/C# choice.

**Absolute imports resolve directly against the repo root** — a "flat layout" assumption (no
`src/` prefix, no `pyproject.toml`/`setup.py` `package_dir` remapping read), matching the real
validation target and the overwhelming majority of real, non-`src`-layout Python projects. A
`src`-layout repo is a documented, honest gap — **never guessed at by trying a `src/` prefix as a
fallback**, the same "exact match only, never a partial/suffix heuristic" discipline ADR-0023
established for C#'s `using` resolution, carried forward unchanged and unprompted this time (the
user's guardrail request during C#'s design is now this project's standing default for every
language added after it, not a one-off).

## Decision: `self`'s type is deterministic, not a naming guess — reusing, not repeating, the C# guardrail conversation

Python has no reserved `this`-like keyword the grammar itself marks (unlike C#'s literal `this`
token) — `self` is a first-parameter naming CONVENTION (PEP 8), universal in practice but not
syntax-enforced. Given the user's explicit, standing instruction from ADR-0023 (never resolve a
receiver's type from a naming convention alone), this needed a real answer, not an assumption:
**`self`'s type is deterministic because it names the method's own enclosing class — a structural
fact about where the code is written, not a guess about what an arbitrary identifier probably
means.** This is categorically different from the REJECTED C# heuristic (guessing that
`SomeName.Method()`'s `SomeName` is probably a class because it starts with a capital letter, which
guesses at a VALUE'S identity from its spelling) — `self`'s type is fixed by the surrounding syntax
tree, not inferred from the call site's text. The one honest risk (a method whose first parameter
is named something other than `self`, non-idiomatic and rare) resolves to `DispositionUnimplemented`
via the existing "unknown receiver type" gap, never guessed at either way.

This let `self.method()` and `self.field.method()` resolve as real internal edges — verified live
against the real repo: `self.get_queryset()` inside `ArticleViewSet.list` resolves as an actual
`CALLS` edge (confidence 0.85, provenance inferred), the exact receiver-type mechanism Go/C#'s own
`this`/receiver-variable handling already established, reached here through a textual convention
check instead of a language keyword.

## What was found ONLY by validating against real code

- **A def's OWN name node is the wrong place to start the "am I nested?" walk** — the first working
  version passed a function's NAME identifier to the nesting-scope check, whose immediate parent
  IS the function_definition node itself (name is a field OF that node) — every top-level function
  was misjudged as "nested inside itself" and silently dropped from `facts.Entities` (zero methods
  or functions extracted at all, caught immediately by the extractor's own unit tests before ever
  reaching real code — see `edge-case-backlog.md` E7). Fixed by walking from the def's own
  `function_definition` node instead, so the first parent visited is whatever actually CONTAINS it.
- **Qualified calls are the norm, not the exception**, unlike Go (every cross-file call is
  `pkg.Func()`) or even TypeScript. Since `from x import Name` binds a NAME (not a namespace) and
  most real logic lives in class methods, the dominant real-repo call shape is `self.x.y()`/
  `obj.method()`. Django's own idiom of writing a base class through a module-qualified name
  (`class Article(models.Model)`) meant class heritage needed a genuinely new pattern neither Go's
  nor C#'s extractor has at all: a QUALIFIED heritage reference (`extends scope=qualified
  name=models member=Model`), resolved through the same import-table mechanism a qualified call
  uses — not something either prior language's base-class syntax ever needs.
- **No extends-vs-implements ambiguity, the opposite of C#'s story (ADR-0023, D10)**: Python has no
  interface keyword, so every entry in a (possibly multi-inheritance) superclasses list — including
  Django REST Framework's real `mixins.CreateModelMixin, mixins.ListModelMixin,
  viewsets.GenericViewSet` — is genuinely a base class. `reclassifyHeritageEdge` (built for C#) is a
  correct, harmless no-op for Python: it never fires, because this extractor never targets an
  entity whose real `Kind` is `KindInterface` (a Kind this language's extractor never emits at all).

## What was tried, considered, and REJECTED

No new heuristic was proposed and rejected this time — unlike C# (ADR-0023), where two specific
designs were built partway before the user's intervention. The discipline the user established
during C#'s design was simply applied as the STANDING default from the start of this ADR: no
bare-name allowlist (unlike TypeScript's own optimistic-binding tier), no partial/suffix matching
for absolute imports, no naming-convention guess for anything except `self` (justified above as
categorically different, not an exception to the rule).

## Verification

**Self-contained**: `go build/vet/test -race/lint` all clean; 5 new extractor tests
(`internal/parser/python/extractor_test.go`) covering constructor-set-field resolution
(`self.repository = Repository()` then `self.repository.find_by_id()`), same-class `self.method()`
calls, nested-def suppression + `ScopeLocal` tagging, qualified and unqualified class heritage, a
plain aliased namespace import, and relative-import depth parsing — every one exercises a real
design decision above, not just entity-shape coverage.

**Real repo** (`~/code/_ref/django-realworld-example-app`, 44 files, 112 entities):

| Metric | Value |
|---|---:|
| bug_rate | **0.0%** (0 of 331 dispositions) |
| resolved edges | 21 |
| external-known / external-unknown | 88 / 162 |
| unimplemented | 60 |

0.0% bug_rate on real, unmodified production Python code — the third language in a row to clear
this project's ≤15% first-language exit criterion with zero measured bugs (Go: 0.1%, C#: 0.0%,
Python: 0.0%). The large `external-known`/`external-unknown`/`unimplemented` share is the honest,
expected cost of Django/DRF's framework-heavy style (most calls target base-class methods —
`viewsets.GenericViewSet`, `generics.ListAPIView` — this extractor correctly does not chase into
an unindexed library) plus Python's own dynamic-typing ceiling on receiver-type inference (E10) —
not chased further, per the same "measure honestly, document the gap" discipline as every prior
language.

**`ctxbench`, synthetic fixture** (`fixtures/python-basic`, 4 tasks, `fixtures/tasks/python-basic.json`):
77.2% token reduction, **1.00 recall@gold** — passes, with more margin than either C#'s (0.85) or
TypeScript's own (0.85) synthetic fixture.

**`ctxbench`, real repo** (`fixtures/tasks/django-realworld.json`, 7 tasks): **88.9% reduction,
0.86 recall@gold — PASSES the exit criterion on the real repo**, unlike every prior language's
first real-repo measurement (TypeScript needed two rounds of targeted extraction fixes, ADR-0006
and ADR-0022, to reach 0.62; C#'s own first pass, this same session, reached only 0.65). Reported
plainly, including the reason this one measurement is a genuine outlier rather than evidence the
Context Compiler itself improved: this repo's real business logic concentrates in a small number of
well-named, directly-called methods (Django's URL-routing/serializer conventions keep call chains
short and vocabulary-rich — task prompts and the actual method names share more real words than
eShopOnWeb's MediatR-mediated indirection did), not a change to `internal/compile`, which is
untouched by this ADR.

## What this is explicitly NOT

- **Not a Context Compiler change.** `internal/compile` is untouched.
- **Not re-export-aware.** A name only reachable through an `__init__.py` barrel (`from mypackage
  import Name` when `Name` is actually defined in `mypackage/submodule.py` and merely re-exported)
  does not resolve — TypeScript's own `findExportedEntity` chases up to 4 levels of barrel
  re-exports; Python's does not chase at all yet (`edge-case-backlog.md` E2).
- **Not chasing three-level unaliased namespace access** (`import x.y.z` then `x.y.z.member()`) —
  Python binds only the top segment (`x`); the two-level chain past that is a documented,
  unhandled gap, the same bounded scope C#'s `Guard.Against.Null` case already accepted.
- **Not resolving `self.field = some_parameter`** (assigning a constructor's own parameter directly
  to a field, arguably more idiomatic than this project's own synthetic fixture's
  re-instantiate-in-`__init__` style) — only `self.field = SomeClass(...)` gives a real type
  signal; a bare parameter assignment carries no type information Python's own syntax provides
  without a type hint, and even a present type hint isn't yet cross-referenced back into this path
  (`edge-case-backlog.md` E10).
- **Not decorator-aware** beyond "a decorated definition still extracts correctly" — a decorator
  that renames or replaces its target at runtime (`functools.wraps`-based wrappers, Django's
  `@receiver`) is undetectable via syntax alone, a permanent gap in the same category as Go's
  implicit interface satisfaction (J4).
- **Not xUnit/NUnit/MSTest-style attribute test detection** (that's C#'s open item, ADR-0023) —
  Python's own test convention (`test_`-prefixed function/method names, pytest/unittest's actual
  discovery rule) IS closed here, by naming convention, the same choice Go made for `TestXxx`.

This closes the three-language plan (Go, C#, Python) the user set at the start of Phase 3b/3c.
Per `docs/MVP.md`'s deferred list, what's next — Phase 9 (global install), deepening the
Similarity Engine, or something prioritized by real usage feedback — is the user's call.
