package index

import (
	"encoding/xml"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// csharpProject is one discovered .csproj file: the repo-relative
// directory it lives in, the root namespace every type inside that
// project is nested under, and every OTHER project it references (via
// `<ProjectReference>`), as repo-relative directories. This is C#'s
// analog of go.mod's module path (loadGoModule) — the one piece of
// information needed to map a `using` directive's namespace back to a
// directory this index walked — except a single repo can contain MANY
// .csproj files (a multi-project solution, the normal .NET layout:
// eShopOnWeb's own repo has ten), unlike Go's one module per repo. See
// internal/resolve/lang_csharp.go's resolveImportPath for how this is
// used, and ADR-0023 for why namespace matching is EXACT only (no
// partial-suffix heuristic) — a deliberate, user-requested guard against
// the resolver ever guessing a directory from a naming convention;
// ProjectReferences gating (below) follows the same discipline: a
// namespace only resolves across a project boundary when a REAL
// `<ProjectReference>` says that boundary is crossable, never because
// the namespace merely happens to exist somewhere in the repo.
type csharpProject struct {
	Dir               string // repo-relative directory containing the .csproj
	RootNamespace     string
	ProjectReferences []string // repo-relative directories of every referenced project
}

// csprojXML is the minimal shape this reads out of a .csproj file — an
// MSBuild XML project file. Only <RootNamespace> and <ProjectReference>
// are used; everything else (<TargetFramework>, <PackageReference>, ...)
// is irrelevant to import resolution and deliberately not modeled, the
// same "read only the one field this project needs" discipline
// loadGoModule and loadTSConfig already follow for their own config
// formats.
type csprojXML struct {
	PropertyGroups []struct {
		RootNamespace string `xml:"RootNamespace"`
	} `xml:"PropertyGroup"`
	ItemGroups []struct {
		ProjectReferences []struct {
			Include string `xml:"Include,attr"`
		} `xml:"ProjectReference"`
	} `xml:"ItemGroup"`
}

// loadCSharpProjects walks root for every *.csproj file (skipping the same
// build-output/dependency directories internal/exclude already skips for a
// real index run — see skipDetectionDir) and returns one csharpProject per
// file found. A .csproj with no explicit <RootNamespace> defaults to its
// own file name without the extension — the real MSBuild default (the
// project's AssemblyName, which itself defaults to the .csproj's file
// name) — not a guess invented for this project.
func loadCSharpProjects(root string) []csharpProject {
	var out []csharpProject
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil //nolint:nilerr // best-effort walk, matching detectByMarkerOrExtension's own error handling
		}
		if info.IsDir() {
			if p != root && skipDetectionDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(filepath.Ext(p), ".csproj") {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil //nolint:nilerr
		}
		dir := filepath.ToSlash(filepath.Dir(rel))
		if dir == "." {
			dir = ""
		}
		doc := readCsprojXML(p)
		ns := rootNamespaceOf(doc)
		if ns == "" {
			base := filepath.Base(p)
			ns = strings.TrimSuffix(base, filepath.Ext(base))
		}
		out = append(out, csharpProject{
			Dir:               dir,
			RootNamespace:     ns,
			ProjectReferences: projectReferenceDirs(dir, doc),
		})
		return nil
	})
	return out
}

// readCsprojXML reads and parses path. Returns nil on any read/parse
// failure — the caller falls back to defaults (a file-name-derived
// namespace, no project references), never treats a malformed .csproj as
// fatal to the whole index run (same "skip, don't guess, don't fail the
// run" discipline loadTSConfig follows for a malformed tsconfig.json).
func readCsprojXML(path string) *csprojXML {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc csprojXML
	if err := xml.Unmarshal(data, &doc); err != nil {
		return nil
	}
	return &doc
}

// rootNamespaceOf reads <RootNamespace> from the first PropertyGroup that
// declares one. "" (doc == nil, or none declare it) tells the caller to
// fall back to the file-name default.
func rootNamespaceOf(doc *csprojXML) string {
	if doc == nil {
		return ""
	}
	for _, pg := range doc.PropertyGroups {
		if pg.RootNamespace != "" {
			return pg.RootNamespace
		}
	}
	return ""
}

// projectReferenceDirs resolves every `<ProjectReference Include="...">`
// path in doc (relative to csprojDir, the referencING project's own
// repo-relative directory) to the referencED project's repo-relative
// directory — i.e., strips the referenced .csproj's own file name,
// keeping just its containing directory, exactly like this project's own
// Dir field is derived in loadCSharpProjects. MSBuild project paths use
// backslashes (Windows-style, even on a repo checked out on macOS/Linux —
// verified against eShopOnWeb's real .csproj files, not assumed); both
// separators are normalized before resolving.
func projectReferenceDirs(csprojDir string, doc *csprojXML) []string {
	if doc == nil {
		return nil
	}
	var out []string
	for _, ig := range doc.ItemGroups {
		for _, pr := range ig.ProjectReferences {
			include := strings.ReplaceAll(pr.Include, `\`, "/")
			if include == "" {
				continue
			}
			resolved := path.Join(csprojDir, path.Dir(include))
			out = append(out, resolved)
		}
	}
	return out
}
