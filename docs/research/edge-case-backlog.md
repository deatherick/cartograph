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

## D. C# (Phase 3)

| # | Case | Origin |
|---|---|---|
| D1 | `using static` and `using` aliases | ADR-0013 |
| D2 | Visibility boundary by `.csproj` / `.sln` — don't resolve across unreferenced projects | C# resolution |
| D3 | Namespaces with `file-scoped namespace` and nesting | C# |
| D4 | `record` and `record struct` | C# |
| D5 | Extension methods: the receiver is the first parameter | C# |
| D6 | Generic interfaces: `IRepository<T>` vs `IRepository<Employee>` | ADR-0012 |
| D7 | Test detection: xUnit, NUnit, and MSTest with distinct attributes | C# |

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
