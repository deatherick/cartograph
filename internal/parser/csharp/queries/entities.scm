; Declarative entity extraction for C# — Phase 3b (ADR-0023), the same
; query-driven bet Phase 1/3a made for TypeScript/Go
; (internal/parser/ts, internal/parser/golang), applied to the third
; language.
;
; Node/field names verified against tree-sitter-c-sharp v0.23.1's
; node-types.json and grammar.js, not guessed.
;
; Qualified names are DIRECTORY-scoped, like Go's extractor (ADR-0010) —
; NOT re-derived from each file's own `namespace X.Y;` declaration. This is
; a deliberate, documented approximation: the standard, tool-enforced C#
; convention is that a project's folder structure mirrors its namespace
; (Visual Studio/`dotnet new` both default to this), which held for every
; real file this extractor was validated against (ADR-0023). A namespace
; that intentionally diverges from its folder (rare) is a known, honest
; gap — same category as Go's own directory-scoping approximation.

(class_declaration
  name: (identifier) @entity.name
  body: (declaration_list) @entity.body) @entity.class

(struct_declaration
  name: (identifier) @entity.name
  body: (declaration_list) @entity.body) @entity.class

(record_declaration
  name: (identifier) @entity.name) @entity.class

(interface_declaration
  name: (identifier) @entity.name
  body: (declaration_list) @entity.body) @entity.interface

(enum_declaration
  name: (identifier) @entity.name) @entity.enum

; `delegate ReturnType Handler(...)` — a named function-shaped type, C#'s
; closest analog to TypeScript's `type Handler = (...) => T` and Go's
; `type Handler func(...)`.
(delegate_declaration
  name: (identifier) @entity.name) @entity.typealias

; Methods, including interface method SIGNATURES (no body — `body:` is
; deliberately not required here, unlike Go's function/method patterns,
; since an interface member and an abstract method are both real,
; findable entities; only their body is absent).
(method_declaration
  name: (identifier) @entity.name
  parameters: (parameter_list) @entity.params) @entity.method

; Constructors — `public Foo(...) { ... }`. C# requires a constructor's
; name to be its declaring type's own name, so entity.name IS the owner
; name here; entityFromMatch still resolves the owner independently via
; enclosingTypeName for the same "one code path, not two" reason Go's
; entityFromMatch reuses entity.owner for its receiver-variable signal.
(constructor_declaration
  name: (identifier) @entity.name
  parameters: (parameter_list) @entity.params) @entity.method

; Auto-implemented and explicit properties — `public string Name { get; set; }`
; — the first extractor to populate model.KindProperty (defined in
; model.go since ADR-0003, never emitted before this ADR). A property is
; also a receiver-type signal (see receiver.propfield below): the common
; ASP.NET Core pattern `public IOrderRepository Repository { get; }` used
; the same way a constructor-injected field is.
(property_declaration
  type: [(identifier) (generic_name)] @entity.proptype
  name: (identifier) @entity.name) @entity.property

; Extension methods: `public static T Foo(this ExtendedType x, ...)`. The
; `this` parameter modifier is only valid (real, compiling C#) on a
; method's FIRST parameter — anchored here with `.` (parameter_list's
; own "(" token doesn't count against a query's anchor, only named
; children do, confirmed the same way route.call's `.`-anchored
; arguments pattern already works in internal/parser/ts). Captured
; generically: ANY parameter-list modifier, filtered against the exact
; literal "this" in Go (isThisModifier) rather than narrowed here — a
; parameter can carry OTHER modifiers (`ref`, `scoped`, `in`,
; `readonly`), and only "this" makes this an extension method.
(method_declaration
  parameters: (parameter_list
    . (parameter
        (modifier) @ext.modifier
        type: [(identifier) (generic_name)] @ext.type))) @ext.methodnode

; xUnit/NUnit/MSTest test-method detection via attributes: `[Fact]`,
; `[Theory]`, `[Test]`, `[TestMethod]` (bare or namespace-qualified,
; `[Xunit.Fact]`). attribute_list is an unnamed, positional child of
; method_declaration (grammar.js) — captured generically (ANY attribute
; on ANY method), filtered against a known allowlist in Go
; (isTestAttribute) rather than narrowed here, the same "capture broadly,
; filter by exact name in Go" split TypeScript's test.fn capture already
; uses. A method can carry multiple attribute_lists/attributes, so this
; may produce more than one match per test method — harmless, since Go
; only checks set membership by the method's own start byte.
(method_declaration
  (attribute_list
    (attribute
      name: [(identifier) (qualified_name)] @test.attr))) @test.methodnode

; Local functions (C# 7+): `void DoThing() { ... }` declared inside a
; method body. Genuinely local — never cross-resolved (model.ScopeLocal),
; the same role Go's localfunc.decl patterns play for closures; a bare
; call to one must not be flagged as a missed extraction or an ambiguous
; repo-wide bare name.
(local_function_statement
  name: (identifier) @localfunc.name) @localfunc.decl

; --- Heritage: `class X : Base, IFoo, IBar` / `interface IX : IY, IZ` ---
; base_list is an unnamed, positional child (grammar.js) shared by
; class/struct/record/interface declarations — captured as a whole node
; here, walked in Go (heritageTargets) rather than pattern-matched member
; by member: distinguishing "the base class" from "an implemented
; interface" from C# syntax ALONE is not reliable (both appear in the same
; list, with no keyword marking which is which — unlike TypeScript's
; separate extends_clause/implements_clause). Every entry is emitted as
; RefExtends; internal/resolve's generic (language-agnostic)
; reclassifyHeritageEdge corrects it to RefImplements once the target
; entity actually resolves and its real Kind is known to be Interface —
; deterministic, never a name-based guess (ADR-0023, following explicit
; user guardrails against inference-by-convention).
(class_declaration (base_list) @heritage.list) @heritage.owner
(struct_declaration (base_list) @heritage.list) @heritage.owner
(record_declaration (base_list) @heritage.list) @heritage.owner
(interface_declaration (base_list) @heritage.list) @heritage.owner

; --- Call sites, bare and qualified ---
(invocation_expression
  function: (identifier) @call.name) @call.bare

(invocation_expression
  function: (member_access_expression
    expression: (identifier) @call.object
    name: (identifier) @call.name)) @call.qualified

; `this.Method(...)` — the single most common in-class call shape.
(invocation_expression
  function: (member_access_expression
    expression: "this"
    name: (identifier) @call.name)) @call.this

; `this._field.Method(...)` — calling through a field/property accessed
; explicitly via `this.`. Less common in idiomatic C# than bare
; `_field.Method()` (already covered by call.qualified above, since a
; private field access needs no `this.` prefix), but real code does both.
(invocation_expression
  function: (member_access_expression
    expression: (member_access_expression
      expression: "this"
      name: (identifier) @call.object)
    name: (identifier) @call.name)) @call.qualified.this

; `new Foo(...)` — treated as a construction reference to the class, the
; same RefTypeUse role TypeScript's new.target plays. Found missing while
; validating against a real repo (ADR-0023, eShopOnWeb): the dominant
; MediatR/CQRS pattern there is `_mediator.Send(new GetMyOrders(...))` —
; the actual internal dependency (which request type a controller sends)
; is entirely inside the `new` expression, not the (external, unresolvable)
; `_mediator.Send` call itself.
(object_creation_expression
  type: (identifier) @newtype.target) @newtype.expr

; --- Imports: `using` directives ---
; Captured as a whole node and parsed in Go (parseUsingDirective) rather
; than pattern-matched — see that function's doc for why: the grammar's
; alias form (`using X = Y;`) and plain form (`using X.Y.Z;`) are only
; distinguishable by the presence of an `=` token among using_directive's
; children, which tree-sitter's query field-matching cannot express
; without also matching the alias form's own identifier as a false
; "plain namespace" capture.
(using_directive) @using.stmt

; --- Receiver-type signals (this project's "what type is this name?"
; map, C#'s analog of Go's structFieldTypes/fileVarTypes and TypeScript's
; receiver.* queries — docs/research/edge-case-backlog.md B13). These
; produce no entities or refs themselves. ---

; Constructor-injected dependency, the dominant ASP.NET Core DI pattern:
; `private readonly IOrderRepository _orderRepository;`
(field_declaration
  (variable_declaration
    type: [(identifier) (generic_name)] @receiver.fieldtype
    (variable_declarator
      name: (identifier) @receiver.fieldname))) @receiver.field

; A typed property used as a receiver, e.g. a base controller's
; `protected IOrderRepository Repository { get; }`.
(property_declaration
  type: [(identifier) (generic_name)] @receiver.fieldtype
  name: (identifier) @receiver.fieldname) @receiver.propfield

; A method/constructor parameter's declared type — the other half of
; constructor DI (`public OrderController(IOrderRepository repo) { ... }`)
; plus any locally-scoped typed parameter used bare within the method
; body. File-wide, not block-scoped — the same bounded simplification
; internal/parser/ts and internal/parser/golang already document and
; accept.
(parameter
  type: [(identifier) (generic_name)] @receiver.vartype
  name: (identifier) @receiver.varname) @receiver.param

; `IFoo x = ...;` — a locally typed variable declaration.
(variable_declaration
  type: [(identifier) (generic_name)] @receiver.vartype
  (variable_declarator
    name: (identifier) @receiver.varname)) @receiver.typedvar

; `var x = new Foo();` — implicitly typed, but the constructor call's own
; type name is an equally reliable signal (no annotation needed), the
; same idiom TypeScript's receiver.newvar and Go's receiver.newvar cover.
(variable_declarator
  name: (identifier) @receiver.varname
  (object_creation_expression
    type: (identifier) @receiver.vartype)) @receiver.newvar
