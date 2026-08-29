; Declarative entity extraction for Go — the same query-driven bet Phase 1
; made for TypeScript (internal/parser/ts/queries/entities.scm), applied to
; the language this project's own source is written in (the self-hosting
; milestone, docs/MVP.md's deferred list).
;
; Node/field names verified against tree-sitter-go v0.25.0's node-types.json,
; not guessed.
;
; Go has no classes; a `type X struct {...}` is the closest analog (fields +
; methods), so it is extracted as model.KindClass — reusing the existing
; taxonomy rather than adding a Go-only KindStruct, consistent with model.go's
; "framework/infra kinds are added only once an extractor actually populates
; them" policy: a struct genuinely fills the same role a class does in this
; graph (something with fields, something methods attach to).

(type_spec
  name: (type_identifier) @entity.name
  type: (struct_type (field_declaration_list) @entity.body)) @entity.class

(type_spec
  name: (type_identifier) @entity.name
  type: (interface_type) @entity.body) @entity.interface

; A named type over anything else (string, a function type, a pointer, a
; slice, another named type, ...) is Go's closest analog to TypeScript's
; type alias — `type ID string`, `type Handler func(w, r)`. Explicitly
; alternated to the non-struct/non-interface underlying shapes; without
; this restriction, every struct/interface type_spec would ALSO match here
; (type_identifier/struct_type/interface_type are all subtypes of the `type`
; field's declared type), producing a spurious duplicate entity for every
; struct and interface — caught before it shipped by checking node-types.json
; rather than assuming the grammar disambiguates this for us.
(type_spec
  name: (type_identifier) @entity.name
  type: [
    (type_identifier)
    (qualified_type)
    (pointer_type)
    (array_type)
    (slice_type)
    (map_type)
    (channel_type)
    (function_type)
    (generic_type)
  ] @entity.underlying) @entity.typealias

; Go's `type X = Y` alias form (distinct node type from type_spec's plain
; `type X Y` definition form — type_alias vs type_spec in the grammar).
; The underlying type is not captured — `_type` is a supertype node, not a
; queryable concrete node kind, and unlike the type_spec case above there is
; no struct/interface ambiguity to resolve here (type_alias is its own node
; type, never shared with type_spec's patterns).
(type_alias
  name: (type_identifier) @entity.name) @entity.typealias

(function_declaration
  name: (identifier) @entity.name
  parameters: (parameter_list) @entity.params
  body: (block) @entity.body) @entity.function

; Method with a pointer receiver: `func (r *Foo) Bar(...) {...}`.
(method_declaration
  receiver: (parameter_list
    (parameter_declaration
      name: (identifier)? @receiver.varname
      type: (pointer_type (type_identifier) @entity.owner)))
  name: (field_identifier) @entity.name
  parameters: (parameter_list) @entity.params
  body: (block) @entity.body) @entity.method

; Method with a value receiver: `func (r Foo) Bar(...) {...}`.
(method_declaration
  receiver: (parameter_list
    (parameter_declaration
      name: (identifier)? @receiver.varname
      type: (type_identifier) @entity.owner))
  name: (field_identifier) @entity.name
  parameters: (parameter_list) @entity.params
  body: (block) @entity.body) @entity.method

; Struct embedding (anonymous field): `type X struct { Base; ... }` —
; grants promoted fields/methods, the closest Go analog to inheritance in
; this project's fixed EXTENDS/IMPLEMENTS/OVERRIDES edge taxonomy (model.go).
; Distinguished from a normal named field by the ABSENCE of field_declaration's
; optional `name` field — an anonymous field has none. Go's implicit
; interface satisfaction (no `implements` keyword — any type satisfying an
; interface's method set is undetectable without real type-checking) is a
; deliberate, permanent extractor gap: this extractor never emits
; EdgeImplements for Go, consistent with there being no syntax to key off of.
(field_declaration
  name: (field_identifier)? @field.name
  type: (type_identifier) @field.type) @field.decl

(field_declaration
  name: (field_identifier)? @field.name
  type: (pointer_type (type_identifier) @field.type)) @field.decl.ptr

; Call sites, bare and qualified. Go's grammar does not distinguish
; syntactically between `pkg.Func()` (package-qualified) and
; `receiver.Method()` (method call through a value) — both are
; selector_expression on an identifier operand. The resolver tells them
; apart the same way TypeScript's does: check the import table first, then
; fall back to receiver-type inference (docs/research/03's tiered pipeline,
; adapted here rather than reinvented).
(call_expression
  function: (identifier) @call.name) @call.bare

(call_expression
  function: (selector_expression
    operand: (identifier) @call.object
    field: (field_identifier) @call.name)) @call.qualified

; `base.object.Name(...)` — a two-level selector, the Go analog of
; TypeScript's `this.member.method()`: calling through a field of a locally
; typed value (most commonly the method's own receiver, e.g.
; `r.repo.FindByEmail()` inside a method on r). Needs its own pattern
; because the outer call_expression's function field is itself a
; selector_expression here, not a bare identifier — call.qualified above
; only matches when operand is a plain identifier.
(call_expression
  function: (selector_expression
    operand: (selector_expression
      operand: (identifier) @call.base
      field: (field_identifier) @call.object)
    field: (field_identifier) @call.name)) @call.qualified2

; Imports. Every Go import is used package-qualified at call sites
; (`pkg.Func()`), so every ImportBinding is modeled the same way TypeScript's
; namespace import (`import * as x`) is — see internal/resolve's package doc
; for why that lets Go reuse the same resolution tier unchanged. Blank
; (`_ "pkg"`) and dot (`. "pkg"`) imports deliberately produce no capture
; here — see import.alias's type constraint below — since neither binds a
; usable identifier this extractor can resolve calls through (dot imports
; bring a package's exports into unqualified scope, a rare and discouraged
; idiom; documented gap, not silently wrong: a dot-imported symbol simply
; falls through to the same-file/same-package/builtin tiers and, finding
; none, is reported as a disposition rather than guessed).
(import_spec
  name: (package_identifier)? @import.alias
  path: (interpreted_string_literal) @import.source) @import.stmt

; --- Receiver-type signals (Go's analog of TypeScript's receiver.* queries,
; docs/research/edge-case-backlog.md B13) — these do not produce entities or
; refs themselves; the extractor uses them to build a "what type is this
; name?" map so `receiver.Method()` calls can resolve the same way
; `this.repo.findByEmail()` does in TypeScript. ---

; `var x Foo` / `var x *Foo` — package-level or function-local.
(var_spec
  name: (identifier) @receiver.varname
  type: (type_identifier) @receiver.vartype) @receiver.typedvar

(var_spec
  name: (identifier) @receiver.varname
  type: (pointer_type (type_identifier) @receiver.vartype)) @receiver.typedvar

; `x := Foo{}` / `x := &Foo{}` — the dominant Go construction idiom, since
; Go has no `new Foo()` syntax; the type comes from the composite literal's
; own type field, not an annotation.
(short_var_declaration
  left: (expression_list (identifier) @receiver.varname)
  right: (expression_list (composite_literal type: (type_identifier) @receiver.vartype))) @receiver.newvar

(short_var_declaration
  left: (expression_list (identifier) @receiver.varname)
  right: (expression_list
    (unary_expression
      operator: "&"
      operand: (composite_literal type: (type_identifier) @receiver.vartype)))) @receiver.newvar

; A function parameter's declared type, treated as a variable-type signal
; scoped to the whole file — the same file-wide, not block-scoped,
; simplification internal/parser/ts's fileVarTypes already documents and
; accepts (a parameter name reused with a different type in another
; function collapses to "unknown" rather than being guessed; see Extract's
; fileVarTypesRaw doc).
(parameter_declaration
  name: (identifier) @receiver.varname
  type: (type_identifier) @receiver.vartype) @receiver.param

(parameter_declaration
  name: (identifier) @receiver.varname
  type: (pointer_type (type_identifier) @receiver.vartype)) @receiver.param

; A local function-valued binding — a closure (`walk := func(n *Node) {...}`),
; a callback parameter (`fn func(path string, content []byte) error`), or a
; func-typed `var` declaration (`var walk func(*Node)`, assigned to
; separately). Called bare (`fn(...)`, `walk(...)`, `cancel()`), these are
; NOT package-level declarations — without this signal, the resolver's
; same-file/same-package/builtin tiers would all miss them and misreport
; DispositionBugExtractor ("this should be a package-level thing we
; missed"), when the correct, existing taxonomy answer is ScopeLocal (model.go,
; edge-case-backlog.md B4) — the first extractor to actually emit it. Found
; by dogfooding Cartograph on its own Go source (docs/MVP.md's self-hosting
; milestone): internal/exclude.WalkSource's own `fn` callback parameter, and
; the recursive `walk` closure pattern in internal/parser/ts and
; internal/parser/golang's own extractors, are exactly this shape.
(short_var_declaration
  left: (expression_list (identifier) @localfunc.name)
  right: (expression_list (func_literal))) @localfunc.decl

(parameter_declaration
  name: (identifier) @localfunc.name
  type: (function_type)) @localfunc.decl

(var_spec
  name: (identifier) @localfunc.name
  type: (function_type)) @localfunc.decl
