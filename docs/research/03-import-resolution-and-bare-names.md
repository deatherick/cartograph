# 03 — Resolution: imports, bare names, and dispatch by receiver type

## Problem

Turning `foo(...)`, `mod.foo(...)`, and `x.Foo(...)` into correct edges. It's *the*
hard problem of static indexing and where graph quality is won or lost.

## How Grafel solved it — three ADRs, three lessons

### ADR-0011 — Bare names: allowlist, not blacklist

A "bare name" is a callsite where only an unqualified identifier is seen. It is
**the single biggest source of resolver false positives**. Both extremes fail:

- Resolving everything optimistically: `format(...)` matches every entity called `format`,
  including unrelated classes' methods. Edges multiply and the graph becomes noise.
- Rejecting everything: real edges are lost in dynamic languages where calling module
  helpers unqualified is idiomatic.

Their solution, in three layers:
1. **Per-language allowlist** — names that can be matched optimistically. An entry
   enters the allowlist only when matching it bare can't plausibly be wrong.
2. **Exclusion list of collision-prone names** — `format`, `get`, `set`, `run`,
   `make`, `init`, `new`, `value`… Never matched, even if they'd pass the allowlist gate.
3. **Everything else → disposition**, category `bare_name_no_scope`. Never an edge.

Explicit principle: *"whitelisting is safer than blacklisting for graph quality:
the edges that exist are real, even if some are missed."*

Every new language starts with an **empty** allowlist and grows only when a fixture proves
a name is safe.

### ADR-0013 — Import-aware cross-file resolution

The binding almost always arrives via an `import` at the top of the file and the target lives
in another file. Ignoring that degrades quality in every language with namespaces.

Solution: each extractor emits a **per-file import table** (alias → qualified
target). The resolver, faced with a callsite with a non-empty qualifier:
1. looks up the qualifier in the file's import table → translates alias to canonical module;
2. resolves the canonical against the repo's entity index;
3. **only** falls back to the bare-name resolver if there's no qualifier.

The import table is extractor output, not recomputed at query time.

### ADR-0012 — Tracking the receiver's static type

In typed languages, `x.Foo(...)` dispatches by the static type of `x`. When `x` is an
stdlib interface (`io.Reader`, `IEnumerable<T>`), the resolver knows the method name
but not the implementation. Both losing the edge and matching by name across the whole graph fail equally.

Solution: extractors record the **receiver's static type**, and a dedicated pass:
1. recognizes a **curated** set of per-language stdlib interfaces;
2. searches the callsite's scope for concrete types that could land there;
3. if there is **exactly one** candidate that defines the method → emits the edge;
4. otherwise → `stdlib_interface_unresolved` disposition. **Never guesses.**

The set is curated, not heuristic: each entry costs zero or one false positive in the
corpus tests before it's added.

## How we solve it

All three are adopted almost as-is — they're hard-won knowledge and there isn't a
more ready-made version of this. The differences:

1. **Fixed and documented pipeline order**, same as theirs:
   `same-file → import-table → receiver-type → bare-name(allowlist) → disposition`.
2. **The allowlist and the exclusion list live in data, not code.** They left this explicit:
   *"runtime tuning is not supported in v1; allowlist edits require a binary rebuild"*.
   We load them from an embedded file that's project-overridable, because a
   large monorepo has its own vocabulary of generic names. The base exclusion
   list starts from theirs, which is already validated.
3. **The unique-candidate rule is generalized**: not only for stdlib interfaces, but as a
   global resolver policy — *if there's exactly one candidate, edge with high confidence;
   if there's more than one, disposition with the candidate list as evidence*. The context
   capsule can show that ambiguity to the agent, who often can disambiguate it.
   That's a use of the Context Compiler that Grafel doesn't have: turning the resolver's
   ambiguity into a concrete question instead of a lost edge.
4. **The empty-allowlist-per-language rule is enforced in CI**: a new language
   can't add entries without a fixture that justifies them.

## Application to our three languages

- **TypeScript/JS**: the import table is mandatory (ESM + CJS + `export *` re-exports).
  Also `tsconfig.json` with `paths`/`baseUrl`, `index.ts` resolution, and implicit
  extensions. Here 90% of edges depend on the import table.
- **C#**: `using` + `using static` + alias, namespaces, and the project graph (`.csproj`/`.sln`)
  as a visibility boundary. Receiver-type is especially profitable since it's typed.
- **Python**: `from x import y`, relative imports, re-exports via `__init__.py`. This is where
  bare-name policy matters most.
