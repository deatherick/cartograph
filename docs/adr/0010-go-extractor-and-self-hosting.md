# ADR-0010: Go extractor — Phase 3's first language, and self-hosting

- **Status**: Accepted (done, measured on this project's own real source)
- **Date**: 2026-08-29
- **Related**: docs/MVP.md, ADR-0004/0006 (TypeScript extraction and resolution), ADR-0003 (data model)

## Context

`docs/MVP.md`'s deferred list named Phase 3 as "Go/C#/Python extraction, daemon, incremental
indexing" and specifically called out "true self-hosting/dogfooding (Cartograph analyzing its
own Go source)" as depending on a Go extractor that did not yet exist. This ADR is that
extractor, chosen as Phase 3's first language ahead of C#/Python (the master plan's original
three target languages) for one concrete reason: it is the language this project's own source is
written in, so building it unlocks the single most direct validation this project can perform —
pointing itself at itself, the same "TypeScript writing its own compiler" idea the user proposed
during Phase 2 (deferred then, executed now).

## What was built

`internal/parser/golang` (named `golang`, not `go` — `go` is a Go keyword, `package go` does not
compile), query-driven exactly like Phase 1's TypeScript extractor
(`internal/parser/golang/queries/entities.scm`, verified against tree-sitter-go v0.25.0's
`node-types.json`, not guessed):

- **Entities**: `type X struct {...}` → `KindClass` (the closest existing taxonomy fit — fields
  plus methods — rather than adding a Go-only `KindStruct`); `type X interface {...}` →
  `KindInterface`; a named type over anything else (`type ID string`, `type Handler func(...)`,
  Go's `type X = Y` alias form) → `KindTypeAlias`; `func` declarations → `KindFunction`; methods
  (pointer or value receiver) → `KindMethod`; `func TestXxx(t *testing.T)` in a `_test.go` file →
  reclassified to `KindTest` (a real function declaration, unlike Jest's callback-block tests in
  TypeScript, so this is a reclassification, not a second entity).
- **Qualified names are DIRECTORY-scoped, not file-scoped** — the single biggest structural
  difference from TypeScript's extractor. A Go package spans every file in one directory; a
  struct's methods routinely live in a different file from the struct itself, and the Go
  compiler treats them as one unit. `Entity.Qualified` is `"<repo-relative-dir>#<Name>"` (or
  `"<dir>#<Type>.<Method>"`), never the file path.
- **Every Go import is package-qualified at every call site** (`pkg.Func()`) — Go has no
  equivalent of TypeScript's destructured named import. Every `model.ImportBinding` this
  extractor emits is therefore modeled as a namespace import (`IsNamespace: true`), which turns
  out to let the resolver reuse TypeScript's existing namespace-import resolution branch for Go
  almost unchanged (see below) — an accident of Go's own syntax, not a coincidence engineered for
  convenience.
- **Struct embedding** (`type X struct { Base; ... }`) → `RefExtends`, the closest fit in the
  fixed edge taxonomy (model.go) for the promoted-fields/methods relationship it grants. Go's
  **implicit interface satisfaction has no syntax to key off** — any type whose method set
  matches an interface satisfies it, with no `implements` keyword — so this extractor never emits
  `RefImplements` for Go. This is a permanent, structural gap, not a missing feature: detecting it
  would need real type-checking, not tree-sitter queries.
- **Receiver-type signals**, the Go analog of TypeScript's `receiver.*` queries
  (docs/research/edge-case-backlog.md B13): typed `var` declarations, the dominant
  `x := Foo{}` / `x := &Foo{}` construction idiom (Go has no `new Foo()` syntax), function
  parameter types, struct field types, and — new to Go, since Go has no `this` — **the method's
  own receiver variable itself** (`func (r *Foo) Bar()` registers `r` → `Foo` as a var-type
  signal, which is what lets a call like `r.repo.FindByEmail()` inside `Bar`'s body resolve).
- **Two-level selector calls** (`r.repo.FindByEmail()`) needed their own query pattern, the exact
  same problem TypeScript's `call.qualified.this` pattern solved for `this.member.method()`: the
  outer call's function field is itself a selector expression, not a bare identifier, so the
  single-level pattern never matches it.
- **`ScopeLocal`, defined in the data model since ADR-0003 but never emitted by any extractor
  until now**: a local function-valued binding — a closure (`walk := func(n *Node) {...}`), a
  callback parameter (`fn func(path string) error`), or a func-typed `var` — called bare
  (`fn(...)`, `walk(...)`). Found by self-hosting (see below): without this signal, the
  resolver's same-file/same-package/builtin tiers all miss these and misreport
  `DispositionBugExtractor` ("this should be a package-level declaration we missed"), when the
  correct, already-designed answer was `ScopeLocal` (edge-case-backlog.md B4) the whole time —
  nobody had written an extractor that needed it before.

## Resolver changes (`internal/resolve`)

Go needed a same-package tier TypeScript has no equivalent of, and a different import-resolution
scheme, but the fixed pipeline shape (`same-file → same-package → import-table → receiver-type →
builtins → disposition`) reuses almost all of TypeScript's existing machinery:

- `fileEntry` gained `lang` and `dir` fields; `Index` gained `filesByDir` (a directory → files
  map) and `goModule` (from `go.mod`'s `module` directive, read by a new `internal/index/gomod.go`
  mirroring `loadTSConfig`'s role).
- **Same-package tier** (`findExportedEntityGo`): merges `byName` across every file in a
  directory. No re-export chasing — Go has no barrel-file concept.
- **Package-qualified imports** (`resolveGoQualifiedImport`, `resolveGoImportPath`): maps an
  import path to a directory via the module prefix; `resolveByReceiverType` was extended to also
  search sibling files in the same package directory (a struct and the method being called on it
  can legitimately live in different files — TypeScript's version of this function never needed
  to look past the current file plus one import hop).
- **Go has no bare-name allowlist tier, and does not need one**: unlike TypeScript/JavaScript,
  where an unqualified identifier can legitimately be an implicit global with no local
  declaration, Go's static resolution rules mean a bare call target must be a predeclared builtin,
  a same-file/same-package declaration, or (rare, unsupported) a dot import. Reaching the end of
  the pipeline for a Go ref therefore means the extractor missed something — `DispositionBugExtractor`,
  not TypeScript's "presumed external" `DispositionExternalUnknown` default.
- `goBuiltins` (the Go spec's predeclared identifiers — `len`, `make`, `panic`, `error`, `nil`,
  ...) and `goKnownPackages` (a starter allowlist of this project's own real third-party Go
  dependencies) added to `internal/resolve/policy.go`, disjoint lists from the existing TS/JS
  ones.

## Self-hosting: the actual measurement

`rm -rf ~/.cartograph && ./bin/ctx index ~/code/cartograph` — indexing this project's own
54-file, ~9,500-line Go source tree, the real validation this ADR exists to report:

```
files:          54
entities:       393
resolved edges: 539
bug_rate:       0.1%
duration:       206ms
dispositions:
  resolved           539
  external-known     734
  external-unknown   11
  bug-extractor      1
  unimplemented      622
  unclassified       9
```

**0.1% bug_rate** (1 bug in 1,916 total dispositions) — well under Grafel's own measured
7.8%–12% range and this project's Phase 1 exit criterion of ≤15%, on real, unmodified production
Go code (this project's own), not a synthetic fixture. `ctx find`/`inspect`/`related`/`context`
were all run against the self-index and confirmed working end-to-end, including a real
`context` capsule for the task *"add support for a new language extractor"* that correctly
surfaced `internal/parser#Extractor` (the interface), `internal/parser/golang#Extractor`, and
`internal/parser/ts#Extractor` (both implementations) as PRIMARY results — cross-language
retrieval working exactly as intended, the two extractors this project actually has serving as
each other's evidence.

**The measurement process itself found two real gaps**, both fixed in this same session before
the final number above (an initial run measured 0.5% / 10 bugs — see the fix history for why the
number moved):

1. **The `ScopeLocal` gap** described above (closures/callback parameters called bare) — 9 of the
   original 10 bug-extractor cases. Fixed by adding `queries/entities.scm`'s `localfunc.decl`
   patterns and threading a `localFuncNames` set through to `refsFromMatch`, tagging these refs
   `ScopeLocal` instead of `ScopeUnqualified`. Regression-tested
   (`TestExtract_LocalFunctionValue_IsScopeLocalNotUnqualified`).
2. **A false positive in the architecture-boundary test itself**: `internal/resolve/policy.go`'s
   first draft of `goKnownPackages` listed this project's own tree-sitter dependencies by their
   literal import path — which tripped `internal/parser/architecture_test.go`'s whole-repo text
   grep for those exact substrings (the same grep that exists to catch a *leaked binding type*,
   ADR-0023's Grafel story). Fixed by removing those two entries from the allowlist (a cosmetic
   `ExternalUnknown` instead of `ExternalKnown` classification for two of this project's own
   imports — not a bug_rate hit, since only `BugExtractor`/`BugResolver` count) and documenting
   why, without reproducing the offending substrings in the comment either.

## Known, documented gaps (not chased further this session)

- **The one remaining bug-extractor case**: `ctx, cancel := context.WithTimeout(...)` —
  `cancel`'s function type comes from `context.WithTimeout`'s return signature, not a syntactic
  func literal or annotation the `localfunc.decl` patterns can see. Real type inference would be
  needed to close this; a documented, narrow gap, not a pattern this extractor chases with
  framework-specific special-casing.
- **Struct fields typed from another package via `pkg.Type`** (`repo *pkg.UserRepo`) do not
  produce a receiver-type signal — only bare `type_identifier` fields do. A call through such a
  field (`s.repo.Method()`) resolves to `DispositionUnimplemented`, not a wrong answer, just an
  honest "don't know" — the same "best-effort, never guess" limitation ADR-0004 already documents
  for TypeScript's receiver-type tier.
- **No export-awareness** (a name starting with a lowercase letter is unexported in Go and should
  never resolve across packages) — same gap TypeScript's resolver already has; carried over
  unchanged rather than fixed twice in one language and not the other.
- **An import's local identifier, when no alias is written, is approximated as the import path's
  last segment** — correct for every package in this project's own source and the overwhelming
  majority of real Go packages, wrong only for the rare package whose declared name differs from
  its directory name.

None of these moved `bug_rate` above 0.1% on this project's own real source, and per this
project's own standing rule (docs/MVP.md), a documented gap is not the same thing as a bug —
these are recorded here so they are not silently forgotten, not because they block anything.
