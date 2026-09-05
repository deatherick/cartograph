; Declarative entity extraction for Python — Phase 3c (ADR-0024), the same
; query-driven bet Phase 1/3a/3b made for TypeScript/Go/C#.
;
; Node/field names verified against tree-sitter-python v0.25.0's
; node-types.json and grammar.js, not guessed — the field-order lesson
; from C# (ADR-0023, edge-case-backlog.md D9: a query's field order must
; match the grammar's own declared child order, or the query compiler
; rejects the pattern as "Impossible pattern") was applied while writing
; every pattern below, verified against grammar.js's actual seq() order
; before writing each one, not rediscovered by trial and error this time.
;
; Qualified names are FILE-scoped, like TypeScript — NOT directory-scoped
; like Go/C#. This is a genuine language difference, not a narrower
; approximation: Python has no implicit same-package visibility the way a
; Go package or (by convention) a C# namespace does — a sibling file in
; the same directory still needs an explicit `from .other import Name`
; to use anything from it. See internal/resolve/lang_python.go's package
; doc.
;
; `@decorated.definition` (e.g. `@property`, `@receiver(...)`) wraps a
; class_definition/function_definition in a decorated_definition node —
; every pattern below matches the INNER definition node directly and is
; unaffected by whether it's decorated, since tree-sitter queries match a
; node wherever it appears regardless of its parent.

(class_definition
  name: (identifier) @entity.name
  body: (block) @entity.body) @entity.class

(function_definition
  name: (identifier) @entity.name
  parameters: (parameters) @entity.params
  body: (block) @entity.body) @entity.function

; --- Class heritage: `class X(Base, Mixin1, Mixin2):` ---
; Unlike C# (ADR-0023, D10), Python's superclasses list needs NO
; extends-vs-implements reclassification: Python has no formal interface
; keyword — every entry in the list is a genuine base class (multiple
; inheritance is a real, normal Python feature), so every entry is simply
; RefExtends, no resolver-side correction needed.
(class_definition
  name: (identifier) @entity.name
  superclasses: (argument_list) @heritage.list) @heritage.owner

; --- Call sites, bare and qualified ---
(call
  function: (identifier) @call.name) @call.bare

(call
  function: (attribute
    object: (identifier) @call.object
    attribute: (identifier) @call.name)) @call.qualified

; `self.attr.method(...)` — a two-level attribute chain rooted at `self`,
; the Python analog of C#'s `this._field.Method()` / Go's
; `r.repo.FindByEmail()`. Needs its own pattern because the outer call's
; function field is itself an `attribute` node here, not a bare
; identifier — call.qualified above only matches a single level.
; call.qualified.this's object node's OWN text is checked against the
; literal string "self" in Go code (extractor.go), not here — Python has
; no reserved `this`-like token the grammar itself marks, only the
; overwhelming PEP 8 naming convention, so the check is textual, exactly
; like TypeScript's `this` keyword check is structural. A first parameter
; named something other than `self` (rare, non-idiomatic) is a documented,
; honest gap, not silently guessed at.
(call
  function: (attribute
    object: (attribute
      object: (identifier) @call.selfobject
      attribute: (identifier) @call.object)
    attribute: (identifier) @call.name)) @call.qualified.this

; --- Imports ---
; `import x.y.z` / `import x.y.z as w` — a namespace-style import,
; parsed by hand in Go (parsePlainImport) rather than pattern-matched
; further: `name` is a repeatable field (`import a, b as c` imports
; several at once), and dotted_name/aliased_import need different
; LocalName derivation (see importsFromMatch's doc).
(import_statement) @import.stmt

; `from x.y import a, b as c` / `from . import x` / `from ..pkg import y`
; — captured whole and parsed by hand for the same reason: `module_name`
; is a relative_import (leading dots + optional dotted path) or a plain
; dotted_name, and `name` is a repeatable list of aliased_import/
; dotted_name entries (or a single wildcard_import for `from x import *`,
; a documented, unhandled gap — see importsFromMatch's doc).
(import_from_statement) @importfrom.stmt

; --- Receiver-type signals ---
; `self.attr = SomeClass(...)` in `__init__` (or any method) — the
; dominant Python "constructor-injected" idiom, Python's analog of C#'s
; typed field declaration, except Python has no static type system to
; declare it up front: the type is only knowable from what's actually
; assigned. object must be literally "self" (checked in Go code, same
; convention-based check as call.qualified.this above).
(assignment
  left: (attribute
    object: (identifier) @receiver.selfobject
    attribute: (identifier) @receiver.fieldname)
  right: (call
    function: (identifier) @receiver.fieldtype)) @receiver.field

; A locally typed variable via a PEP 484 annotation: `repo: UserRepository = ...`.
; Real, deterministic type information when present — rare in this
; project's own real-repo validation target (a pre-type-hints-era Django
; app), but a real, common idiom in modern (post-3.6) Python and worth
; supporting since it costs nothing extra once the query pattern exists.
(typed_parameter
  (identifier) @receiver.varname
  type: (type (identifier) @receiver.vartype)) @receiver.typedparam

(typed_default_parameter
  name: (identifier) @receiver.varname
  type: (type (identifier) @receiver.vartype)) @receiver.typeddefaultparam

; `x = SomeClass(...)` — a plain (non-self) local variable constructed
; from a call, the same "no annotation needed, the constructor call name
; IS the type" idiom TypeScript/Go/C#'s own receiver.newvar patterns cover.
(assignment
  left: (identifier) @receiver.varname
  right: (call
    function: (identifier) @receiver.vartype)) @receiver.newvar

; Note: there is no separate query pattern for "local (nested) function
; detection" here, unlike Go's localfunc.decl — a nested `def` matches
; the SAME entity.function pattern above as a top-level one (tree-sitter
; can't distinguish nesting depth via a field constraint), so extractor.go
; decides in Go code (enclosingDefScope) whether a matched
; function_definition is module-level, a class method, or nested inside
; ANOTHER function — only the last case is excluded from facts.Entities
; and recorded as a ScopeLocal-only name instead. See Extract's doc.
