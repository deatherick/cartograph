# 01 — Parser and tree-sitter binding

## Problem

Choosing a tree-sitter binding for Go and deciding how the extractors consume the tree.
The decision seems minor and turns out to be the most expensive to revert.

## How Grafel solved it

- It started with `smacker/go-tree-sitter`. That binding **is dead**: the commit they had
  pinned (2024-08-27) *is* upstream's HEAD, `ahead_by: 0`. No fresh grammars and
  no way to automate the bump.
- ADR-0023 documents the migration to the official `tree-sitter/go-tree-sitter` binding (v0.24.0,
  alive), where **each grammar is its own Go module** (`tree-sitter/tree-sitter-<lang>/bindings/go`),
  bumpable independently by Renovate.
- The measured cost of that migration: **245 files** import the binding, **1,758** references
  to `sitter.Node`, **102** call sites of `GetLanguage()`. Method names change
  (`Type()`→`Kind()`, `StartPoint()`→`StartPosition()`, `Content()`→`Utf8Text()`) and types
  (`uint32`→`uint`).
- **Zero use of tree-sitter's query engine.** There isn't a single `.scm` file in the repo.
  All extraction is hand-written manual depth-first traversal.
- Safeguards that did land correctly:
  - **10% syntax error ratio gate**: if the tree has more than 10% ERROR nodes,
    the file is rejected instead of producing garbage.
  - **Per-parse watchdog** (`GRAFEL_PARSE_TIMEOUT`): tree-sitter can hang on pathological
    files (minified, generated, 2MB lines).
  - The tree is explicitly closed **on the error path too** — not doing so leaked
    C heap for the entire life of the process.
  - An independent parser per call, with bounded concurrency. The global mutex they had
    was a workaround for a shared-state race in smacker, not a real constraint.

## The real cost of manual traversal

| Extractor | Files (excl. tests) | Lines |
|---|---:|---:|
| JavaScript/TypeScript | 39 | **21,128** |
| Python | 38 | **14,188** |
| Go | 14 | 6,239 |
| C# | 6 | 2,510 |

21k lines of Go written by hand to extract structure from TS/JS, and the binding leaked
into 245 files.

## How we solve it

1. **Official binding from day 1**: `github.com/tree-sitter/go-tree-sitter` + a grammar
   module per language. We don't pay the migration cost they paid.
2. **The binding NEVER leaves `internal/parser`.** No `sitter.*` type appears in the signature
   of anything outside that package. An architecture test (grep over imports) enforces it.
   This is the whole point: their migration cost 245 files because the type leaked.
3. **Declarative extraction with `.scm` queries**, not manual traversal. A new language is
   a queries file + a mapping from captures to entities, not 900 lines of Go.
   Manual traversal is reserved for what queries can't express (scope resolution,
   import tables).
4. We inherit the three safeguards as-is: 10% error-ratio gate, per-parse timeout,
   tree close on every path.

## Why different

The structural 80% (classes, functions, methods, imports, calls, inheritance) is exactly
what tree-sitter's query engine does well and declaratively. Grafel didn't use it and
ended up with 21k lines per language. Its advantage is that manual traversal does capture
framework patterns (React hooks, Express routes) that a query alone can't see — that's why
our design leaves a pattern layer on top of the queries, instead of replacing one
with the other.

**Accepted risk:** if queries turn out to be insufficient for C#, we fall back to manual
traversal only for that language. The `internal/parser` boundary makes that decision local.
