package resolve

import (
	"fmt"
	"path"
	"strings"

	"github.com/deatherick/cartograph/internal/model"
)

// pyPolicy is Python's LanguagePolicy: one file, self contained, touching
// nothing outside this package's shared Index/fileEntry types and
// nothing in lang_ts.go/lang_go.go/lang_csharp.go. See langpolicy.go's
// interface doc for what "plug and play" means here.
//
// FILE-scoped, like TypeScript — NOT directory/namespace-scoped like
// Go/C#. This is a genuine language difference, not a narrower
// approximation: Python has no implicit same-package visibility the way
// a Go package or a C# namespace (by folder convention) does — a sibling
// file in the same directory still needs an explicit `from .other import
// Name` to use anything from it (verified: `SameScopeFiles` returns nil).
//
// Absolute imports (`import conduit.apps.core.models`,
// `from conduit.apps.core.models import User`) resolve directly against
// the REPO ROOT — a "flat layout" assumption (no `src/` prefix, no
// `pyproject.toml`/`setup.py` `package_dir` remapping read), matching
// this project's own real validation target
// (gothinkster/django-realworld-example-app) and the overwhelming
// majority of real, non-`src`-layout Python projects. A `src`-layout
// repo (where the on-disk root doesn't match the importable package
// root) is a documented, honest gap — never guessed at by trying a "src/"
// prefix as a fallback, the same "exact match only, never a partial/
// suffix heuristic" discipline ADR-0023 established for C#'s `using`
// resolution.
type pyPolicy struct{}

// NewPythonPolicy constructs the Python policy. No configuration is
// needed (unlike Go's go.mod module path or C#'s per-.csproj root
// namespaces) — see the package doc above for why absolute imports
// resolve directly against the repo root instead.
func NewPythonPolicy() LanguagePolicy {
	return &pyPolicy{}
}

func (p *pyPolicy) Lang() model.Lang { return model.LangPython }

func (p *pyPolicy) SameScopeFiles(idx *Index, file string) []string { return nil }

// ResolveQualifiedImport handles `import x.y.z [as w]` then `w.Member`
// (or `x.Member` when unaliased and x IS the whole dotted path — the
// common case in this project's own validation, `import jwt` then
// `jwt.encode(...)`, which correctly resolves as EXTERNAL since "jwt" is
// not under this repo). Multi-segment unaliased access (`import x.y.z`
// then `x.y.z.member()`, a three-level chain) is a documented,
// unhandled gap — the same "don't chase 3-level chains" scoping already
// accepted for C#'s `Guard.Against.Null` case (ADR-0023).
func (p *pyPolicy) ResolveQualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	targetFile, internal, found := p.resolveModulePath(idx, file, im.Source)
	if !internal {
		return p.externalDisposition(im.Source)
	}
	if !found {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("python import %q did not resolve to any indexed file (tried %q)", im.Source, targetFile)}
	}
	if id, ok := p.findExportedEntity(idx, targetFile, ref.Target.Member); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("module import %s (%s), member %s", ref.Target.Name, im.Source, ref.Target.Member))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("module %s (%q) has no member %s (this extractor is not yet re-export-aware — a name only re-exported through an __init__.py barrel, edge-case-backlog.md E2, would also report this)", ref.Target.Name, im.Source, ref.Target.Member)}
}

// ResolveUnqualifiedImport handles `from x.y import Name` / `from .
// import Name` / `from ..pkg import Name` then a bare `Name(...)` call —
// the dominant real-repo import shape (every internal import in
// django-realworld-example-app's own source is this form).
func (p *pyPolicy) ResolveUnqualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	targetFile, internal, found := p.resolveModulePath(idx, file, im.Source)
	if !internal {
		return p.externalDisposition(im.Source)
	}
	if !found {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("python import %q (for %s) did not resolve to any indexed file (tried %q)", im.Source, name, targetFile)}
	}
	if id, ok := p.findExportedEntity(idx, targetFile, im.ImportedName); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("from %s import %s", im.Source, im.ImportedName))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("from %q import %s resolved to file %s but no matching definition was found (not re-export-aware — see edge-case-backlog.md E2)", im.Source, im.ImportedName, targetFile)}
}

// FollowImportToMethods resolves receiverType through fe's import table
// to another file's methodsByOwner map — Python's version of "an
// imported class used as its own receiver for a qualified/static call"
// (`from .models import Article` then `Article.objects...` or a
// classmethod call). Reused unchanged from the core pipeline's own
// generic fallback (resolveQualified already sets receiverType to the
// import's LocalName for this exact shape); this method just needs to
// follow the import to the target file.
func (p *pyPolicy) FollowImportToMethods(idx *Index, file string, fe *fileEntry, receiverType string) (map[string]model.EntityID, bool) {
	for _, im := range fe.imports {
		if im.LocalName != receiverType || im.IsNamespace {
			continue
		}
		targetFile, internal, found := p.resolveModulePath(idx, file, im.Source)
		if !internal || !found {
			continue
		}
		target := idx.files[targetFile]
		if target == nil {
			continue
		}
		if byMethod, ok := target.methodsByOwner[im.ImportedName]; ok {
			return byMethod, true
		}
	}
	return nil, false
}

// ResolveImportTarget exposes resolveModulePath standalone for
// Index.Dependents (ADR-0020) — a single-file-per-module language, like
// TypeScript, resolves to at most one file.
func (p *pyPolicy) ResolveImportTarget(idx *Index, file, source string) ([]string, bool) {
	target, internal, found := p.resolveModulePath(idx, file, source)
	if !internal || !found {
		return nil, false
	}
	return []string{target}, true
}

func (p *pyPolicy) IsBuiltin(name string) bool { return pyBuiltins[name] }

// FinalDisposition: like TypeScript/C#, Python has abundant implicit
// external scope (stdlib/third-party names this extractor never chases
// into) — "presumed external, never guessed" is the default. No
// bare-name allowlist (unlike TypeScript's): a name is never bound just
// because it happens to be the only same-named candidate in the repo,
// the same conservative standard this project adopted starting with C#
// (ADR-0023) at the user's explicit request.
func (p *pyPolicy) FinalDisposition(idx *Index, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	if candidates := idx.byBareName[name]; len(candidates) > 0 {
		return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: candidates,
			Reason: fmt.Sprintf("bare name %q exists elsewhere in the repo (%d candidate(s)) but was not reached via same-file/import resolution — never bound by repo-wide uniqueness alone", name, len(candidates))}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown,
		Reason: fmt.Sprintf("bare identifier %q not found locally or via any import resolved to an indexed file — presumed external (stdlib/third-party)", name)}
}

// resolveModulePath maps a Python module reference (im.Source — either
// absolute, "conduit.apps.core.models", or relative-encoded-as-text,
// ".models"/".."/"..pkg", produced by internal/parser/python's
// parseImportFrom) to a repo-relative file. internal reports whether the
// reference is something this policy could plausibly resolve at all
// (false only for a source this extractor doesn't recognize the shape
// of — in practice, always true, since every Python import is either
// relative or absolute); found reports whether that path actually has an
// indexed file — false is a strong bug signal for a relative import
// (which can ONLY mean an internal module) and a softer one for an
// absolute import (see externalDisposition for how those are told apart).
func (p *pyPolicy) resolveModulePath(idx *Index, fromFile, source string) (target string, internal bool, found bool) {
	isRelative := strings.HasPrefix(source, ".")
	var base string
	if isRelative {
		depth := 0
		for depth < len(source) && source[depth] == '.' {
			depth++
		}
		suffix := source[depth:]
		dir := path.Dir(fromFile)
		for i := 1; i < depth; i++ {
			dir = path.Dir(dir)
		}
		if suffix == "" {
			base = dir
		} else {
			base = path.Join(dir, strings.ReplaceAll(suffix, ".", "/"))
		}
	} else {
		base = strings.ReplaceAll(source, ".", "/")
	}
	candidates := []string{base + ".py", path.Join(base, "__init__.py")}
	for _, c := range candidates {
		if _, ok := idx.files[c]; ok {
			return c, true, true
		}
	}
	// A relative import that fails to resolve is always internal, just
	// broken (BugExtractor territory — see ResolveUnqualifiedImport/
	// ResolveQualifiedImport's callers); an absolute one that fails is
	// only a soft signal — most absolute imports really are external
	// (stdlib/third-party), so it's routed to externalDisposition
	// instead, never assumed internal-but-broken.
	return base, isRelative, false
}

// findExportedEntity looks up name as a module-level entity declared
// directly in file. No re-export chasing (a name only re-exported
// through an __init__.py barrel is a documented gap, edge-case-backlog.md
// E2) — the same conservative "don't guess through a chain" default
// TypeScript's own findExportedEntity uses for barrel re-exports it DOES
// chase, except Python's version doesn't chase at all yet.
func (p *pyPolicy) findExportedEntity(idx *Index, file, name string) (model.EntityID, bool) {
	target := idx.files[file]
	if target == nil {
		return "", false
	}
	if ids := target.byName[name]; len(ids) == 1 {
		return ids[0], true
	}
	return "", false
}

// externalDisposition classifies an absolute Python import that
// resolveModulePath could not map to any indexed file. A single-segment,
// no-dot source with no matching file (e.g. "os", "jwt", "random") is
// presumed a stdlib/third-party top-level package — Python's own
// packaging convention has no cheap syntactic tell between "stdlib" and
// "third-party" the way Go's first-segment-has-no-dot rule works for
// import PATHS (a Python package name and a repo-internal top-level
// package name look identical), so this checks pyKnownPackages
// (this project's own validated real dependencies) for ExternalKnown,
// else ExternalUnknown — never BugExtractor for an absolute import (only
// a relative one, which can only ever mean "internal", earns that).
func (p *pyPolicy) externalDisposition(source string) model.ResolvedRef {
	first := source
	if i := strings.IndexByte(source, '.'); i >= 0 {
		first = source[:i]
	}
	if pyKnownPackages[first] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("known external Python package %q", source)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown, Reason: fmt.Sprintf("unrecognized external Python package %q", source)}
}
