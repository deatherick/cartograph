package resolve

import "github.com/deatherick/cartograph/internal/model"

// LanguagePolicy is what a language plugs into the resolver: every
// resolution decision that differs by language, so the core pipeline
// (resolveQualified/resolveUnqualified/resolveByReceiverType, in
// resolve.go) never branches on model.Lang directly. Each language's
// policy is one self-contained file in this package (lang_ts.go,
// lang_go.go) — adding a new language means writing one new file
// implementing this interface and registering it (internal/index wires
// extractor + policy together per language, the same place it already
// maps file extensions to parser.Extractor values); nothing in the core
// pipeline or in an existing language's file ever changes.
//
// No language's file imports another's, and neither imports this
// package's caller (internal/index) — only this interface and the shared
// Index/fileEntry types connect them. That is what makes languages
// independently addable: two policies interact only through this
// contract, never with each other's code, so there is no bidirectional
// dependency to maintain as more languages are added.
type LanguagePolicy interface {
	// Lang reports which model.Lang this policy handles. Index uses this
	// to key its policies map — RegisterPolicy panics on a duplicate Lang,
	// the same "fail loudly at wiring time" discipline internal/parser/ts
	// and internal/parser/golang's New() already follow for a malformed
	// embedded query.
	Lang() model.Lang

	// SameScopeFiles returns every file (other than file itself, which the
	// core pipeline already tries first) that participates in unqualified
	// name resolution alongside it. A one-module-per-file language
	// (TypeScript) returns nil — there is no broader scope. A
	// directory-scoped language (Go, where a package spans every file in
	// one directory) returns its package siblings. Also used by the
	// receiver-type tier to search sibling files for a struct/class
	// declared apart from the method being resolved on it.
	SameScopeFiles(idx *Index, file string) []string

	// ResolveQualifiedImport resolves `obj.member` once im has already been
	// identified as the import binding obj refers to (im.LocalName ==
	// obj, im.IsNamespace — the core pipeline finds the match; this method
	// decides what happens next, which is where languages genuinely
	// differ: TypeScript resolves a relative path to a single file, Go
	// resolves a module-relative import path to a whole package
	// directory).
	ResolveQualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef

	// ResolveUnqualifiedImport resolves a bare name once im has already
	// been identified as a matching NON-namespace import binding
	// (im.LocalName == name, !im.IsNamespace) — TypeScript's named-import
	// tier. A language where every import is namespace-style (Go) is
	// never called: the core loop's im.IsNamespace check excludes it by
	// construction, so such a language may implement this as an
	// unreachable stub.
	ResolveUnqualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef

	// FollowImportToMethods resolves receiverType through fe's import
	// table to another file's methodsByOwner map — the "imported class
	// used as its own receiver type" tier (`User.findById(...)` where
	// User is an imported class, not a same-file one). ok=false: no
	// import binding named receiverType was found (the common case, and
	// the only outcome for a language with no such import-aliasing
	// concept).
	FollowImportToMethods(idx *Index, file string, fe *fileEntry, receiverType string) (methods map[string]model.EntityID, ok bool)

	// IsBuiltin reports whether name is a language runtime/predeclared
	// identifier — never a repo entity, never a potential bug, regardless
	// of how many (zero) repo-wide candidates share its name.
	IsBuiltin(name string) bool

	// FinalDisposition decides the outcome for a bare name that no earlier
	// tier (same-file, same-scope, import, receiver-type, builtin)
	// resolved. Takes ref/kind, not just the name, because this tier CAN
	// still resolve an edge (TypeScript's bare-name allowlist: a name
	// bound optimistically when it is both allowlisted and has exactly one
	// repo-wide candidate) — it is not always a terminal disposition. This
	// is where "presumed external" (TypeScript/JavaScript, which has
	// implicit globals) and "presumed a missed extraction" (Go, whose
	// static resolution rules leave no third option) genuinely diverge.
	FinalDisposition(idx *Index, ref model.Ref, kind model.EdgeKind) model.ResolvedRef
}
