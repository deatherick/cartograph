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
// same-file resolution) and its import table (for import-table
// resolution).
type fileEntry struct {
	entities   []model.Entity
	byName     map[string][]model.EntityID // bare name -> entities defined in THIS file
	imports    []model.ImportBinding
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
}

// NewIndex creates an empty resolver index for repo.
func NewIndex(repo string) *Index {
	return &Index{
		repo:       repo,
		files:      map[string]*fileEntry{},
		byBareName: map[string][]model.EntityID{},
	}
}

// AddFile registers one file's extracted facts. Must be called for every
// file before Resolve — the resolver has no notion of incremental
// per-file resolution yet (that arrives with Phase 3's incremental index).
func (idx *Index) AddFile(facts *model.FileFacts) {
	fe := &fileEntry{
		entities: facts.Entities,
		byName:   map[string][]model.EntityID{},
		imports:  facts.Imports,
	}
	for _, e := range facts.Entities {
		fe.byName[e.Name] = append(fe.byName[e.Name], e.ID)
		idx.byBareName[e.Name] = append(idx.byBareName[e.Name], e.ID)
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
			targetFile, found := resolveImportPath(file, im.Source, idx.files)
			if !found {
				return externalDisposition(im.Source)
			}
			target := idx.files[targetFile]
			if ids := target.byName[ref.Target.Member]; len(ids) == 1 {
				return resolvedEdge(ref.Src, kind, ids[0], 0.95, model.ProvenanceDeterministic,
					fmt.Sprintf("namespace import %s from %q, member %s", ref.Target.Name, im.Source, ref.Target.Member))
			}
			return model.ResolvedRef{Disposition: model.DispositionBugResolver,
				Reason: fmt.Sprintf("namespace member %s.%s not found in resolved file %s", ref.Target.Name, ref.Target.Member, targetFile)}
		}
	}
	// obj is not a namespace import: either a local variable, a
	// constructor-injected property accessed via `this.`, or an unresolved
	// import. Disambiguating those needs the receiver-type tier — an
	// explicit, documented Phase 1 gap (see package doc), not a bug.
	return model.ResolvedRef{
		Disposition: model.DispositionUnimplemented,
		Reason:      fmt.Sprintf("qualified call %s.%s through a non-namespace binding; receiver-type resolution not implemented in Phase 1", ref.Target.Name, ref.Target.Member),
	}
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

	// Tier 2: import table.
	if fe != nil {
		for _, im := range fe.imports {
			if im.LocalName != name || im.IsNamespace {
				continue
			}
			targetFile, found := resolveImportPath(file, im.Source, idx.files)
			if !found {
				return externalDisposition(im.Source)
			}
			target := idx.files[targetFile]
			exported := im.ImportedName
			if im.IsDefault {
				// V0 does not track which entity a file's `export
				// default` names — a documented simplification. Fall
				// through to matching by local name, the common case
				// where the default export shares its declared name.
				exported = name
			}
			if ids := target.byName[exported]; len(ids) == 1 {
				return resolvedEdge(ref.Src, kind, ids[0], 0.95, model.ProvenanceDeterministic,
					fmt.Sprintf("import %s from %q", name, im.Source))
			}
			return model.ResolvedRef{Disposition: model.DispositionBugResolver,
				Reason: fmt.Sprintf("import %s from %q resolved to file %s but no matching export found", name, im.Source, targetFile)}
		}
	}

	// Tier 3: known globals/stdlib (console, Promise, Array, ...) — not a
	// bare-name allowlist entry, a fixed list of runtime builtins that
	// exist in every TS/JS file and would otherwise flood bug_rate.
	if knownGlobals[name] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: "known JS/TS runtime global"}
	}

	// Tier 4: bare-name allowlist policy (docs/research/03). Whitelisting,
	// never blacklisting: a name is bound bare ONLY if explicitly
	// allowlisted, regardless of how many repo-wide candidates exist.
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

// resolveImportPath maps a file-relative import source (e.g.
// "../repositories/userRepository") to a registered file key, trying the
// same candidate suffixes a TS/Node resolver would (implicit extension,
// then index-file resolution). Returns found=false for anything that isn't
// a repo-relative path (bare package specifiers are never files in this
// index) or that doesn't match any registered file.
func resolveImportPath(fromFile, source string, files map[string]*fileEntry) (string, bool) {
	if !strings.HasPrefix(source, ".") {
		return "", false
	}
	base := path.Join(path.Dir(fromFile), source)
	candidates := []string{
		base + ".ts",
		base + ".tsx",
		path.Join(base, "index.ts"),
		path.Join(base, "index.tsx"),
	}
	for _, c := range candidates {
		if _, ok := files[c]; ok {
			return c, true
		}
	}
	return "", false
}
