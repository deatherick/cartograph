// Package resolve turns the unresolved model.Refs an extractor produces
// into model.Edges or model.Dispositions, following the fixed pipeline
// documented in docs/research/03-import-resolution-and-bare-names.md:
//
//	same-file → import-table → receiver-type → bare-name(allowlist) → disposition
//
// PHASE 1 SCOPE GAP (documented, not hidden): the receiver-type tier is not
// implemented. Resolving `obj.method()` where obj is a local variable (not
// an import) requires knowing obj's static type, which needs a type
// checker Phase 1 does not have. Those refs get DispositionUnimplemented,
// which does NOT count toward bug_rate — it is a known scope gap, not a
// defect. See docs/adr/0003-data-model.md and the Phase 1 exit criteria in
// the project plan.
//
// Every other tier is fully implemented and follows Grafel's own hard-won
// policy: whitelisting is safer than blacklisting (docs/research/03) — a
// bare name never auto-resolves just because exactly one candidate exists
// repo-wide; it must be on the (starter, near-empty) allowlist.
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
	dir      string // repo-relative directory, used for Go's same-package tier
	entities []model.Entity
	byName   map[string][]model.EntityID // bare name -> entities defined in THIS file
	imports  []model.ImportBinding
	// methodsByOwner indexes Method-kind entities by (owning class name,
	// method name) — derived from Entity.Qualified's "file#Owner.Name"
	// convention (see internal/parser/ts's entityFromMatch). This is what
	// the receiver-type tier consults: given a statically-known receiver
	// type ("UserRepository") and a member name ("findByEmail"), find the
	// method directly instead of falling back to an unscoped bare-name
	// lookup that could match a same-named method on an unrelated class.
	methodsByOwner map[string]map[string]model.EntityID
	// reExports is this file's barrel re-exports (`export * from`,
	// `export { a as b } from`) — see findExportedEntity, which follows
	// these when a name isn't found directly in a file (ADR-0013's
	// re-export discussion, docs/research/03).
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
	// tsconfig is optional path-alias configuration (baseUrl/paths from
	// tsconfig.json), set via SetTSConfig. Zero value disables alias
	// resolution — every source is treated as either repo-relative
	// (starts with ".") or an external package, exactly as before this
	// existed.
	tsconfig TSConfig
	// goModule is the Go module path from go.mod (e.g.
	// "github.com/deatherick/cartograph"), used to map an import path to a
	// repo-relative directory — Go's analog of TSConfig's baseUrl/paths.
	// Empty disables Go import resolution (every Go import then resolves as
	// external, never internal).
	goModule string
	// filesByDir groups every registered file's key by its repo-relative
	// directory — the unit a Go package actually is (one or more files, one
	// directory), unlike TypeScript's one-module-per-file model. Populated
	// in AddFile, consulted by the Go same-package resolution tier and by
	// findExportedEntityGo.
	filesByDir map[string][]string
}

// TSConfig is the subset of tsconfig.json's compilerOptions the resolver
// uses for path-alias resolution (docs/research/03's ADR-explicit Phase 1
// scope item: "tsconfig paths, baseUrl"). Populated by internal/index,
// which reads the file from disk — the resolver itself does no file I/O,
// staying testable against synthetic facts.
type TSConfig struct {
	BaseURL string
	// Paths maps a pattern (e.g. "@/*") to one or more target templates
	// (e.g. []string{"src/*"}), matching tsconfig.json's own shape. Only
	// single-wildcard patterns are supported — the overwhelmingly common
	// case; multi-segment/regex-like alias patterns are a documented gap.
	Paths map[string][]string
}

// NewIndex creates an empty resolver index for repo.
func NewIndex(repo string) *Index {
	return &Index{
		repo:       repo,
		files:      map[string]*fileEntry{},
		byBareName: map[string][]model.EntityID{},
		filesByDir: map[string][]string{},
	}
}

// SetTSConfig installs path-alias configuration. Optional — call before
// Resolve if the repo has a tsconfig.json with baseUrl/paths.
func (idx *Index) SetTSConfig(cfg TSConfig) {
	idx.tsconfig = cfg
}

// SetGoModule installs the Go module path (from go.mod) used to map an
// import path to a repo-relative directory. Optional — call before Resolve
// if the repo has a go.mod; without it, every Go import resolves as
// external (correctly conservative: an import cannot be presumed internal
// without knowing the module path it would have to match).
func (idx *Index) SetGoModule(modulePath string) {
	idx.goModule = modulePath
}

// AddFile registers one file's extracted facts. Must be called for every
// file before Resolve — the resolver has no notion of incremental
// per-file resolution yet (that arrives with Phase 3's incremental index).
func (idx *Index) AddFile(facts *model.FileFacts) {
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
		// Qualified is "<file>#<Owner>.<Name>" for methods that have an
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
		// variable cross-resolving against an unrelated global). V0's
		// extractor does not currently emit ScopeLocal refs, but the
		// pipeline handles it correctly if a future extractor does.
		return model.ResolvedRef{Disposition: model.DispositionUnclassified, Reason: "scope-local ref, never cross-resolved"}

	case model.ScopeQualified:
		// `obj.member()` / `this.prop.member()`. Only the import-table
		// tier is implemented (obj is a namespace import). Anything else
		// is the documented receiver-type gap.
		return idx.resolveQualified(file, fe, ref, edgeKind)

	default: // ScopeUnqualified (or ScopeSameFile, treated identically —
		// the extractor does not yet distinguish them; same-file is simply
		// the first tier tried below regardless of the tag).
		return idx.resolveUnqualified(file, fe, ref, edgeKind)
	}
}

func (idx *Index) resolveQualified(file string, fe *fileEntry, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	if fe != nil {
		for _, im := range fe.imports {
			if im.LocalName != ref.Target.Name || !im.IsNamespace {
				continue
			}
			if fe.lang == model.LangGo {
				// Every Go import is namespace-style (see
				// internal/parser/golang's importFromMatch doc) and Go
				// packages span a directory, not a single file — resolved
				// via the module-path mapping, not TS's relative-path +
				// extension-candidate scheme.
				return idx.resolveGoQualifiedImport(ref, im, kind)
			}
			targetFile, found := idx.resolveImportPath(file, im.Source)
			if !found {
				return externalDisposition(im.Source)
			}
			if id, ok := idx.findExportedEntity(targetFile, ref.Target.Member); ok {
				return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
					fmt.Sprintf("namespace import %s from %q, member %s", ref.Target.Name, im.Source, ref.Target.Member))
			}
			return model.ResolvedRef{Disposition: model.DispositionBugResolver,
				Reason: fmt.Sprintf("namespace member %s.%s not found in resolved file %s (including any barrel re-exports)", ref.Target.Name, ref.Target.Member, targetFile)}
		}
	}

	// Receiver-type tier: obj is not a namespace import, but the extractor
	// may still know its static type — a constructor-parameter-property,
	// a typed class field, or a locally typed/`new`-initialized variable
	// (see internal/parser/ts's receiver.* query captures). This is what
	// closes the largest Phase 1 resolver gap (docs/research/edge-case-backlog.md
	// B13): most qualified calls in real OOP-style code
	// (`this.repo.findByEmail()`) are exactly this shape.
	receiverType := ref.Target.ReceiverType
	if receiverType == "" && fe != nil {
		// obj itself might be a plain (non-namespace) imported class used
		// for a "static" call — `User.findById(...)` where User is a
		// Mongoose model imported by name. Found while re-validating this
		// tier against the real repo (typescript-node-express-realworld-
		// example-app): every DB access there is exactly this shape, not
		// a constructor-injected instance. Treating the imported name as
		// its own receiver type reuses resolveByReceiverType's existing
		// same-file/cross-file lookup unchanged — it correctly falls
		// through (not a guess) when the member turns out to be inherited
		// from an unindexed library base class (e.g. Mongoose's own
		// `Model.findById`), which is the common case for this pattern.
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
		// entities either same-file or via import. Found validating
		// against the real repo: `User.findById(...)` where User is a
		// real, known Mongoose model, but findById is inherited from
		// Mongoose's own Model base class — never in scope to index (no
		// node_modules resolution in Phase 1). This is meaningfully
		// different from not knowing the type at all: it is presumed
		// external (a library base-class method), not an unimplemented
		// resolver tier — DispositionUnimplemented would wrongly suggest
		// the resolver just hasn't tried yet, when it tried and found a
		// real, specific reason to give up.
		return model.ResolvedRef{
			Disposition: model.DispositionExternalUnknown,
			Reason:      fmt.Sprintf("receiver type %s is known, but member %s was not found on it — presumed inherited from an unindexed base class/library", receiverType, ref.Target.Member),
		}
	}

	// obj's type is unknown: a local variable with no type annotation and
	// no `new` initializer, a destructured parameter, or any other shape
	// the extractor's receiver-type signals don't cover. This remains an
	// explicit, documented Phase 1 gap (see package doc), not a bug —
	// full type inference (through function calls, generics, unions) is
	// out of scope; only statically-annotated/obviously-typed receivers
	// are covered.
	return model.ResolvedRef{
		Disposition: model.DispositionUnimplemented,
		Reason:      fmt.Sprintf("qualified call %s.%s: receiver type unknown (no type annotation, import, or `new` initializer found)", ref.Target.Name, ref.Target.Member),
	}
}

// resolveByReceiverType looks up member as a method of the class named
// receiverType, first in the current file, then — since receiverType may
// itself be an imported class name — via the current file's import table.
// Returns ok=false if the type isn't a known class or has no such method,
// letting the caller fall through to DispositionUnimplemented rather than
// guess.
func (idx *Index) resolveByReceiverType(file string, fe *fileEntry, receiverType, member string) (model.EntityID, bool) {
	if fe == nil {
		return "", false
	}
	if byMethod, ok := fe.methodsByOwner[receiverType]; ok {
		if id, ok := byMethod[member]; ok {
			return id, true
		}
	}
	if fe.lang == model.LangGo {
		// Go's struct/method split across sibling files in one package
		// directory (unlike TypeScript, where a class and its methods are
		// always in the same file) — receiverType may be declared in a
		// DIFFERENT file of the same package, never reached via fe's own
		// methodsByOwner nor via an import (there is none: same-package
		// references are never imported in Go).
		for _, siblingFile := range idx.filesByDir[fe.dir] {
			if siblingFile == file {
				continue // already checked via fe.methodsByOwner above
			}
			sibling := idx.files[siblingFile]
			if sibling == nil {
				continue
			}
			if byMethod, ok := sibling.methodsByOwner[receiverType]; ok {
				if id, ok := byMethod[member]; ok {
					return id, true
				}
			}
		}
	}
	for _, im := range fe.imports {
		if im.LocalName != receiverType || im.IsNamespace {
			continue
		}
		targetFile, found := idx.resolveImportPath(file, im.Source)
		if !found {
			continue
		}
		target := idx.files[targetFile]
		if target == nil {
			continue
		}
		exported := im.ImportedName
		if im.IsDefault {
			exported = receiverType
		}
		if byMethod, ok := target.methodsByOwner[exported]; ok {
			if id, ok := byMethod[member]; ok {
				return id, true
			}
		}
	}
	return "", false
}

func (idx *Index) resolveUnqualified(file string, fe *fileEntry, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name

	// Tier 1: same-file.
	if fe != nil {
		if ids := fe.byName[name]; len(ids) == 1 {
			return resolvedEdge(ref.Src, kind, ids[0], 0.95, model.ProvenanceDeterministic, "same-file declaration")
		} else if len(ids) > 1 {
			return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: ids, Reason: "multiple same-file declarations with this name"}
		}
	}

	// Tier 1.5 (Go only): same-package. A Go package spans every file in
	// one directory — an unqualified reference to a sibling file's
	// declaration is the normal, common case (unlike TypeScript, where
	// every file is its own module and this tier does not exist).
	if fe != nil && fe.lang == model.LangGo {
		if id, ok := idx.findExportedEntityGo(fe.dir, name); ok {
			return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic, "same-package declaration")
		}
	}

	// Tier 2: import table.
	if fe != nil {
		for _, im := range fe.imports {
			if im.LocalName != name || im.IsNamespace {
				continue
			}
			targetFile, found := idx.resolveImportPath(file, im.Source)
			if !found {
				return externalDisposition(im.Source)
			}
			exported := im.ImportedName
			if im.IsDefault {
				// V0 does not track which entity a file's `export
				// default` names — a documented simplification. Fall
				// through to matching by local name, the common case
				// where the default export shares its declared name.
				exported = name
			}
			if id, ok := idx.findExportedEntity(targetFile, exported); ok {
				return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
					fmt.Sprintf("import %s from %q", name, im.Source))
			}
			return model.ResolvedRef{Disposition: model.DispositionBugResolver,
				Reason: fmt.Sprintf("import %s from %q resolved to file %s but no matching export found (including any barrel re-exports)", name, im.Source, targetFile)}
		}
	}

	// Tier 3: known globals/builtins — a fixed list of identifiers that
	// exist in every file of the language and would otherwise flood
	// bug_rate. Go's predeclared identifiers (len, make, panic, error, ...)
	// are a disjoint list from TS/JS's runtime globals (console, Promise,
	// ...); which one applies is decided by the file's language, not the
	// name's spelling — the two lists happen to share zero entries today,
	// but that is not guaranteed to stay true forever.
	isGo := fe != nil && fe.lang == model.LangGo
	if isGo && goBuiltins[name] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: "Go predeclared identifier"}
	}
	if !isGo && knownGlobals[name] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: "known JS/TS runtime global"}
	}

	if isGo {
		// Go has no bare-name allowlist tier: unlike TypeScript/JavaScript,
		// where an unqualified identifier CAN legitimately be an implicit
		// global with no local declaration, Go's static resolution rules
		// mean a bare identifier used as a call target must be either a
		// predeclared builtin (just checked above) or declared somewhere
		// in the current package (same-file/same-package tiers, already
		// tried above) — there is no third option short of a rare dot
		// import (a documented, deliberately unsupported gap; see
		// internal/parser/golang's import.stmt query doc). Reaching here
		// therefore means the extractor missed a real declaration, not
		// that the name is "presumed external" the way TS/JS treats an
		// unresolved bare name.
		if candidates := idx.byBareName[name]; len(candidates) > 0 {
			// The name exists somewhere in the repo, just not in this
			// package — evidence for a human/agent to look at, not a
			// confident bind (docs/research/03's "never guess" rule).
			return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: candidates,
				Reason: fmt.Sprintf("bare name %q not found in this package; %d repo-wide candidate(s) elsewhere", name, len(candidates))}
		}
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("bare Go identifier %q not found in this file, its package, the predeclared identifiers, or anywhere else in the repo — Go's static resolution rules mean this should not be possible unless the extractor missed a declaration (or this is a rare dot-import, an unsupported gap)", name)}
	}

	// Tier 4 (TS/JS only): bare-name allowlist policy (docs/research/03).
	// Whitelisting, never blacklisting: a name is bound bare ONLY if
	// explicitly allowlisted, regardless of how many repo-wide candidates
	// exist.
	candidates := idx.byBareName[name]
	if bareNameAllowlist[name] && len(candidates) == 1 {
		return resolvedEdge(ref.Src, kind, candidates[0], 0.7, model.ProvenanceInferred, "bare-name allowlist match")
	}
	if len(candidates) > 0 {
		return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: candidates,
			Reason: fmt.Sprintf("bare name %q has %d repo-wide candidate(s) but is not allowlisted for bare resolution", name, len(candidates))}
	}
	if bareNameExclusions[name] {
		return model.ResolvedRef{Disposition: model.DispositionExternalUnknown,
			Reason: fmt.Sprintf("generic bare name %q, no repo-wide candidates, excluded from bare resolution by policy", name)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown,
		Reason: fmt.Sprintf("bare name %q, no repo-wide candidates — presumed external (no node_modules resolution in Phase 1)", name)}
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

// externalDisposition classifies an import whose source could not be
// resolved to a known repo file. A repo-relative path (./ or ../) that
// fails to resolve is a stronger signal — it should be one of ours — so it
// is BugExtractor rather than ExternalUnknown; anything else (a bare
// package specifier like "express") is presumed a real external dependency.
func externalDisposition(source string) model.ResolvedRef {
	if strings.HasPrefix(source, ".") {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("repo-relative import %q did not resolve to any indexed file", source)}
	}
	if knownPackages[source] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("known external package %q", source)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown, Reason: fmt.Sprintf("unrecognized external package %q", source)}
}

// resolveGoQualifiedImport resolves a package-qualified Go call/type-use
// (`pkg.Member`) through im, the import binding whose LocalName matched the
// call's object identifier. Mirrors resolveQualified's TS namespace-import
// branch, but against a Go package (a directory of files), not a single
// resolved file.
func (idx *Index) resolveGoQualifiedImport(ref model.Ref, im model.ImportBinding, kind model.EdgeKind) model.ResolvedRef {
	dir, internal, found := idx.resolveGoImportPath(im.Source)
	if !internal {
		return goExternalDisposition(im.Source)
	}
	if !found {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("go import %q is under this module but no indexed file was found in directory %q", im.Source, dir)}
	}
	if id, ok := idx.findExportedEntityGo(dir, ref.Target.Member); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("package import %s (%s), member %s", ref.Target.Name, im.Source, ref.Target.Member))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("package %s (%q) has no member %s (this extractor is not yet export-aware — an unexported name in that package would also report this)", ref.Target.Name, im.Source, ref.Target.Member)}
}

// resolveGoImportPath maps a Go import path to a repo-relative directory
// using idx.goModule (from go.mod). internal reports whether importPath is
// under this module at all (false for stdlib/third-party paths, which the
// caller routes to goExternalDisposition instead); found reports whether
// that directory actually has any indexed file (false is a strong bug
// signal — an internal import that resolves to nothing this index walked).
func (idx *Index) resolveGoImportPath(importPath string) (dir string, internal bool, found bool) {
	if idx.goModule == "" {
		return "", false, false
	}
	switch {
	case importPath == idx.goModule:
		dir = "."
	case strings.HasPrefix(importPath, idx.goModule+"/"):
		dir = strings.TrimPrefix(importPath, idx.goModule+"/")
	default:
		return "", false, false
	}
	_, found = idx.filesByDir[dir]
	return dir, true, found
}

// findExportedEntityGo looks up name across every file in a Go package
// directory — the unit Go actually resolves names within, unlike
// TypeScript's per-file findExportedEntity. No re-export chasing: Go has no
// barrel-file concept. Returns ok=false for zero or (in valid, compiling Go
// source, which should not happen) more than one match, the same
// conservative "don't guess" rule findExportedEntity follows.
func (idx *Index) findExportedEntityGo(dir, name string) (model.EntityID, bool) {
	var match model.EntityID
	count := 0
	for _, file := range idx.filesByDir[dir] {
		fe := idx.files[file]
		if fe == nil {
			continue
		}
		for _, id := range fe.byName[name] {
			match = id
			count++
		}
	}
	if count == 1 {
		return match, true
	}
	return "", false
}

// goExternalDisposition classifies a Go import path that resolveGoImportPath
// determined is NOT under this module. A first path segment with no dot is
// Go's own convention for a standard-library import ("fmt", "encoding/json",
// "net/http" — a real third-party path always starts with a domain like
// "github.com"), so it is ExternalKnown without needing an allowlist entry;
// anything else is checked against goKnownPackages (this project's own real
// dependencies), else ExternalUnknown.
func goExternalDisposition(importPath string) model.ResolvedRef {
	first := importPath
	if i := strings.IndexByte(importPath, '/'); i >= 0 {
		first = importPath[:i]
	}
	if !strings.Contains(first, ".") {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("Go standard library package %q", importPath)}
	}
	if goKnownPackages[importPath] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("known external Go package %q", importPath)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown, Reason: fmt.Sprintf("unrecognized external Go package %q", importPath)}
}

// resolveImportPath maps an import source to a registered file key, trying
// the same candidate suffixes a TS/Node resolver would (implicit
// extension, then index-file resolution). Handles three source shapes:
//   - repo-relative ("./x", "../x") — resolved against fromFile's directory
//   - a tsconfig path alias ("@/services/x") — resolved via idx.tsconfig,
//     the Phase 1 scope item docs/research/03 lists ("tsconfig paths,
//     baseUrl") and edge-case-backlog.md C1
//   - anything else (a bare package specifier) — found=false; bare
//     specifiers are never files in this index
func (idx *Index) resolveImportPath(fromFile, source string) (string, bool) {
	var base string
	switch {
	case strings.HasPrefix(source, "."):
		base = path.Join(path.Dir(fromFile), source)
	default:
		aliased, ok := idx.resolveTSConfigAlias(source)
		if !ok {
			return "", false
		}
		base = aliased
	}
	candidates := []string{
		base + ".ts",
		base + ".tsx",
		path.Join(base, "index.ts"),
		path.Join(base, "index.tsx"),
	}
	for _, c := range candidates {
		if _, ok := idx.files[c]; ok {
			return c, true
		}
	}
	return "", false
}

// resolveTSConfigAlias maps a non-relative import source through
// idx.tsconfig's paths/baseUrl, tsconfig.json's own alias mechanism.
// Only single-wildcard patterns are supported (the overwhelmingly common
// case, e.g. "@/*": ["src/*"]) — see TSConfig's doc for that scoping.
func (idx *Index) resolveTSConfigAlias(source string) (string, bool) {
	if idx.tsconfig.BaseURL == "" && len(idx.tsconfig.Paths) == 0 {
		return "", false
	}
	for pattern, targets := range idx.tsconfig.Paths {
		star := strings.IndexByte(pattern, '*')
		if star < 0 {
			if pattern != source {
				continue
			}
			for _, target := range targets {
				return path.Join(idx.tsconfig.BaseURL, target), true
			}
			continue
		}
		prefix := pattern[:star]
		suffix := pattern[star+1:]
		if !strings.HasPrefix(source, prefix) || !strings.HasSuffix(source, suffix) {
			continue
		}
		matched := source[len(prefix) : len(source)-len(suffix)]
		for _, target := range targets {
			resolved := strings.Replace(target, "*", matched, 1)
			return path.Join(idx.tsconfig.BaseURL, resolved), true
		}
	}
	// No explicit paths entry matched, but TS also resolves bare
	// specifiers directly against baseUrl when one is set.
	if idx.tsconfig.BaseURL != "" {
		return path.Join(idx.tsconfig.BaseURL, source), true
	}
	return "", false
}

// findExportedEntity looks up name as an entity exported by file, either
// declared directly or reached through a chain of barrel re-exports
// (`export * from`, `export { a as b } from` — ADR-0013's discussion in
// docs/research/03-import-resolution-and-bare-names.md, edge-case-backlog.md
// C4). Depth-limited and cycle-safe: a re-export chain longer than
// maxReExportDepth, or one that revisits a file, stops rather than loops.
func (idx *Index) findExportedEntity(file, name string) (model.EntityID, bool) {
	return idx.findExportedEntityDepth(file, name, 0, map[string]bool{})
}

const maxReExportDepth = 4

func (idx *Index) findExportedEntityDepth(file, name string, depth int, visited map[string]bool) (model.EntityID, bool) {
	target := idx.files[file]
	if target == nil {
		return "", false
	}
	if ids := target.byName[name]; len(ids) == 1 {
		return ids[0], true
	}
	if depth >= maxReExportDepth || visited[file] {
		return "", false
	}
	visited[file] = true

	for _, re := range target.reExports {
		nextFile, found := idx.resolveImportPath(file, re.Source)
		if !found {
			continue
		}
		if re.IsStar {
			if id, ok := idx.findExportedEntityDepth(nextFile, name, depth+1, visited); ok {
				return id, true
			}
			continue
		}
		if re.LocalAlias == name {
			if id, ok := idx.findExportedEntityDepth(nextFile, re.ExportedName, depth+1, visited); ok {
				return id, true
			}
		}
	}
	return "", false
}
