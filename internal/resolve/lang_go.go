package resolve

import (
	"fmt"
	"strings"

	"github.com/deatherick/cartograph/internal/model"
)

// goPolicy is Go's LanguagePolicy: one file, self contained, touching
// nothing outside this package's shared Index/fileEntry types and nothing
// in lang_ts.go. See langpolicy.go's interface doc for what "plug and
// play" means here.
type goPolicy struct {
	// goModule is the Go module path from go.mod (e.g.
	// "github.com/deatherick/cartograph"), used to map an import path to a
	// repo-relative directory — Go's analog of TSConfig's baseUrl/paths.
	// Empty disables Go import resolution (every Go import then resolves
	// as external, never internal — correctly conservative: an import
	// cannot be presumed internal without knowing the module path it would
	// have to match).
	goModule string
}

// NewGoPolicy constructs the Go policy. modulePath is "" when the repo has
// no go.mod (or it could not be read) — every Go import then resolves as
// external rather than guessing at internal structure.
func NewGoPolicy(modulePath string) LanguagePolicy {
	return &goPolicy{goModule: modulePath}
}

func (p *goPolicy) Lang() model.Lang { return model.LangGo }

// SameScopeFiles: a Go package spans every file in one directory, unlike
// TypeScript's one-module-per-file model — a struct's method routinely
// lives in a different file from the struct itself, and the Go compiler
// treats them as one unit.
func (p *goPolicy) SameScopeFiles(idx *Index, file string) []string {
	fe := idx.files[file]
	if fe == nil {
		return nil
	}
	var out []string
	for _, sibling := range idx.filesByDir[fe.dir] {
		if sibling != file {
			out = append(out, sibling)
		}
	}
	return out
}

// ResolveQualifiedImport resolves a package-qualified Go call/type-use
// (`pkg.Member`) through im, the import binding whose LocalName matched
// the call's object identifier — against a Go package (a directory of
// files), not a single resolved file.
func (p *goPolicy) ResolveQualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	dir, internal, found := p.resolveImportPath(idx, im.Source)
	if !internal {
		return p.externalDisposition(im.Source)
	}
	if !found {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("go import %q is under this module but no indexed file was found in directory %q", im.Source, dir)}
	}
	if id, ok := p.findExportedEntity(idx, dir, ref.Target.Member); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("package import %s (%s), member %s", ref.Target.Name, im.Source, ref.Target.Member))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("package %s (%q) has no member %s (this extractor is not yet export-aware — an unexported name in that package would also report this)", ref.Target.Name, im.Source, ref.Target.Member)}
}

// ResolveUnqualifiedImport is never actually reached: every Go import is
// namespace-style (see internal/parser/golang's importFromMatch doc), so
// the core pipeline's `!im.IsNamespace` guard excludes Go before this is
// ever called. Implemented defensively rather than omitted, since
// LanguagePolicy requires it.
func (p *goPolicy) ResolveUnqualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	return model.ResolvedRef{Disposition: model.DispositionUnclassified,
		Reason: "unreachable: every Go import is namespace-style"}
}

// FollowImportToMethods: Go has no import-aliasing concept where a
// same-package type's methods would be reached through the import table —
// same-package resolution is handled entirely by SameScopeFiles instead
// (see resolveByReceiverType's core pipeline). Always ok=false.
func (p *goPolicy) FollowImportToMethods(idx *Index, file string, fe *fileEntry, receiverType string) (map[string]model.EntityID, bool) {
	return nil, false
}

func (p *goPolicy) IsBuiltin(name string) bool { return goBuiltins[name] }

// FinalDisposition: Go has no bare-name allowlist tier, and does not need
// one. Unlike TypeScript/JavaScript, where an unqualified identifier can
// legitimately be an implicit global with no local declaration, Go's
// static resolution rules mean a bare call target must be a predeclared
// builtin (already checked before this is called), a same-file/
// same-package declaration (also already checked), or — rare, unsupported
// — a dot import (see internal/parser/golang's import.stmt query doc).
// Reaching here therefore means the extractor missed a real declaration,
// not that the name is "presumed external" the way TypeScript/JavaScript
// treats an unresolved bare name.
func (p *goPolicy) FinalDisposition(idx *Index, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	if candidates := idx.byBareName[name]; len(candidates) > 0 {
		// The name exists somewhere in the repo, just not in this package
		// — evidence for a human/agent to look at, not a confident bind
		// (docs/research/03's "never guess" rule).
		return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: candidates,
			Reason: fmt.Sprintf("bare name %q not found in this package; %d repo-wide candidate(s) elsewhere", name, len(candidates))}
	}
	return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
		Reason: fmt.Sprintf("bare Go identifier %q not found in this file, its package, the predeclared identifiers, or anywhere else in the repo — Go's static resolution rules mean this should not be possible unless the extractor missed a declaration (or this is a rare dot-import, an unsupported gap)", name)}
}

// resolveImportPath maps a Go import path to a repo-relative directory
// using p.goModule (from go.mod). internal reports whether importPath is
// under this module at all (false for stdlib/third-party paths, which the
// caller routes to externalDisposition instead); found reports whether
// that directory actually has any indexed file (false is a strong bug
// signal — an internal import that resolves to nothing this index
// walked).
func (p *goPolicy) resolveImportPath(idx *Index, importPath string) (dir string, internal bool, found bool) {
	if p.goModule == "" {
		return "", false, false
	}
	switch {
	case importPath == p.goModule:
		dir = "."
	case strings.HasPrefix(importPath, p.goModule+"/"):
		dir = strings.TrimPrefix(importPath, p.goModule+"/")
	default:
		return "", false, false
	}
	_, found = idx.filesByDir[dir]
	return dir, true, found
}

// findExportedEntity looks up name across every file in a Go package
// directory — the unit Go actually resolves names within, unlike
// TypeScript's per-file lookup. No re-export chasing: Go has no
// barrel-file concept. Returns ok=false for zero or (in valid, compiling
// Go source, which should not happen) more than one match, the same
// conservative "don't guess" rule TypeScript's findExportedEntity follows.
func (p *goPolicy) findExportedEntity(idx *Index, dir, name string) (model.EntityID, bool) {
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

// externalDisposition classifies a Go import path that resolveImportPath
// determined is NOT under this module. A first path segment with no dot is
// Go's own convention for a standard-library import ("fmt", "encoding/json",
// "net/http" — a real third-party path always starts with a domain like
// "github.com"), so it is ExternalKnown without needing an allowlist entry;
// anything else is checked against goKnownPackages (this project's own
// real dependencies), else ExternalUnknown.
func (p *goPolicy) externalDisposition(importPath string) model.ResolvedRef {
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
