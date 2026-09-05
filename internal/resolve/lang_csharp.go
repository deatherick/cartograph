package resolve

import (
	"fmt"
	"path"
	"strings"

	"github.com/deatherick/cartograph/internal/model"
)

// CSharpProject is one .csproj this repo's index run found — the
// directory it lives in (repo-relative) and its root namespace. A single
// repo can hold several (a multi-project .NET solution, the normal
// layout), unlike Go's single go.mod module — see
// internal/index/csproj.go for how these are discovered.
type CSharpProject struct {
	Dir           string
	RootNamespace string
}

// csPolicy is C#'s LanguagePolicy: one file, self contained, touching
// nothing outside this package's shared Index/fileEntry types and
// nothing in lang_ts.go/lang_go.go. See langpolicy.go's interface doc for
// what "plug and play" means here.
//
// Directory-scoped, like Go's policy (ADR-0010) — see
// internal/parser/csharp/queries/entities.scm's package doc for why
// (the folder-mirrors-namespace convention). `using Some.Namespace;`
// directives resolve to a directory ONLY on an EXACT namespace match
// against a known project's root namespace (never a partial/suffix
// heuristic) — a deliberate guard the user asked for explicitly during
// this ADR's design: the resolver must never guess a directory from a
// naming convention, only from real, exact project configuration.
type csPolicy struct {
	projects []CSharpProject
}

// NewCSharpPolicy constructs the C# policy. projects is empty when the
// repo has no .csproj files (or none could be read) — every `using`
// directive then resolves as external rather than guessing at internal
// structure, the same conservative default NewGoPolicy("") uses for a
// missing go.mod.
func NewCSharpPolicy(projects []CSharpProject) LanguagePolicy {
	return &csPolicy{projects: projects}
}

func (p *csPolicy) Lang() model.Lang { return model.LangCSharp }

// SameScopeFiles: every other file in the same directory (the
// folder-mirrors-namespace convention) PLUS every file in a directory
// resolved from one of this file's own `using` directives — both are
// "files whose declarations are visible here without qualification",
// which is exactly what SameScopeFiles means (see langpolicy.go's doc).
func (p *csPolicy) SameScopeFiles(idx *Index, file string) []string {
	fe := idx.files[file]
	if fe == nil {
		return nil
	}
	seen := map[string]bool{file: true}
	var out []string
	add := func(f string) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for _, sibling := range idx.filesByDir[fe.dir] {
		add(sibling)
	}
	for _, im := range fe.imports {
		if !im.IsNamespace || im.LocalName != "" {
			continue // LocalName != "" is an alias directive, handled by ResolveQualifiedImport instead
		}
		dir, _, found := p.resolveImportPath(idx, im.Source)
		if !found {
			continue
		}
		for _, f := range idx.filesByDir[dir] {
			add(f)
		}
	}
	return out
}

// ResolveQualifiedImport handles ONLY the namespace/type alias form
// (`using Alias = Some.Namespace;` then `Alias.Member`) — the one C#
// import shape that binds a single specific local name, matching
// ResolveQualifiedImport's contract (im.LocalName == the call's object
// identifier). Every other `using` directive is plain-form (no
// LocalName), handled entirely by SameScopeFiles instead — see that
// method's doc.
func (p *csPolicy) ResolveQualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	dir, internal, found := p.resolveImportPath(idx, im.Source)
	if !internal {
		return p.externalDisposition(im.Source)
	}
	if !found {
		return model.ResolvedRef{Disposition: model.DispositionBugExtractor,
			Reason: fmt.Sprintf("using alias %q -> %q is under a known project's root namespace but no indexed directory %q was found", im.LocalName, im.Source, dir)}
	}
	if id, ok := p.findExportedEntity(idx, dir, ref.Target.Member); ok {
		return resolvedEdge(ref.Src, kind, id, 0.95, model.ProvenanceDeterministic,
			fmt.Sprintf("using alias %s = %s, member %s", ref.Target.Name, im.Source, ref.Target.Member))
	}
	return model.ResolvedRef{Disposition: model.DispositionBugResolver,
		Reason: fmt.Sprintf("namespace %s (alias %s) has no member %s (this extractor is not yet access-modifier-aware — a private/internal name would also report this)", im.Source, ref.Target.Name, ref.Target.Member)}
}

// ResolveUnqualifiedImport is never actually reached: a plain `using`
// directive (the only NON-alias C# import shape) binds no single
// LocalName — see SameScopeFiles's doc — so the core pipeline's
// `im.LocalName != name` guard excludes it before this is ever called.
// Implemented defensively rather than omitted, since LanguagePolicy
// requires it (mirrors lang_go.go's own unreachable stub).
func (p *csPolicy) ResolveUnqualifiedImport(idx *Index, file string, im model.ImportBinding, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	return model.ResolvedRef{Disposition: model.DispositionUnclassified,
		Reason: "unreachable: a plain `using` directive binds no single local name"}
}

// FollowImportToMethods: C# has no import-aliasing concept where a
// same-namespace type's methods would be reached through the import
// table — that path is handled entirely by SameScopeFiles instead (see
// resolveByReceiverType's core pipeline). Always ok=false, mirroring
// lang_go.go.
func (p *csPolicy) FollowImportToMethods(idx *Index, file string, fe *fileEntry, receiverType string) (map[string]model.EntityID, bool) {
	return nil, false
}

// ResolveImportTarget exposes resolveImportPath standalone for
// Index.Dependents (ADR-0020) — a `using` resolves to a whole namespace
// directory, so every file in it is returned (a copy, since
// idx.filesByDir[dir] is a live slice callers must not mutate).
func (p *csPolicy) ResolveImportTarget(idx *Index, file, source string) ([]string, bool) {
	dir, internal, found := p.resolveImportPath(idx, source)
	if !internal || !found {
		return nil, false
	}
	files := idx.filesByDir[dir]
	out := make([]string, len(files))
	copy(out, files)
	return out, true
}

func (p *csPolicy) IsBuiltin(name string) bool { return csBuiltins[name] }

// FinalDisposition: unlike Go's exhaustive static resolution, C# has
// abundant implicit scope (BCL/framework types brought in by a `using`
// this extractor could not map to an indexed directory, or referenced via
// fully-qualified names this extractor does not chase) — the same
// "presumed external, never guessed" default TypeScript's FinalDisposition
// uses, WITHOUT TypeScript's bare-name allowlist (no optimistic binding
// by repo-wide uniqueness) — the user explicitly asked, during this ADR's
// design, that an unresolved name never be bound just because it happens
// to be the only same-named candidate in the repo.
func (p *csPolicy) FinalDisposition(idx *Index, ref model.Ref, kind model.EdgeKind) model.ResolvedRef {
	name := ref.Target.Name
	if candidates := idx.byBareName[name]; len(candidates) > 0 {
		return model.ResolvedRef{Disposition: model.DispositionAmbiguous, Candidates: candidates,
			Reason: fmt.Sprintf("bare name %q exists elsewhere in the repo (%d candidate(s)) but was not reached via same-file/same-directory/using resolution — never bound by repo-wide uniqueness alone", name, len(candidates))}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown,
		Reason: fmt.Sprintf("bare identifier %q not found locally, in any same-scope directory, or via any `using` directive resolved to an indexed directory — presumed external (BCL/NuGet)", name)}
}

// resolveImportPath maps a C# namespace string to a repo-relative
// directory using p.projects (from every .csproj this run found).
// internal reports whether namespace is an EXACT prefix match of some
// known project's root namespace (false routes the caller to
// externalDisposition instead of guessing); found reports whether that
// directory actually has any indexed file. Deliberately exact-only — no
// partial/suffix matching — per this ADR's explicit design guard.
func (p *csPolicy) resolveImportPath(idx *Index, namespace string) (dir string, internal bool, found bool) {
	for _, proj := range p.projects {
		var candidate string
		switch {
		case namespace == proj.RootNamespace:
			candidate = proj.Dir
		case strings.HasPrefix(namespace, proj.RootNamespace+"."):
			rest := strings.TrimPrefix(namespace, proj.RootNamespace+".")
			candidate = path.Join(proj.Dir, strings.ReplaceAll(rest, ".", "/"))
		default:
			continue
		}
		_, found = idx.filesByDir[candidate]
		return candidate, true, found
	}
	return "", false, false
}

// findExportedEntity looks up name across every file in a namespace
// directory — mirrors lang_go.go's own findExportedEntity exactly (same
// directory-scoped lookup unit, same "don't guess" rule for zero or
// multiple matches).
func (p *csPolicy) findExportedEntity(idx *Index, dir, name string) (model.EntityID, bool) {
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

// externalDisposition classifies a `using` namespace that resolveImportPath
// determined is not under any known project's root namespace. "System"
// and "Microsoft" are the two real, well-known BCL/first-party namespace
// roots (not a guess — every .NET BCL and first-party ASP.NET Core
// namespace starts with one of these two words); anything else is
// checked against csKnownNamespaces (this project's own validated NuGet
// dependencies), else ExternalUnknown.
func (p *csPolicy) externalDisposition(namespace string) model.ResolvedRef {
	first := namespace
	if i := strings.IndexByte(namespace, '.'); i >= 0 {
		first = namespace[:i]
	}
	if first == "System" || first == "Microsoft" {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf(".NET BCL/first-party namespace %q", namespace)}
	}
	if csKnownNamespaces[first] {
		return model.ResolvedRef{Disposition: model.DispositionExternalKnown, Reason: fmt.Sprintf("known external C# namespace %q", namespace)}
	}
	return model.ResolvedRef{Disposition: model.DispositionExternalUnknown, Reason: fmt.Sprintf("unrecognized external C# namespace %q", namespace)}
}
