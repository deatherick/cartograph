# ADR-0004: Query-based TypeScript extraction, not hand-written AST traversal

- **Status**: Accepted (Phase 1 vertical slice landed; extraction coverage is ongoing)
- **Date**: 2026-08-29

## Context

Phase 1's central bet, made explicit in `docs/research/01-parser-and-treesitter-binding.md`,
was that tree-sitter's own declarative query engine (`.scm` files) could cover the 80%
structural surface Grafel's extractor covers with 21,128 hand-written lines of AST traversal
for TypeScript/JavaScript alone, and zero `.scm` files anywhere in its codebase.

This ADR records the outcome of that bet after building a working vertical slice — parser →
extractor → resolver → in-memory graph → CLI — and validating it against both a synthetic
fixture (`fixtures/ts-basic`, hand-written) and a real, unmodified open-source repository
(`typescript-node-express-realworld-example-app`, cloned read-only for this purpose; see
`docs/benchmarks/README.md`).

## Decision

TypeScript/JavaScript extraction is implemented entirely as tree-sitter queries
(`internal/parser/ts/queries/entities.scm`), with the Go extractor doing only capture
processing and scope-attribution bookkeeping — no manual AST walking. As of this ADR the
query file covers: classes, interfaces, functions, methods, enums, type aliases, prototype/
schema-method assignment (`X.methods.foo = function(){}`), class heritage (`extends`/
`implements`/interface `extends`), ESM imports (default/named/aliased/namespace), and four
call-expression shapes (bare, `obj.method()`, `this.method()`, `this.member.method()`).

## Result

The bet holds, with real-repo validation surfacing two coverage gaps the synthetic fixture
did not, both fixed within this same slice:

1. **Prototype/schema-method assignment was invisible.** The real repo's model layer (Mongoose)
   writes every method as `Schema.methods.foo = function() {...}`, never as a class method —
   11 methods in one file alone. The extractor's original query only recognized
   `class`/`interface` declarations and missed every one of these silently. Fixed by adding
   a query pattern for two-level property assignment with a `function_expression`
   right-hand side, deliberately generic (not tied to the literal word `methods`) so it also
   covers Mongoose `.statics.` and analogous prototype-assignment idioms elsewhere.
2. **`this.method()` — single-level, no intermediate member — was entirely uncaptured.** The
   extractor had patterns for bare calls (`foo()`) and two-level qualified calls
   (`obj.method()`, `this.member.method()`) but nothing for the single most common shape in
   real OOP-style code: a method calling a sibling method on the same instance
   (`this.generateJWT()` called from `toAuthJSON`). Fixed with a dedicated query pattern.

Both gaps were found by running the real, unmodified repo through the pipeline and noticing
implausibly low entity/edge counts — exactly the value real-repo validation was added for
(see the conversation record: "hagamos una prueba real, descarguemos un repositorio y midamos
el resultado"). Neither gap would have been caught by the synthetic fixture alone, since it
was written by the same author who wrote the queries and unconsciously avoided the shapes the
queries didn't handle.

### Measured, before and after both fixes (`ctx stats`)

| Metric | Synthetic fixture (`ts-basic`) | Real repo (`realworld-ts`) |
|---|---:|---:|
| Files | 14 | 20 |
| Entities | 58 | 13 → **28** |
| Resolved edges | 35 | 6 → **9** |
| `bug_rate` | 0.0% | 1.2% → **1.1%** |

`bug_rate` was already low before the fixes (1.2%) — the missing entities didn't manifest as
resolver bugs, they manifested as **entities that never existed at all**, which is a worse,
quieter failure than an unresolved reference to a known one (a graph that silently doesn't
know eleven real methods exist is more dangerous than one that admits it can't resolve a call).
This is why entity/edge counts, not just `bug_rate`, must be sanity-checked against real code —
a taxonomy of dispositions says nothing about references an extractor never emitted a Ref for
in the first place.

Grafel's own real-corpus `bug_rate` range is 7.8%–12% (docs/research/08). Our 1.1% is not yet
comparable: it reflects a much smaller extraction surface (Phase 1 gap: receiver-type inference
is unimplemented — `DispositionUnimplemented`, 181 refs in the real repo — is explicitly
excluded from `bug_rate` as a documented scope gap, not a defect; see
`internal/resolve`'s package doc). A fair comparison needs that tier built first.

## Consequences

- Adding a new construct to the TypeScript extractor is a `.scm` pattern plus a small capture-
  handling branch in Go — not a new hand-written traversal function. The Mongoose
  methods-assignment fix above was ~15 lines of query plus ~20 lines of Go.
- Real-repo validation is now a standing practice for this extractor, not a one-time check:
  `internal/index`'s test suite runs against both the synthetic fixture and
  `~/code/_ref/realworld-ts` (skipped cleanly in CI where the external clone isn't present).
- The receiver-type tier (`obj.method()` where `obj` is a local variable, not an import) remains
  unimplemented and is the largest share of unresolved references in real code (181 of ~236
  total refs in the real repo). This is Phase 1's most significant remaining gap and the next
  place to invest, once persistence (`internal/store`) exists to make iteration cheap.

## Alternatives considered

- **Hand-written AST traversal** (Grafel's approach) — rejected per the original Phase 1 bet;
  this ADR is the evidence that the alternative was validated, not merely asserted.
- **A generic/framework-annotated pattern library shipped as data** (e.g., a `frameworks.yaml`
  cataloging ORM idioms per package, closer to Grafel's `internal/custom`/`internal/frameworks`)
  — deferred. The two gaps found here were general JS/TS idioms (property assignment, `this`
  call shapes), not framework-specific; a framework catalog is Phase 7 scope
  (`docs/research/09-assessment-and-decisions.md`), not Phase 1.
