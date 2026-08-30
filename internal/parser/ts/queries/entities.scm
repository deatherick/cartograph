; Declarative entity extraction for TypeScript/JavaScript, replacing the
; manual AST traversal Grafel wrote by hand (21,128 lines for its TS/JS
; extractor — see docs/research/01-parser-and-treesitter-binding.md). This
; is the central bet of Phase 1: that tree-sitter's own query engine covers
; the 80% structural surface a hand-written walker was used for.
;
; Node/field names verified against tree-sitter-typescript v0.23.2's
; node-types.json, not guessed.

(class_declaration
  name: (type_identifier) @entity.name
  body: (class_body) @entity.body) @entity.class

(interface_declaration
  name: (type_identifier) @entity.name
  body: (interface_body) @entity.body) @entity.interface

(function_declaration
  name: (identifier) @entity.name
  parameters: (formal_parameters) @entity.params
  body: (statement_block) @entity.body) @entity.function

(method_definition
  name: (property_identifier) @entity.name
  parameters: (formal_parameters) @entity.params
  body: (statement_block) @entity.body) @entity.method

(enum_declaration
  name: (identifier) @entity.name
  body: (enum_body) @entity.body) @entity.enum

(type_alias_declaration
  name: (type_identifier) @entity.name) @entity.typealias

; Prototype/schema-method assignment: `X.methods.foo = function(...) {...}`.
; The dominant OOP idiom in pre-ES6-class and Mongoose-style JS/TS code —
; found while validating against a real repo
; (typescript-node-express-realworld-example-app): every model method in
; that codebase is written this way, none as class methods, and the
; extractor initially missed all of them entirely (a fully invisible
; entity is worse than an unresolved reference to a known one). Deliberately
; generic (any two-level property assignment, not just literally
; `.methods.`) so it also covers Mongoose `.statics.` and analogous
; prototype-assignment conventions in other libraries.
(assignment_expression
  left: (member_expression
    object: (member_expression
      object: (identifier) @entity.owner)
    property: (property_identifier) @entity.name)
  right: (function_expression
    parameters: (formal_parameters) @entity.params
    body: (statement_block) @entity.body)) @entity.methodassign

; Object/schema-style const declarations: `const User = model('User', UserSchema)`,
; `export const User: Model<IUserModel> = model<IUserModel>('User', UserSchema)`.
; Found missing entirely while validating against a real repo
; (typescript-node-express-realworld-example-app): every Mongoose model in that
; codebase is exported exactly this way — a plain `const` binding, never a class
; — and with no entity for it, every OTHER file's import of `User` (the actual
; domain type used everywhere) had nothing to resolve to, measured as the root
; cause of that repo's Context Compiler recall gap (edge-case-backlog.md I11).
; The `.` before the string argument anchors it as the factory call's FIRST
; argument (`someFn('Name', ...)`), distinguishing this from an unrelated call
; like `someFn(x, 'label')` where a string merely appears later in the argument
; list. Two shapes: a bare factory name (`model(...)`) and a qualified one
; (`mongoose.model(...)`) — both real, both seen in practice.
(variable_declarator
  name: (identifier) @entity.name
  value: (call_expression
    function: (identifier)
    arguments: (arguments . (string (string_fragment))))) @entity.schemaconst

(variable_declarator
  name: (identifier) @entity.name
  value: (call_expression
    function: (member_expression
      property: (property_identifier))
    arguments: (arguments . (string (string_fragment))))) @entity.schemaconst

; Express-style route registration: `router.get('/', authentication.optional,
; function (req, res, next) {...})`, or an arrow-function handler. Found
; missing entirely while validating against the same real repo
; (typescript-node-express-realworld-example-app): every route file there
; registers routes exactly this way, with all the real application logic
; living in an ANONYMOUS callback with no declared name of its own —
; before this, such a file produced ZERO entities at all, measured as the
; direct cause of a Context Compiler recall=0 on two real tasks whose gold
; file was exactly this kind of route file (edge-case-backlog.md, ADR
; TODO). The handler itself becomes the entity (routeEntityFromMatch
; synthesizes its name from the HTTP verb + path, since there is no
; identifier to use); middleware arguments between the path and the
; handler (`authentication.optional`, etc.) are skipped via `(_)*` —
; only the first (path) and last (handler) argument positions are
; anchored, so this matches regardless of how many middlewares sit
; between them.
(call_expression
  function: (member_expression
    object: (identifier) @route.receiver
    property: (property_identifier) @route.verb)
  arguments: (arguments
    . (string (string_fragment) @route.path)
    (_)*
    .
    [
      (function_expression body: (statement_block)) @route.handler
      (arrow_function body: (statement_block)) @route.handler
    ])) @route.call

; Class heritage: `class X extends Y implements Z`
(class_declaration
  name: (type_identifier) @entity.name
  (class_heritage
    (extends_clause
      value: (identifier) @extends.target)))

(class_declaration
  name: (type_identifier) @entity.name
  (class_heritage
    (implements_clause
      (type_identifier) @implements.target)))

(interface_declaration
  name: (type_identifier) @entity.name
  (extends_type_clause
    type: (type_identifier) @extends.target))

; Call sites, bare and qualified. Qualified calls capture the object
; identifier separately so the resolver's import-table pass can translate
; it; see docs/research/03-import-resolution-and-bare-names.md.
(call_expression
  function: (identifier) @call.name) @call.bare

(call_expression
  function: (member_expression
    object: (identifier) @call.object
    property: (property_identifier) @call.name)) @call.qualified

; `this.method(...)` — calling a sibling method on the same instance. The
; single most common call shape in OOP-style code (found missing entirely
; while validating against a real repo: every Mongoose schema method calls
; at least one sibling this way, e.g. `toAuthJSON` calling
; `this.generateJWT()`). Resolved same-file/same-class by the resolver,
; not here.
(call_expression
  function: (member_expression
    object: (this)
    property: (property_identifier) @call.name)) @call.this

; `this.member.method(...)` — calling through a constructor-injected
; dependency (`this.repo.findByEmail(...)`), the single most common
; qualified-call shape in the fixtures this extractor was validated
; against. `this.member` is itself a member_expression, not a bare
; identifier, so it needs its own pattern rather than falling out of the
; one above.
(call_expression
  function: (member_expression
    object: (member_expression
      object: (this)
      property: (property_identifier) @call.object)
    property: (property_identifier) @call.name)) @call.qualified.this

; new X(...) — treated as a construction reference to the class.
(new_expression
  constructor: (identifier) @new.target) @new.expr

; ESM imports.
(import_statement
  (import_clause
    (identifier) @import.default)
  source: (string (string_fragment) @import.source)) @import.stmt

; alias is optional on import_specifier — a single pattern with `?` avoids
; matching an aliased specifier twice (once without the alias capture, once
; with), which would otherwise produce a spurious ImportBinding whose
; LocalName is wrong (the original name instead of the alias).
(import_statement
  (import_clause
    (named_imports
      (import_specifier
        name: (identifier) @import.named
        alias: (identifier)? @import.alias)))
  source: (string (string_fragment) @import.source)) @import.stmt

(import_statement
  (import_clause
    (namespace_import
      (identifier) @import.namespace))
  source: (string (string_fragment) @import.source)) @import.stmt

; --- Receiver-type signals (closes the largest Phase 1 resolver gap: see
; docs/research/edge-case-backlog.md B13, ADR-0004/ADR-0006) ---
;
; These do not produce entities or refs by themselves — the extractor uses
; them to build a "what type is this name?" map, then attaches
; RefTarget.ReceiverType to qualified-call refs so the resolver's
; receiver-type tier can bind `this.repo.findByEmail()` /
; `service.method()` the same way ADR-0012 describes for statically-typed
; languages, adapted here to TypeScript's own type-annotation idioms
; rather than Go/Java stdlib-interface dispatch.

; Constructor parameter property: `constructor(private repo: UserRepository) {}`
; — TypeScript sugar declaring BOTH a constructor parameter and a class
; field of the same name/type in one place. The dominant way real code
; declares constructor-injected dependencies (confirmed against both this
; project's own fixtures and the real-repo validation clone).
(required_parameter
  (accessibility_modifier)
  pattern: (identifier) @receiver.propname
  type: (type_annotation (type_identifier) @receiver.proptype)) @receiver.ctorprop

; Typed class field declared directly in the class body (no constructor
; sugar): `private repo: UserRepository;`
(public_field_definition
  name: (property_identifier) @receiver.propname
  type: (type_annotation (type_identifier) @receiver.proptype)) @receiver.fieldprop

; Locally typed variable: `const x: Foo = ...`
(variable_declarator
  name: (identifier) @receiver.varname
  type: (type_annotation (type_identifier) @receiver.vartype)) @receiver.typedvar

; Variable initialized via `new`: `const x = new Foo(...)` — the type is
; the constructor name, no explicit annotation needed.
(variable_declarator
  name: (identifier) @receiver.varname
  value: (new_expression constructor: (identifier) @receiver.vartype)) @receiver.newvar

; --- CommonJS require() imports ---
; `const x = require('./m');`
(variable_declarator
  name: (identifier) @import.cjs.default
  value: (call_expression
    function: (identifier) @import.cjs.require
    arguments: (arguments (string (string_fragment) @import.cjs.source)))) @import.cjs.stmt

; `const { a, b } = require('./m');`
(variable_declarator
  name: (object_pattern
    (shorthand_property_identifier_pattern) @import.cjs.named)
  value: (call_expression
    function: (identifier) @import.cjs.require2
    arguments: (arguments (string (string_fragment) @import.cjs.source)))) @import.cjs.stmt2

; --- Re-exports (barrel files): `export * from './x'` and
; `export { a, b as c } from './x'`. Both share export_statement's
; `source` field; disambiguated in Go by whether an export_clause (named
; specifiers) is present among the match's captures — see
; docs/research/03-import-resolution-and-bare-names.md's ADR-0013
; discussion of re-exports as the highest-signal scoping data available.
(export_statement
  source: (string (string_fragment) @reexport.source)) @reexport.stmt

(export_statement
  (export_clause
    (export_specifier
      name: (identifier) @reexport.named
      alias: (identifier)? @reexport.alias))
  source: (string (string_fragment) @reexport.source)) @reexport.namedstmt

; --- Test detection: `it('...', ...)` / `test('...', ...)` /
; `describe('...', ...)` with a string-literal first argument. The
; dominant Jest/Mocha convention; framework-specific patterns beyond this
; (custom test runners) are Phase 7 scope (docs/research/09's "framework
; catalog" deferral), not Phase 1.
(call_expression
  function: (identifier) @test.fn
  arguments: (arguments
    . (string (string_fragment) @test.name))) @test.call
