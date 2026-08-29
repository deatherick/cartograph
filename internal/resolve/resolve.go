// Package resolve turns the unresolved model.Refs an extractor produces
// into model.Edges or model.Dispositions, following a fixed pipeline shape
// documented in docs/research/03-import-resolution-and-bare-names.md:
//
//	same-file → same-scope → import-table → receiver-type → builtins → disposition
//
// Everything that differs by language is a LanguagePolicy (langpolicy.go) —
// this file contains NO per-language branching: no specific model.Lang
// value is ever named directly in it (verified by
// TestArchitectureBoundary_CoreNeverBranchesOnLang in resolve_test.go —
// which is also why this comment never spells out either language's
// constant by name).
// TypeScript's and Go's policies (lang_ts.go, lang_go.go) are each a single
// self-contained file that never imports or references the other — adding
// a third language means writing one more such file and registering it
// (internal/index wires extractor + policy together per language); nothing
// here or in an existing language's file changes. See langpolicy.go's
// interface doc for the full reasoning.
//
// Every tier follows Grafel's own hard-won policy: whitelisting is safer
// than blacklisting (docs/research/03) — a bare name never auto-resolves
// just because exactly one candidate exists repo-wide; a language's policy
// must say so explicitly (TypeScript's allowlist; Go has none, and does
// not need one — see lang_go.go's FinalDisposition doc).
package resolve

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path"
	"strings"

	"github.com/deatherick/cartograph/internal/model"
)

// fileEntry is what the resolver keeps per file: its entities (for
// same-file resolution), its import table (for import-table resolution),
// and its owner-scoped methods (for the receiver-type tier).
type fileEntry struct {
	lang     model.Lang
	dir      string // repo-relative directory — the scope unit for a directory-scoped language's LanguagePolicy (e.g. Go's)
	entities []model.Entity
	byName   map[string][]model.EntityID // bare name -> entities defined in THIS file
	imports  []model.ImportBinding
	// methodsByOwner indexes Method-kind entities by (owning class name,
	// method name) — derived from Entity.Qualified's "<scope>#<Owner>.<Name>"
	// convention (see internal/parser/ts and internal/parser/golang's
	// entityFromMatch). This is what the receiver-type tier consults: given
	// a statically-known receiver type ("UserRepository") and a member name
	// ("findByEmail"), find the method directly instead of falling back to
	// an unscoped bare-name lookup that could match a same-named method on
	// an unrelated class.
	methodsByOwner map[string]map[string]model.EntityID
	// reExports is this file's barrel re-exports (`export * from`,
	// `export { a as b } from`) — TypeScript-specific (Go has no
	// barrel-file concept), consulted only by lang_ts.go.
	reExports []model.ReExport
}

// Index accumulates every file's facts before resolution starts — the
// resolver needs the whole repo's entity set to answer "does this name
// exist anywhere?" (used to distinguish DispositionBugExtractor from
// DispositionExternalUnknown), so resolution happens in a second pass
// after every file has been added.
type Index struct {
	repo string
	// files is keyed by the file path exactly as extractors report it —
	// slash-normalized, repo-relative (e.g. "src/services/userService.ts").
	files map[string]*fileEntry
	// byBareName is the repo-wide index used only to distinguish "this name
	// doesn't exist anywhere" (a stronger signal for bug-extractor) from
	// "it exists but we won't bind to it" (ambiguous).
	byBareName map[string][]model.EntityID
	// filesByDir groups every registered file's key by its repo-relative
	// directory. Generic infrastructure, not Go-specific — any
	// directory-scoped language's LanguagePolicy can use it (today, only
	// Go's does, via SameScopeFiles).
	filesByDir map[string][]string
	// policies holds one LanguagePolicy per model.Lang, registered via
	// RegisterPolicy. A ref whose file's language has no registered policy
	// resolves only through the language-agnostic tiers (same-file,
	// builtins n/a, disposition falls through to DispositionUnclassified)
	// — see resolveQualified/resolveUnqualified's nil-policy handling.
	policies map[model.Lang]LanguagePolicy
}

// NewIndex creates an empty resolver index for repo, with no languages
// registered yet — call RegisterPolicy for each language this index run
// should resolve. An unregistered language's refs still get same-file
// resolution (language-agnostic) but no same-scope/import/receiver-type/
// builtin tiers, which is the intended behavior for a deliberately
// disabled language (see internal/index's language-selection docs): a
// lighter run, not a broken one.
func NewIndex(repo string) *Index {
	return &Index{
		repo:       repo,
		files:      map[string]*fileEntry{},
		byBareName: map[string][]model.EntityID{},
		filesByDir: map[string][]string{},
		policies:   map[model.Lang]LanguagePolicy{},
	}
}

// RegisterPolicy installs a language's resolution policy. Call once per
// language before Resolve; calling it twice for the same model.Lang is a
// wiring bug (internal/index registering the same language twice) and
// panics rather than silently using whichever call happened last.
func (idx *Index) RegisterPolicy(p LanguagePolicy) {
	lang := p.Lang()
	if _, exists := idx.policies[lang]; exists {
		panic(fmt.Sprintf("resolve: policy for language %q registered twice", lang))
	}
	idx.policies[lang] = p
}

// AddFile registers one file's extracted facts. Must be called for every
// file before Resolve. Idempotent: re-adding a file already present (an
// incremental update, ADR-0020) first removes its old registration
// (RemoveFile) so byBareName/filesByDir never accumulate stale or
// duplicate entries from a file's previous version — the caller does not
// need to call RemoveFile itself first.
func (idx *Index) AddFile(facts *model.FileFacts) {
	if _, exists := idx.files[facts.File]; exists {
		idx.RemoveFile(facts.File)
	}
	dir := path.Dir(facts.File)
	fe := &fileEntry{
		lang:           facts.Lang,
		dir:            dir,
		entities:       facts.Entities,
		byName:         map[string][]model.EntityID{},
		imports:        facts.Imports,
		reExports:      facts.ReExports,
		methodsByOwner: map[string]map[string]model.EntityID{},
	}
	idx.filesByDir[dir] = append(idx.filesByDir[dir], facts.File)
	for _, e := range facts.Entities {
		// KindTest entities are named from a STRING LITERAL argument
		// (`describe("UserService", ...)`) — never a real language
		// binding, unlike every other entity kind. Indexing them
		// alongside real symbols let a test label silently shadow a real
		// import of the same name: `tests/userService.test.ts` both
		// declares a "UserService" test (from `describe("UserService", ...)`)
		// and imports the real `UserService` class, and `new UserService()`
		// inside it was resolving to the TEST, not the class — found by
		// this file's own precision measurement (internal/index/precision_test.go)
		// before it ever reached a real repo. Test entities stay fully
		// findable via facts.Entities/snap.All() (ctx find/inspect still
		// work), just excluded from the resolver's name indices.
		if e.Kind == model.KindTest {
			continue
		}
		fe.byName[e.Name] = append(fe.byName[e.Name], e.ID)
		idx.byBareName[e.Name] = append(idx.byBareName[e.Name], e.ID)

		if e.Kind != model.KindMethod {
			continue
		}
		// Qualified is "<scope>#<Owner>.<Name>" for methods that have an
		// owner (class methods, methodassign) — see entityFromMatch. A
		// method with no owner in its qualified name (shouldn't happen in
		// practice, but defensively skipped rather than mis-indexed)
		// simply doesn't participate in receiver-type lookups.
		hashIdx := strings.IndexByte(e.Qualified, '#')
		if hashIdx < 0 {
			continue
		}
		rest := e.Qualified[hashIdx+1:]
		dotIdx := strings.LastIndexByte(rest, '.')
		if dotIdx < 0 {
			continue
		}
		owner := rest[:dotIdx]
		if fe.methodsByOwner[owner] == nil {
			fe.methodsByOwner[owner] = map[string]model.EntityID{}
		}
		fe.methodsByOwner[owner][e.Name] = e.ID
	}
	idx.files[facts.File] = fe
}

// HasFile reports whether file is currently registered (via AddFile, and
// not since removed by RemoveFile) — used by incremental indexing
// (ADR-0020, internal/index.Indexer) to distinguish a brand-new file from
// one merely being re-added with unchanged content.
func (idx *Index) HasFile(file string) bool {
	_, ok := idx.files[file]
	return ok
}

// FileCount returns how many files are currently registered — the "Files"
// figure incremental indexing (ADR-0020) reports without needing a
// separate counter maintained alongside AddFile/RemoveFile.
func (idx *Index) FileCount() int {
	return len(idx.files)
}

// RemoveFile deletes file's registered facts — the counterpart to AddFile
// that makes it safe to call AddFile again for the same file (an
// incremental update, ADR-0020): every entity ID this file contributed is
// pruned from the repo-wide byBareName index, and file is removed from
// its directory's filesByDir list, before file itself is dropped from
// idx.files. A no-op if file was never added.
func (idx *Index) RemoveFile(file string) {
	fe, ok := idx.files[file]
	if !ok {
		return
	}
	for _, e := range fe.entities {
		if e.Kind == model.KindTest {
			continue // never indexed into byBareName in the first place — see AddFile's doc
		}
		idx.byBareName[e.Name] = removeEntityID(idx.byBareName[e.Name], e.ID)
		if len(idx.byBareName[e.Name]) == 0 {
			delete(idx.byBareName, e.Name)
		}
	}
	idx.filesByDir[fe.dir] = removeFilePath(idx.filesByDir[fe.dir], file)
	if len(idx.filesByDir[fe.dir]) == 0 {
		delete(idx.filesByDir, fe.dir)
	}
	delete(idx.files, file)
}

func removeEntityID(ids []model.EntityID, target model.EntityID) []model.EntityID {
	kept := ids[:0]
	for _, id := range ids {
		if id != target {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

func removeFilePath(files []string, target string) []string {
	kept := files[:0]
	for _, f := range files {
		if f != target {
			kept = append(kept, f)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	return kept
}

// Dependents returns every OTHER registered file whose resolution outcome
// could change if file's exports or content change: files that import it
// (directly, or via a barrel re-export chain) and — for a directory-
// scoped language like Go, via LanguagePolicy.SameScopeFiles — package
// siblings that share its unqualified-name scope. This is the "who else
// needs re-resolving" reverse lookup incremental indexing (ADR-0020)
// needs beyond the changed file itself.
//
// Implemented as a scan over every registered file's own (small) import
// table, not a maintained reverse index — the resolver keeps no such
// index today, and this scan is fast relative to the extraction/
// resolution work incremental indexing exists to avoid repeating. A real
// reverse-import index (O(1) instead of O(files)) is a legitimate future
// optimization if profiling ever shows this scan itself is the
// bottleneck — not attempted here without a measurement to justify it.
func (idx *Index) Dependents(file string) []string {
	seen := map[string]bool{file: true} // never report a file as its own dependent
	var out []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}

	for other, fe := range idx.files {
		if other == file {
			continue
		}
		policy := idx.policyFor(fe)
		if policy == nil {
			continue
		}
		for _, im := range fe.imports {
			if targets, ok := policy.ResolveImportTarget(idx, other, im.Source); ok {
				for _, t := range targets {
					if t == file {
						add(other)
					}
				}
			}
		}
		for _, re := range fe.reExports {
			if targets, ok := policy.ResolveImportTarget(idx, other, re.Source); ok {
				for _, t := range targets {
					if t == file {
						add(other)
					}
				}
			}
		}
	}

	if fe := idx.files[file]; fe != nil {
		if policy := idx.policyFor(fe); policy != nil {
			for _, sibling := range policy.SameScopeFiles(idx, file) {
				add(sibling)
			}
		}
	}

	return out
}

// Resolve resolves every ref of every file added so far, returning one
// model.ResolvedRef per input model.Ref. It is pure with respect to the
// index — call it after all files are added, not incrementally per file,
// since import-table resolution needs every target file to already be
// registered.
func (idx *Index) Resolve(facts []*model.FileFacts) []model.ResolvedRef {
	var out []model.ResolvedRef
	for _, f := range facts {
		fe := idx.files[f.File]
		for _, ref := range f.Refs {
			out = append(out, idx.resolveOne(f.File, fe, ref))
		}
	}
	return out
}

func (idx *Index) resolveOne(file string, fe *fileEntry, ref model.Ref) model.ResolvedRef {
	edgeKind, ok := edgeKindFor(ref.Kind)
	if !ok {
		return model.ResolvedRef{Disposition: model.DispositionUnclassified, Reason: fmt.Sprintf("unhandled ref kind %q", ref.Kind)}
	}

	switch ref.Target.Scope {
	case model.ScopeLocal:
		// Never cross-resolved by construction — see model.TargetScope's
		// doc and edge-case-backlog.md B4 (Grafel's #3936: a local sort-key
		// variable cross-resolving against an unrelated global). The
		// Go extractor emits this for closures/callback parameters called
		// bare (edge-case-backlog.md J2); no extractor emitted it before.
		return model.ResolvedRef{Disposition: model.DispositionUnclassified, Reason: "scope-local ref, never cross-resolved"}

	case model.ScopeQualified:
		// `obj.member()` / `this.prop.member()`. Only the import-table and
		// receiver-type tiers are implemented; a receiver whose static type
		// cannot be determined is the documented receiver-type gap.
		return idx.resolveQualified(file, fe, ref, edgeKind)

	default: // ScopeUnqualified (or ScopeSameFile, treated identically —
		// no extractor currently distinguishes them; same-file is simply
		// the first tier tried below regardless of the tag).
		return idx.resolveUnqualified(file, fe, ref, edgeKind)
	}
}

// policyFor returns fe's language policy, or nil if fe is nil or no
// policy was registered for its language (a deliberately disabled
// language — see NewIndex's doc).
func (idx *Index) policyFor(fe *fileEntry) LanguagePolicy {
	if fe == nil {
		return nil
	}
	return idx.policies[fe.lang]
}

func (idx *Index) resolveQualified(file string, fe *fileEntry, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	policy := idx.policyFor(fe)

	if fe != nil && policy != nil {
		for _, im := range fe.imports {
			if im.LocalName != ref.Target.Name || !im.IsNamespace {
				continue
			}
			return policy.ResolveQualifiedImport(idx, file, im, ref, kind)
		}
	}

	// Receiver-type tier: obj is not a namespace import, but the extractor
	// may still know its static type — a constructor-parameter-property, a
	// typed class field, a locally typed/constructed variable, or (Go) a
	// method's own receiver variable (see each language's receiver.*
	// query captures). This is what closes the largest resolver gap
	// (docs/research/edge-case-backlog.md B13): most qualified calls in
	// real OOP-style code (`this.repo.findByEmail()`, `r.repo.FindByID()`)
	// are exactly this shape.
	receiverType := ref.Target.ReceiverType
	if receiverType == "" && fe != nil {
		// obj itself might be a plain (non-namespace) imported class used
		// for a "static" call — `User.findById(...)` where User is a
		// Mongoose model imported by name. Found while validating against
		// a real repo: every DB access there is exactly this shape, not a
		// constructor-injected instance. Treating the imported name as its
		// own receiver type reuses resolveByReceiverType's existing
		// same-file/same-scope/cross-file lookup unchanged — it correctly
		// falls through (not a guess) when the member turns out to be
		// inherited from an unindexed library base class.
		for _, im := range fe.imports {
			if im.LocalName == ref.Target.Name && !im.IsNamespace {
				receiverType = ref.Target.Name
				break
			}
		}
	}
	if receiverType != "" {
		if id, ok := idx.resolveByReceiverType(file, fe, receiverType, ref.Target.Member); ok {
			return resolvedEdge(ref.Src, kind, id, 0.85, model.ProvenanceInferred,
				fmt.Sprintf("receiver type %s (statically declared), member %s", receiverType, ref.Target.Member))
		}
	}

	if receiverType != "" {
		// We DID determine obj's type, but Member isn't one of ITS
		// entities either same-scope or via import. Found validating
		// against a real repo: `User.findById(...)` where User is a real,
		// known Mongoose model, but findById is inherited from Mongoose's
		// own Model base class — never in scope to index (no
		// node_modules/vendor resolution). This is meaningfully different
		// from not knowing the type at all: it is presumed external (a
		// library base-class method), not an unimplemented resolver tier —
		// DispositionUnimplemented would wrongly suggest the resolver just
		// hasn't tried yet, when it tried and found a real, specific
		// reason to give up.
		return model.ResolvedRef{
			Disposition: model.DispositionExternalUnknown,
			Reason:      fmt.Sprintf("receiver type %s is known, but member %s was not found on it — presumed inherited from an unindexed base class/library", receiverType, ref.Target.Member),
		}
	}

	// obj's type is unknown: a local variable with no type annotation and
	// no constructor initializer, a destructured parameter, or any other
	// shape no language's receiver-type signals cover. This remains an
	// explicit, documented gap (see package doc), not a bug — full type
	// inference (through function calls, generics, unions) is out of
	// scope; only statically-annotated/obviously-typed receivers are
	// covered.
	return model.ResolvedRef{
		Disposition: model.DispositionUnimplemented,
		Reason:      fmt.Sprintf("qualified call %s.%s: receiver type unknown (no type annotation, import, or constructor initializer found)", ref.Target.Name, ref.Target.Member),
	}
}

// resolveByReceiverType looks up member as a method of the class/struct
// named receiverType: first in the current file, then across the file's
// LanguagePolicy-defined same-scope files (Go's package siblings; a no-op
// for TypeScript), then — since receiverType may itself be an imported
// name — via the current file's import table (FollowImportToMethods).
// Returns ok=false if the type isn't known or has no such method, letting
// the caller fall through to DispositionUnimplemented rather than guess.
func (idx *Index) resolveByReceiverType(file string, fe *fileEntry, receiverType, member string) (model.EntityID, bool) {
	if fe == nil {
		return "", false
	}
	if byMethod, ok := fe.methodsByOwner[receiverType]; ok {
		if id, ok := byMethod[member]; ok {
			return id, true
		}
	}
	if policy := idx.policyFor(fe); policy != nil {
		for _, sibling := range policy.SameScopeFiles(idx, file) {
			sf := idx.files[sibling]
			if sf == nil {
				continue
			}
			if byMethod, ok := sf.methodsByOwner[receiverType]; ok {
				if id, ok := byMethod[member]; ok {
					return id, true
				}
			}
		}
		if methods, ok := policy.FollowImportToMethods(idx, file, fe, receiverType); ok {
			if id, ok := methods[member]; ok {
				return id, true
			}
		}
	}
	return "", false
}

func (idx *Index) resolveUnqualified(file string, fe *fileEntry, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	policy := idx.policyFor(fe)

	// Tier 1: same-file, then same-scope (a directory-scoped language's
	// package siblings — a no-op set for a one-module-per-file language).
	// Unified into one tier: collect every candidate across {file} ∪
	// policy.SameScopeFiles(file), resolve only if exactly one total
	// candidate exists — same "don't guess" rule either way.
	if fe != nil {
		ids := append([]model.EntityID{}, fe.byName[name]...)
		if policy != nil {
			for _, sibling := range policy.SameScopeFiles(idx, file) {
				if sf := idx.files[sibling]; sf != nil {
					ids = append(ids, sf.byName[name]...)
				}
			}
		}
		if len(ids) == 1 {
			return resolvedEdge(ref.Src, kind, ids[0], 0.95, model.ProvenanceDeterministic, "same-file/same-scope declaration")
		} else if len(ids) > 1 {
			return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: ids, Reason: "multiple same-file/same-scope declarations with this name"}
		}
	}

	// Tier 2: import table (a no-op for a language where every import is
	// namespace-style, e.g. Go — see LanguagePolicy.ResolveUnqualifiedImport's doc).
	if fe != nil && policy != nil {
		for _, im := range fe.imports {
			if im.LocalName != name || im.IsNamespace {
				continue
			}
			return policy.ResolveUnqualifiedImport(idx, file, im, ref, kind)
		}
	}

	// Tier 3: known globals/builtins — a fixed list of identifiers that
	// exist in every file of the language and would otherwise flood
	// bug_rate.
	if policy != nil && policy.IsBuiltin(name) {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("%s builtin/runtime global", fe.lang)}
	}

	// Tier 4: language-specific final disposition — TypeScript's bare-name
	// allowlist (can still resolve an edge) plus "presumed external"
	// default; Go's "presumed a missed extraction" default. A ref whose
	// language has no registered policy (deliberately disabled — see
	// NewIndex's doc) falls through to DispositionUnclassified rather than
	// guessing at a policy that was never wired in.
	if policy != nil {
		return policy.FinalDisposition(idx, ref, kind)
	}
	return model.ResolvedRef{Disposition: model.DispositionUnclassified,
		Reason: fmt.Sprintf("no LanguagePolicy registered for %q (language deliberately disabled for this run)", fe.lang)}
}

func edgeKindFor(k model.RefKind) (model.EdgeKind, bool) {
	switch k {
	case model.RefCall:
		return model.EdgeCalls, true
	case model.RefExtends:
		return model.EdgeExtends, true
	case model.RefImplements:
		return model.EdgeImplements, true
	case model.RefTypeUse:
		return model.EdgeUses, true
	}
	return "", false
}

func resolvedEdge(src model.EntityID, kind model.EdgeKind, dst model.EntityID, confidence float32, prov model.Provenance, evidence string) model.ResolvedRef {
	edge := &model.Edge{
		ID:         edgeID(kind, dst, evidence),
		Src:        src,
		Dst:        dst,
		Kind:       kind,
		Confidence: confidence,
		Provenance: prov,
		Evidence:   evidence,
	}
	return model.ResolvedRef{Disposition: model.DispositionResolved, Edge: edge}
}

func edgeID(kind model.EdgeKind, dst model.EntityID, evidence string) string {
	h := sha256.Sum256([]byte(string(kind) + "\x00" + string(dst) + "\x00" + evidence))
	return hex.EncodeToString(h[:])[:16]
}
