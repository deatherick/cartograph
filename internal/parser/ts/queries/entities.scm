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
