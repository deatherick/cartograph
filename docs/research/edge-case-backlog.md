# Edge case backlog → fixtures and tests

Each entry is a real case found in Grafel's code, tests, or ADRs. The `#NNNN`
reference is their issue number, kept as traceability for where the case came from.
**No entry is taken from their code; it's taken from the problem their code documents.**

`Phase` column: when the test should exist.

## A. Entity identity and deduplication

| # | Case | Origin | Phase |
|---|---|---|---|
| A1 | Two overloaded methods in the same file produce the same ID and two entity rows | #6161 | 1 |
| A2 | C# `partial` class declared in two files | #6161 | 3 |
| A3 | C# `partial` method (declaration + implementation) | #6161 | 3 |
| A4 | TypeScript overload declarations (`function f(a:string):void; function f(a:number):void;`) followed by the implementation | #6161 | 1 |
| A5 | Python `@overload` and `@singledispatch` | #6161 | 3 |
| A6 | Duplicate `def` under `if TYPE_CHECKING:` | #6161 | 3 |
| A7 | Hash with no separator: `("ab","c")` must not collide with `("a","bc")` | `graph.go:259` | 1 |
| A8 | Two edges with the same `(src, dst, kind)` triple must be able to coexist with different IDs | `graph.go:271` | 1 |
| A9 | Moving a function to another file in the same namespace must **not** change its `EntityID` | ADR-0009 vs code | 1 |
| A10 | Moving a function 20 lines within the same file doesn't change its ID nor invalidate upstream | ADR-0009 | 1 |

## B. Symbol resolution

| # | Case | Origin | Phase |
|---|---|---|---|
| B1 | A binding declared **inside a function body** must not take the name slot for the whole repository; an import of the same name in another file must not link to it | #6467 | 1 |
| B2 | A resolver "rejection" that then falls through to the global name index and **does** link — the rejection must terminate the ladder, not continue it | #6125 | 1 |
| B3 | Two `T.Do` methods in the same package + an unrelated `Do` function in another: the receiver tier must yield a disposition, not link to the unrelated function | #6125 | 1 |
| B4 | A scope-local stub (local variable, sort key) must **never** cross-resolve against the global name index | #3936 | 1 |
| B5 | Generic name (`get`, `format`, `run`, `value`) never produces a bare-name edge | ADR-0011 | 1 |
| B6 | A new language starts with an empty allowlist; CI rejects entries with no fixture that justifies them | ADR-0011 | 1 |
| B7 | OS-native paths (Windows backslash) against slash-form stubs must resolve the same way | #49 | 1 |
| B8 | An import placeholder that shadows an external symbol | #6369 | 1 |
| B9 | Module root alias (`@app/*` → `src/*`) | #4705 | 1 |
| B10 | Qualified call vs. bare name with the same name in the same file | #4554 | 1 |
| B11 | Filtering by the leaf name's kind: `Foo.bar` must not link to a `bar` of an incompatible kind | #6141 | 1 |
| B12 | Dynamic import / `require()` with a constructed string → `dynamic` disposition, not a bug | ADR-0011 | 1 |
| B13 | **Receiver-type inference** (`obj.method()` where `obj` is a local variable, constructor-injected property, or an imported class used as a static receiver) — closed: constructor-parameter-properties, typed fields, typed/`new`-initialized variables (single-type-per-name-per-file), and imported-name-as-static-receiver are all resolved. Unresolvable receivers stay `DispositionUnimplemented`; known-type-unknown-member stays `DispositionExternalUnknown` | ADR-0012, ADR-0004, ADR-0006 | 1 (closed) |

## C. TypeScript / JavaScript (Phase 1)

| # | Case | Origin |
|---|---|---|
| C1 | `tsconfig.json` with `paths` and `baseUrl`, including wildcards | TS resolution |
| C2 | Resolving `index.ts` / `index.tsx` when importing a directory | TS resolution |
| C3 | Implicit extensions (`./foo` → `foo.ts`, `foo.tsx`, `foo.d.ts`) | TS resolution |
| C4 | `export * from './x'` and re-exports chained two levels deep | ADR-0013 |
| C5 | `export { a as b }` — the alias must keep pointing to the original entity | ADR-0013 |
| C6 | `import type { X }` — type-only import, must not generate a runtime edge | TS |
| C7 | Mixing ESM + CJS (`require` and `import` in the same repo) | JS |
| C8 | Destructured calls: `const { foo } = require('./m'); foo()` | #2625 |
| C9 | Class methods as arrow-function properties (`foo = () => {}`) | `class_arrow_measure` |
| C10 | Destructuring of constants used as an extraction gate | #2338 |
| C11 | Type-only `.d.ts` file: bodyless entities | TS |
| C12 | Monorepo with workspaces: import crossing the package boundary | Cross-repo |
| C13 | **Prototype/schema-method assignment** (`X.methods.foo = function(){}`) — found missing entirely against a real repo (11 Mongoose model methods in one file, all invisible); fixed same session, see ADR-0004 | real-repo validation, ADR-0004 |
| C14 | **`this.method()` single-level call** (no intermediate member) — the most common call shape in OOP-style code; found entirely uncaptured against a real repo, fixed same session, see ADR-0004 | real-repo validation, ADR-0004 |

## J. Go (Phase 3a — done, ADR-0010)

| # | Case | Origin | Phase |
|---|---|---|---|
| J1 | A package spans every file in one directory, not one file — qualified names, same-name resolution, and receiver-type lookup must all work across sibling files | real self-hosting run | 3a (closed) |
| J2 | A local function-valued binding (closure, callback parameter, func-typed `var`) called bare must be `ScopeLocal`, never resolved against the same-file/same-package/builtin tiers — the first extractor to actually emit the `ScopeLocal` case B4 already described | found self-hosting: `internal/exclude`'s `fn` callback, the recursive `walk` closure pattern in both this project's own extractors | 3a (closed) |
| J3 | Struct embedding (anonymous field) grants promoted fields/methods — modeled as `RefExtends`, the closest fit in the fixed edge taxonomy | Go spec | 3a (closed) |
| J4 | Implicit interface satisfaction (no `implements` keyword) cannot be detected without real type-checking — a permanent, structural gap, not a missing feature | Go spec | 3a (documented, permanent) |
| J5 | A two-level selector call (`r.field.Method()`) needs its own query pattern — the outer call's function field is itself a selector expression, not a bare identifier | found self-hosting, same shape as TypeScript's `this.member.method()` (edge-case-backlog.md's C14 story) | 3a (closed) |
| J6 | A local variable's function type inferred from a multi-return call's second value (`ctx, cancel := context.WithTimeout(...)`) is not detected — only a syntactic func literal or annotation is | found self-hosting: `cmd/ctx/main.go` | 3a (documented gap) |
| J7 | A struct field typed from another package via `pkg.Type` produces no receiver-type signal — only bare `type_identifier` fields do | design scoping, ADR-0010 | 3a (documented gap) |

## D. C# (Phase 3b — done, ADR-0023)

Original anticipated cases from studying Grafel (D1-D7, kept for the record),
now annotated with what actually happened building and validating the real
extractor against eShopOnWeb — several of these were closed differently than
anticipated, and real validation surfaced cases nobody anticipated (D8-D13).

| # | Case | Origin | Status |
|---|---|---|---|
| D1 | `using static` and `using` aliases | ADR-0013 | Aliases (`using X = Y;`) closed — `internal/parser/csharp`'s `importFromMatch` parses the alias form by hand (see its doc for why: tree-sitter's field-matching can't distinguish the alias form from the plain form without also matching a false capture). `using static` is a documented, deliberate gap (D1 remains partially open) — brings a class's static members into unqualified scope, a rare-enough idiom not chased in V0. |
| D2 | Visibility boundary by `.csproj` / `.sln` — don't resolve across unreferenced projects | C# resolution | Closed differently: `internal/index/csproj.go` discovers every `.csproj` in the repo and its `RootNamespace`; `using` resolution requires an EXACT namespace-prefix match against one of those (never a partial/suffix heuristic — a guard the user explicitly asked for during this ADR's design). This naturally means a `using` naming a project this run's `.csproj` walk didn't find (e.g. one genuinely outside the solution) resolves as external — the right behavior, reached as a side effect of the exact-match design rather than an explicit project-reference graph. A `.csproj`'s actual `<ProjectReference>` edges are NOT read — two sibling projects that both define a same-named type in the same namespace could theoretically cross-resolve; not observed in eShopOnWeb, documented as a real remaining gap. |
| D3 | Namespaces with `file-scoped namespace` and nesting | C# | Not needed: qualified names are DIRECTORY-scoped (mirroring Go's own approximation, ADR-0010), never parsed from a file's own `namespace`/file-scoped-namespace declaration — see `queries/entities.scm`'s package doc. A namespace that deliberately diverges from its folder is the one real gap this leaves (matches Go's own documented approximation, not a new risk). |
| D4 | `record` and `record struct` | C# | Closed: `record_declaration` extracts as `KindClass`; `record struct`/`record class` are the same grammar node, so both are covered without a separate pattern. A positional record's primary-constructor parameters (`record Order(int Id, string Name)`) are NOT extracted as properties (C# synthesizes real properties from them at compile time, invisible to a syntax-only extractor) — documented gap. |
| D5 | Extension methods: the receiver is the first parameter | C# | Not addressed — an extension method (`static class Ext { public static void Foo(this Bar b) {...} }`) is extracted as an ordinary static method of `Ext`, and a call site `bar.Foo()` cannot resolve to it (the receiver-type tier only searches `bar`'s OWN type's methods, never every extension method whose first parameter matches). A real, common C# idiom (LINQ-style APIs); not chased in V0 — same class of gap as receiver-type inference limits already documented for TS/Go. |
| D6 | Generic interfaces: `IRepository<T>` vs `IRepository<Employee>` | ADR-0012 | Closed: every receiver-type/heritage query captures `[(identifier) (generic_name)]` and `baseTypeName` strips the type-argument list, so `IRepository<T>`, `IAsyncRepository<Order>`, etc. all resolve by their base name — validated extensively against eShopOnWeb's repository/specification patterns (`EfRepository<T> : RepositoryBase<T>, IReadRepository<T>, IRepository<T>`). |
| D7 | Test detection: xUnit, NUnit, and MSTest with distinct attributes | C# | Not built — deliberately deferred, not silently dropped: unlike TypeScript's `describe`/`it` (a call expression, matched the same way as any other call) or Go's `TestXxx` (a naming convention on an ordinary function), C#'s test frameworks mark a test via an ATTRIBUTE (`[Fact]`, `[Test]`, `[TestMethod]`) on an ordinary `method_declaration` — attributes are not parsed by this extractor at all yet. A real follow-up (attribute extraction is also what `[HttpGet]`/`[Route]` ASP.NET routing needs, so this is one gap, not two). |
| D8 | **A constructor's declared name always equals its class's own name** — found validating against eShopOnWeb: every constructor's bare `Name` collided with its class's bare `Name` in the resolver's `byName` index, making `ctx find OrderController` report a spurious `DispositionAmbiguous` between the class and its own constructor. Fixed: a constructor's `Entity.Name` is reported as `ctor` (dot-free, unlike .NET reflection's own `.ctor`, chosen only so `Entity.Qualified` reads as `Dir#Class.ctor` rather than `Dir#Class..ctor`) — `internal/parser/csharp/extractor.go`'s `entityFromMatch`. | eShopOnWeb (real repo) | 3b (closed) |
| D9 | **Tree-sitter query field order must match the grammar's own declared child order**, or the query compiler rejects the pattern outright as "Impossible pattern" — found writing `entities.scm`: `property_declaration`'s actual node order is `type` then `name` (grammar.js), but a query written `name: ... type: ...` fails to compile even though field ORDER has no semantic meaning in tree-sitter's own matching model. Same for `parameter` (`type` before `name`) and `variable_declarator` (which has NO `value:` field at all — the initializer is an unnamed positional child after the `=` token, not a named field). Not previously documented because Go/TS's own query patterns happened to already list fields in grammar order. | tree-sitter-c-sharp v0.23.1 query compiler | 3b (closed, documented for future language additions) |
| D10 | **C#'s `base_list` syntax cannot distinguish "extends a class" from "implements an interface"** — both a base class and every implemented interface appear in the same comma-separated list, with no keyword marking which is which (unlike TypeScript's separate `extends_clause`/`implements_clause`). Per an explicit user guard against the extractor guessing from a naming convention (e.g. "starts with I"), every entry is extracted as `RefExtends`; `internal/resolve.reclassifyHeritageEdge` (generic, language-agnostic core logic, not a per-language branch) corrects the edge to `RefImplements` once the target actually RESOLVES and its real `Kind` is known to be `KindInterface` — deterministic, based on resolved data, never a guess. | eShopOnWeb (real repo), user guard | 3b (closed) |
| D11 | **`new Foo(...)` was not tracked as a reference at all** in the first working version — found validating against eShopOnWeb's dominant MediatR/CQRS pattern (`_mediator.Send(new GetMyOrders(...))`): the actual internal dependency (which request type a controller sends) is entirely inside the `new` expression; the qualified call itself (`_mediator.Send`) is unresolvable by design (`IMediator` is an external MediatR interface). Fixed: `object_creation_expression` now emits a `RefTypeUse`, mirroring TypeScript's `new.target` exactly. Measured impact: resolved edges on eShopOnWeb went from 172 to 337 (nearly double) from this one fix. | eShopOnWeb (real repo) | 3b (closed) |
| D12 | **`using` directives bind no single local name** for the plain form (`using Some.Namespace;` — unlike TypeScript's named/namespace imports or Go's always-qualified imports, a plain `using` widens what's visible UNQUALIFIED, binding nothing specific). Modeled with an empty `ImportBinding.LocalName`; resolution happens entirely through `SameScopeFiles` (which reads `fe.imports` directly and expands to every file in a `using`'s exactly-matched namespace directory), never through the core pipeline's `LocalName`-matching tiers. Only the alias form (`using X = Y;`) binds a real `LocalName` and goes through `ResolveQualifiedImport` normally. | design, eShopOnWeb | 3b (closed) |
| D13 | **A multi-project solution has one `.csproj`/root-namespace per project directory, not one for the whole repo** (unlike Go's single `go.mod`) — `internal/index/csproj.go` walks the whole repo for every `.csproj` (eShopOnWeb's own repo has six: Web, ApplicationCore, Infrastructure, PublicApi, BlazorAdmin, BlazorShared) rather than assuming one. Each project's `using` resolution is tried independently by `resolveImportPath`; a project with no `<RootNamespace>` element falls back to its `.csproj` file's own name (the real MSBuild default), not a guess. | eShopOnWeb (six real .csproj files) | 3b (closed) |

## E. Python (Phase 3)

| # | Case | Origin |
|---|---|---|
| E1 | Relative imports (`from . import x`, `from ..pkg import y`) | ADR-0013 |
| E2 | Re-exports via `__init__.py` | ADR-0013 |
| E3 | `import x.y.z as w` and its qualified use | ADR-0013 |
| E4 | Decorators that wrap and rename functions | Python |
| E5 | Module methods called unqualified (the idiomatic case the allowlist must cover) | ADR-0011 |

## F. Incremental indexing (Phase 3)

| # | Case | Origin |
|---|---|---|
| F1 | **Null tree → silent total data loss.** The incremental step evicts the file's entities and re-adds them from re-extraction; if re-extraction receives a null tree and the extractor returns `nil, nil`, the file ends up **empty**, reported as success, with no error | #6151 |
| F2 | **Infinite reindex loop** from stale manifest entries: files no longer in the walk are detected as deleted on every pass → too many changes → fallback that discards the manifest GC → repeat | #5667 |
| F3 | An extraction pass that fails must not leave the graph half-done: the last good snapshot is preserved | #6209, ADR-0026 |
| F4 | Renaming a file without changing its content: re-anchors, doesn't reindex | Anchors |
| F5 | `git checkout` of a branch with hundreds of files: no full reindex | Watcher ADR |
| F6 | Branch change detected via `.git/HEAD`, not inferred from file events | `githead_poller.go` |
| F7 | A file deleted and recreated with the same content within the debounce window | Watcher |
| F8 | A file whose content reverts to an already-indexed state (undo): the `content_hash` matches, nothing is invalidated upstream | Anchors |
| F9 | Daemon down during changes: reconcile / catch-up on startup | `reconcile.go` |

## G. Watcher and filesystem

| # | Case | Origin |
|---|---|---|
| G1 | **Descriptor exhaustion on macOS**: a repo of ~32k files consumes ~40k descriptors with kqueue, against a ceiling of 61,440 | #6180 |
| G2 | The budget is derived from the **effective** `RLIMIT_NOFILE` after the kernel clamp, not the requested one | #6218 |
| G3 | The cost model is selected by **build tag**, not by `runtime.GOOS` | #6218 |
| G4 | A non-gitignored build directory that churns → quarantine after a sustained threshold | #5392 |
| G5 | A human burst of saves must **not** trigger quarantine | #5394 |
| G6 | A quarantined directory that goes quiet recovers on its own | #5394 |
| G7 | Quarantine survives a daemon restart (doesn't re-thrash) | #5394 |
| G8 | Reading a file over a slow or hung filesystem: open with a deadline, not indefinite blocking | #6416 |
| G9 | Debounce/coalesce tests with an injected clock, not dependent on the CI scheduler | `clock.go` |

## H. Parser

| # | Case | Origin |
|---|---|---|
| H1 | A file with more than 10% ERROR nodes → rejected instead of producing garbage entities | `maxErrorRatio` |
| H2 | Pathological file (minified, generated, a 2MB line) → parse timeout, not a hang | `GRAFEL_PARSE_TIMEOUT` |
| H3 | The tree is closed on the error path too (C heap leak) | #5963 |
| H4 | Binary file or non-UTF8 encoding disguised as source | Parser |
| H5 | Empty file and comment-only file | Parser |
| H6 | No tree-sitter binding type appears outside `internal/parser` (architecture test) | ADR-0023 |

## I. Context Compiler and ledger (Phase 2 — our own, no origin in Grafel)

| # | Case |
|---|---|
| I1 | Budget so small that only the primary entity fits: must degrade by ladder rung, not truncate mid-line |
| I2 | Budget larger than all available context: don't pad with noise |
| I3 | Minimum quota per category: no category disappears entirely |
| I4 | The same item requested twice in a session comes back as a handle the second time |
| I5 | An item delivered at L1 and later needed at L3: moves up a rung, isn't resent at L1 |
| I6 | The file changes between two calls in the same session: the handle is revalidated by `content_hash` and resent if it changed |
| I7 | The same capsule via CLI, MCP, and HTTP is byte-identical |
| I8 | A capsule with residuals shows the disambiguation candidates |
| I9 | Two indexings of the same source produce a byte-identical snapshot (determinism) |
| I10 | Reordering the input files doesn't change the output |
| I11 | **Object/schema-style `const` declarations are never extracted as entities** (`const User = model('User', UserSchema)`) — closed: extracted as `KindClass`, gated to module scope only (`isModuleScope`), see `queries/entities.scm`'s `entity.schemaconst` patterns and `TestExtract_SchemaStyleConst`. Verified against the real repo: resolved edges 9→14, `User`/`Article` now real, findable entities. **Measured honestly, not assumed**: this did NOT move the real-repo benchmark's aggregate recall@gold (still 0.47) — the two zero-recall tasks (R07, R10) are route-file/auth concerns, unrelated to model entities. See docs/benchmarks/2026-08-29-schemaconst-realworld-ts.md for the full account, including a real but self-canceling side effect on two other tasks' individual scores. |
| I12 | **An MCP tool handler declared with `Out=any` returning a bare slice fails `tools/call` schema validation** ("expected: record") on every single call — found by a real agent (not by this project's own tests, which never asserted on `StructuredContent`'s shape). Every handler must declare a concrete, struct-shaped `Out` type; a slice result must be wrapped in a named struct. Fixed and regression-tested (`TestMCPServer_StructuredContentIsAlwaysAnObject`) — see docs/adr/0009-live-agent-demo.md |
