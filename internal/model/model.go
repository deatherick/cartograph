// Package model defines the entity/edge data model shared by every stage of
// the pipeline (parser, resolver, graph, compiler). It has no dependency on
// tree-sitter, SQLite, or any storage format — those are all downstream
// concerns. See docs/adr/0003-data-model.md for the reasoning behind every
// choice here.
package model

import (
	"crypto/sha256"
	"encoding/hex"
)

// Kind is the entity taxonomy for V0. Deliberately concrete (Class,
// Function, ...) rather than Grafel's three-layer SCOPE.* namespacing —
// see docs/research/07-identity-taxonomy-and-cross-repo.md for why: a
// capsule read by a human and an LLM communicates more with "Class" than
// with an abstraction that needs translating at render time. Framework/
// infra kinds are added only once an extractor actually populates them.
type Kind string

const (
	KindFile      Kind = "File"
	KindClass     Kind = "Class"
	KindInterface Kind = "Interface"
	KindFunction  Kind = "Function"
	KindMethod    Kind = "Method"
	KindProperty  Kind = "Property"
	KindEnum      Kind = "Enum"
	KindTypeAlias Kind = "TypeAlias"
	KindTest      Kind = "Test"
)

// overloadable reports whether Kind requires a Disambiguator to keep
// EntityID collision-free — see docs/adr/0003-data-model.md and
// docs/research/edge-case-backlog.md cases A1-A6 (Java overloads, C#
// partial methods, TS overload signatures, Python @overload all collide on
// (repo, kind, qualified_name) alone).
func (k Kind) overloadable() bool {
	return k == KindFunction || k == KindMethod
}

// Lang identifies the source language of an entity. V0 targets TypeScript/
// JavaScript first (Phase 1); Go, C#, and Python are Phase 3 — Go first,
// since it is the language this project's own source is written in and the
// self-hosting milestone (docs/MVP.md's deferred list) needs it. C# is
// Phase 3b (ADR-0023), added via the plug-and-play architecture (ADR-0011)
// with no change to this package beyond this one constant.
type Lang string

const (
	LangTS     Lang = "ts"
	LangGo     Lang = "go"
	LangCSharp Lang = "cs"
)

// EntityID is a stable, opaque identifier. Deliberately excludes file and
// line — see docs/adr/0003-data-model.md: moving code between files or
// lines within the same namespace must not invalidate a Context Ledger
// handle built on this ID.
type EntityID string

// NewEntityID computes the identity hash. disambiguator must be non-empty
// for overloadable kinds (KindFunction, KindMethod) — pass a normalized
// signature (arity + parameter type names, lowercased, comma-joined) so
// overloads/partials don't collide (edge-case-backlog.md A1-A6). For
// non-overloadable kinds, pass "".
//
// Fields are hashed with a NUL separator so ("ab","c") and ("a","bc") never
// collide — the same technique Grafel's EntityID uses (internal/graph/
// graph.go:259), kept here deliberately.
func NewEntityID(repo string, kind Kind, qualifiedName string, disambiguator string) EntityID {
	if kind.overloadable() && disambiguator == "" {
		// Not a panic: callers that don't yet have signature info (an
		// incomplete extractor, a first pass) still need a stable ID.
		// Falling back to the qualified name alone reproduces Grafel's
		// #6161 collision for that one entity — acceptable as a visible
		// degradation, never as a silent one. Extractors MUST populate
		// disambiguator for overloadable kinds; the dedupe guard in
		// internal/index catches the rest (see docs/research/edge-case-backlog.md A1).
		disambiguator = "\x00no-disambiguator"
	}
	h := sha256.New()
	h.Write([]byte(repo))
	h.Write([]byte{0})
	h.Write([]byte(kind))
	h.Write([]byte{0})
	h.Write([]byte(qualifiedName))
	h.Write([]byte{0})
	h.Write([]byte(disambiguator))
	return EntityID(hex.EncodeToString(h.Sum(nil))[:16])
}

// Anchor is the mutable location of an entity. Re-anchored on reindex
// without touching EntityID — see docs/adr/0003-data-model.md.
type Anchor struct {
	File        string // slash-normalized, repo-relative
	StartByte   uint
	EndByte     uint
	StartLine   int // 1-indexed, for human-readable output
	EndLine     int
	ContentHash string // hash of the normalized entity body, for re-anchoring
}

// Entity is a node in the graph.
type Entity struct {
	ID         EntityID
	Kind       Kind
	Lang       Lang
	Repo       string
	Qualified  string // dotted qualified name, e.g. "services/userService.UserService.register"
	Name       string // bare name, e.g. "register"
	Signature  string // rendered signature, e.g. "register(input: CreateUserInput): User"
	DocSummary string // first line of a doc comment, if any
	Anchor     Anchor
}

// EdgeKind is the closed enum of relationship kinds for V0.
type EdgeKind string

const (
	EdgeContains   EdgeKind = "CONTAINS"
	EdgeDefines    EdgeKind = "DEFINES"
	EdgeImports    EdgeKind = "IMPORTS"
	EdgeCalls      EdgeKind = "CALLS"
	EdgeUses       EdgeKind = "USES"
	EdgeExtends    EdgeKind = "EXTENDS"
	EdgeImplements EdgeKind = "IMPLEMENTS"
	EdgeOverrides  EdgeKind = "OVERRIDES"
	EdgeTests      EdgeKind = "TESTS"
)

// Provenance names where an edge's truth came from. Never mixed without a
// label — see docs/adr/0003-data-model.md.
type Provenance string

const (
	ProvenanceDeterministic Provenance = "deterministic"
	ProvenanceInferred      Provenance = "inferred"
	ProvenanceLearned       Provenance = "learned"
)

// Edge has its own identity — (Src, Dst, Kind) is deliberately NOT treated
// as a unique key, matching Grafel's own documented invariant
// (internal/graph/graph.go:271-284): two edges can legitimately share a
// triple (e.g. two distinct call sites of the same callee).
type Edge struct {
	ID         string // opaque, caller-assigned (e.g. hash of src+kind+dst+call-site anchor)
	Src        EntityID
	Dst        EntityID
	Kind       EdgeKind
	Confidence float32
	Provenance Provenance
	Evidence   string
}

// Disposition classifies what happened to a reference the resolver could
// not — or chose not to — bind to a concrete entity. See
// docs/research/02-refs-and-dispositions.md: this taxonomy plus the
// bug_rate metric it enables is the single most useful idea taken from
// Grafel's design.
type Disposition string

const (
	DispositionResolved        Disposition = "resolved"
	DispositionExternalKnown   Disposition = "external-known"
	DispositionExternalUnknown Disposition = "external-unknown"
	DispositionDynamic         Disposition = "dynamic"
	DispositionAmbiguous       Disposition = "ambiguous"
	DispositionBugExtractor    Disposition = "bug-extractor"
	DispositionBugResolver     Disposition = "bug-resolver"
	DispositionUnclassified    Disposition = "unclassified"
	// DispositionUnimplemented marks a reference the resolver deliberately
	// does not attempt yet — a documented scope gap (e.g. receiver-type
	// inference: `obj.method()` where obj is a local variable, not an
	// import — Phase 1 has no type checker, so it cannot know obj's class).
	// This is NOT in Grafel's original taxonomy (docs/research/02) and is
	// added here because forcing a deliberate gap into ExternalUnknown or
	// BugExtractor would either overclaim confidence or wrongly inflate
	// bug_rate with something that isn't a defect — see
	// internal/resolve's package doc for where this is used.
	DispositionUnimplemented Disposition = "unimplemented"
)

// IsBug reports whether d counts toward bug_rate — the CI-gated metric from
// docs/research/02-refs-and-dispositions.md. Grafel's measured real-world
// range is 7.8%-12%; ours is a Phase 1 CI gate at <=15% for TypeScript alone.
func (d Disposition) IsBug() bool {
	return d == DispositionBugExtractor || d == DispositionBugResolver
}

// ResolvedRef is what the resolver produces for one reference: either a
// concrete Edge (Disposition == DispositionResolved) or a disposition with
// evidence — never a guess. Ambiguous references carry every candidate as
// evidence so the Context Compiler (Phase 2) can turn them into a
// disambiguation question instead of a lost edge.
type ResolvedRef struct {
	Disposition Disposition
	Edge        *Edge      // set only when Disposition == DispositionResolved
	Candidates  []EntityID // set for DispositionAmbiguous
	Reason      string
}

// RefKind is the kind of reference an extractor observed in the AST, before
// resolution decides what it points to.
type RefKind string

const (
	RefCall       RefKind = "call"
	RefExtends    RefKind = "extends"
	RefImplements RefKind = "implements"
	RefTypeUse    RefKind = "type_use"
)

// TargetScope is a typed classification of a reference's target, replacing
// the magic-string stub grammar Grafel's resolver parses
// (scope:<kind>:<subtype>:<lang>:<file>:<name>, ext:<pkg>, var:<name> — see
// docs/research/02-refs-and-dispositions.md). A stub with ScopeLocal can
// never reach the cross-file resolver by construction: the type system
// makes Grafel's issue #3936 (a local sort-key variable named "order"
// cross-resolving against an unrelated global "order" query param)
// unrepresentable, not just discouraged.
type TargetScope string

const (
	// ScopeLocal is a reference to something with no meaning outside the
	// entity making it (a local variable, a loop index). Never looked up
	// in any cross-file index — see edge-case-backlog.md B4.
	ScopeLocal TargetScope = "local"
	// ScopeSameFile is an unqualified name that must resolve, if at all,
	// to a declaration in the same file.
	ScopeSameFile TargetScope = "same-file"
	// ScopeQualified is a reference reached through an import binding
	// (Module is the resolved import source).
	ScopeQualified TargetScope = "qualified"
	// ScopeUnqualified is a bare name with no local declaration and no
	// import binding — subject to the bare-name allowlist policy (see
	// docs/research/03-import-resolution-and-bare-names.md).
	ScopeUnqualified TargetScope = "unqualified"
)

// RefTarget is what a Ref points at, before resolution. Deliberately a
// struct, never a string, so a ScopeLocal target cannot be handed to a
// global-index lookup by accident (see TargetScope doc).
type RefTarget struct {
	Scope  TargetScope
	Module string // resolved import source, set only when Scope == ScopeQualified
	Name   string // the bare name being referenced
	Member string // e.g. "Name.Member" for a qualified property/method access
	// ReceiverType is the statically-declared type of Name, when the
	// extractor could determine it from a type annotation — a constructor
	// parameter property (`private repo: UserRepository`), a typed class
	// field, or a locally typed variable (`const x: Foo = ...` / `const x
	// = new Foo()`). Empty when unknown. This is what makes the
	// receiver-type resolver tier possible (docs/research/03,
	// adapted from ADR-0012's Go/Java stdlib-interface-dispatch concept
	// for TypeScript's own type-annotation idioms) — see
	// internal/resolve's resolveByReceiverType.
	ReceiverType string
}

// Ref is a reference an extractor observed but did not resolve — the
// resolver's input. See docs/research/02-refs-and-dispositions.md for why
// this separation (extractor emits Refs, resolver turns them into Edges or
// Dispositions) matters: it keeps extraction parallelizable per file and
// makes the resolver's decisions independently testable.
type Ref struct {
	Kind   RefKind
	Src    EntityID // the entity making the reference
	Target RefTarget
	Anchor Anchor // where in the source the reference occurs
}

// ImportBinding is one entry of a file's import table — the highest-signal
// scoping data available without type inference (see
// docs/research/03-import-resolution-and-bare-names.md). Emitted by the
// extractor, never recomputed by the resolver.
type ImportBinding struct {
	LocalName    string // the name bound in the importing file
	Source       string // e.g. "../models/user" — unresolved to a file yet
	ImportedName string // the exported name in the source module; "" for default/namespace forms
	IsNamespace  bool   // `import * as X from "..."`
	IsDefault    bool   // `import X from "..."`
}

// ReExport is a barrel re-export: `export * from './x'` (IsStar) or
// `export { a as b } from './x'` (named). Distinct from ImportBinding
// because it flows in the resolver's IMPORT-FOLLOWING direction, not the
// importing file's own binding table — see docs/research/03-import-resolution-and-bare-names.md's
// ADR-0013 discussion and internal/resolve's followReExports.
type ReExport struct {
	Source       string // e.g. "./userModel" — unresolved to a file yet
	IsStar       bool   // `export * from` — re-exports everything
	ExportedName string // for the named form: the name being re-exported; empty for IsStar
	LocalAlias   string // the name it's re-exported as; equals ExportedName when there is no `as`
}

// FileFacts is everything an extractor produces for one file: entities plus
// unresolved refs and the import table, before any cross-file resolution
// has happened.
type FileFacts struct {
	File       string // slash-normalized, repo-relative
	Lang       Lang
	Entities   []Entity
	Refs       []Ref
	Imports    []ImportBinding
	ReExports  []ReExport
	ErrorRatio float64 // from the parser's syntax-error gate
}

// RelatedEntity pairs an entity with the graph-traversal depth at which it
// was found and the edge that led there — shared between internal/graph
// (in-memory traversal) and internal/store (snapshot traversal) so a
// caller gets the same shape regardless of which one served the query.
type RelatedEntity struct {
	Entity Entity
	Depth  int
	Via    Edge
}
