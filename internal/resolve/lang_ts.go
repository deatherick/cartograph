package resolve

import (
	"fmt"
	"path"
	"strings"

	"github.com/deatherick/cartograph/internal/model"
)

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

// tsPolicy is TypeScript/JavaScript's LanguagePolicy: one file, self
// contained, touching nothing outside this package's shared Index/
// fileEntry types. See langpolicy.go's interface doc for what "plug and
// play" means here.
type tsPolicy struct {
	tsconfig TSConfig
}

// NewTSPolicy constructs the TypeScript/JavaScript policy. cfg is the
// zero value when the repo has no tsconfig.json (or one with no baseUrl/
// paths) — every import is then treated as either repo-relative (starts
// with ".") or external, exactly as if path-alias resolution did not
// exist.
func NewTSPolicy(cfg TSConfig) LanguagePolicy {
	return &tsPolicy{tsconfig: cfg}
}

func (p *tsPolicy) Lang() model.Lang { return model.LangTS }

// SameScopeFiles: a TypeScript file is its own module — there is no
// broader same-scope set to search.
func (p *tsPolicy) SameScopeFiles(idx *Index, file string) []string { return nil }

func (p *tsPolicy) ResolveQualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	targetFile, found := p.resolveImportPath(idx, file, im.Source)
	if !found {
		return p.externalDisposition(im.Source)
	}
	if id, ok := p.findExportedEntity(idx, targetFile, ref.Target.Member); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("namespace import %s from %q, member %s", ref.Target.Name, im.Source, ref.Target.Member))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("namespace member %s.%s not found in resolved file %s (including any barrel re-exports)", ref.Target.Name, ref.Target.Member, targetFile)}
}

func (p *tsPolicy) ResolveUnqualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	targetFile, found := p.resolveImportPath(idx, file, im.Source)
	if !found {
		return p.externalDisposition(im.Source)
	}
	exported := im.ImportedName
	if im.IsDefault {
		// V0 does not track which entity a file's `export default` names —
		// a documented simplification. Fall through to matching by local
		// name, the common case where the default export shares its
		// declared name.
		exported = name
	}
	if id, ok := p.findExportedEntity(idx, targetFile, exported); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("import %s from %q", name, im.Source))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("import %s from %q resolved to file %s but no matching export found (including any barrel re-exports)", name, im.Source, targetFile)}
}

func (p *tsPolicy) FollowImportToMethods(idx *Index, file string, fe *fileEntry, receiverType string) (map[string]model.EntityID, bool) {
	for _, im := range fe.imports {
		if im.LocalName != receiverType || im.IsNamespace {
			continue
		}
		targetFile, found := p.resolveImportPath(idx, file, im.Source)
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
			return byMethod, true
		}
	}
	return nil, false
}

func (p *tsPolicy) IsBuiltin(name string) bool { return knownGlobals[name] }

func (p *tsPolicy) FinalDisposition(idx *Index, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	// Bare-name allowlist policy (docs/research/03). Whitelisting, never
	// blacklisting: a name is bound bare ONLY if explicitly allowlisted,
	// regardless of how many repo-wide candidates exist.
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

// resolveImportPath maps an import source to a registered file key, trying
// the same candidate suffixes a TS/Node resolver would (implicit
// extension, then index-file resolution). Handles three source shapes:
//   - repo-relative ("./x", "../x") — resolved against fromFile's directory
//   - a tsconfig path alias ("@/services/x") — resolved via p.tsconfig,
//     the Phase 1 scope item docs/research/03 lists ("tsconfig paths,
//     baseUrl") and edge-case-backlog.md C1
//   - anything else (a bare package specifier) — found=false; bare
//     specifiers are never files in this index
func (p *tsPolicy) resolveImportPath(idx *Index, fromFile, source string) (string, bool) {
	var base string
	switch {
	case strings.HasPrefix(source, "."):
		base = path.Join(path.Dir(fromFile), source)
	default:
		aliased, ok := p.resolveTSConfigAlias(source)
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
// p.tsconfig's paths/baseUrl, tsconfig.json's own alias mechanism. Only
// single-wildcard patterns are supported (the overwhelmingly common case,
// e.g. "@/*": ["src/*"]) — see TSConfig's doc for that scoping.
func (p *tsPolicy) resolveTSConfigAlias(source string) (string, bool) {
	if p.tsconfig.BaseURL == "" && len(p.tsconfig.Paths) == 0 {
		return "", false
	}
	for pattern, targets := range p.tsconfig.Paths {
		star := strings.IndexByte(pattern, '*')
		if star < 0 {
			if pattern != source {
				continue
			}
			for _, target := range targets {
				return path.Join(p.tsconfig.BaseURL, target), true
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
			return path.Join(p.tsconfig.BaseURL, resolved), true
		}
	}
	// No explicit paths entry matched, but TS also resolves bare
	// specifiers directly against baseUrl when one is set.
	if p.tsconfig.BaseURL != "" {
		return path.Join(p.tsconfig.BaseURL, source), true
	}
	return "", false
}

// findExportedEntity looks up name as an entity exported by file, either
// declared directly or reached through a chain of barrel re-exports
// (`export * from`, `export { a as b } from` — ADR-0013's discussion in
// docs/research/03-import-resolution-and-bare-names.md, edge-case-backlog.md
// C4). Depth-limited and cycle-safe: a re-export chain longer than
// maxReExportDepth, or one that revisits a file, stops rather than loops.
func (p *tsPolicy) findExportedEntity(idx *Index, file, name string) (model.EntityID, bool) {
	return p.findExportedEntityDepth(idx, file, name, 0, map[string]bool{})
}

const maxReExportDepth = 4

func (p *tsPolicy) findExportedEntityDepth(idx *Index, file, name string, depth int, visited map[string]bool) (model.EntityID, bool) {
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
		nextFile, found := p.resolveImportPath(idx, file, re.Source)
		if !found {
			continue
		}
		if re.IsStar {
			if id, ok := p.findExportedEntityDepth(idx, nextFile, name, depth+1, visited); ok {
				return id, true
			}
			continue
		}
		if re.LocalAlias == name {
			if id, ok := p.findExportedEntityDepth(idx, nextFile, re.ExportedName, depth+1, visited); ok {
				return id, true
			}
		}
	}
	return "", false
}

// externalDisposition classifies an import whose source could not be
// resolved to a known repo file. A repo-relative path (./ or ../) that
// fails to resolve is a stronger signal — it should be one of ours — so it
// is BugExtractor rather than ExternalUnknown; anything else (a bare
// package specifier like "express") is presumed a real external dependency.
func (p *tsPolicy) externalDisposition(source string) model.ResolvedRef {
	if strings.HasPrefix(source, ".") {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("repo-relative import %q did not resolve to any indexed file", source)}
	}
	if knownPackages[source] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("known external package %q", source)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown, Reason: fmt.Sprintf("unrecognized external package %q", source)}
}
