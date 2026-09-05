package index

import (
	"os"
	"path/filepath"

	"github.com/deatherick/cartograph/internal/parser"
	"github.com/deatherick/cartograph/internal/parser/csharp"
	"github.com/deatherick/cartograph/internal/parser/golang"
	"github.com/deatherick/cartograph/internal/parser/python"
	"github.com/deatherick/cartograph/internal/parser/ts"
	"github.com/deatherick/cartograph/internal/resolve"
)

// Language bundles everything one language plugs into the pipeline: its
// extractor (internal/parser.Extractor) and its resolver policy
// (resolve.LanguagePolicy), plus a cheap Detect check the init wizard and
// Run's zero-config default both use to decide whether this language
// looks like it belongs in root at all.
//
// This is THE integration point — the one place a new language (C#,
// Python, or a community contribution) is registered. Nothing in
// internal/parser or internal/resolve changes to add one: write an
// Extractor (internal/parser/<lang>) and a LanguagePolicy
// (internal/resolve/lang_<lang>.go), then add one Language value to
// registry() below. No language's files reference another's — see
// internal/resolve/langpolicy.go's doc for why that matters.
type Language struct {
	// Name is the stable identifier used in .cartograph.json and the
	// --languages CLI flag ("typescript", "go") — never the Extensions()
	// list (a language can span several extensions) and never changed
	// once shipped (it is persisted in project config files).
	Name      string
	Extractor parser.Extractor
	Policy    resolve.LanguagePolicy
	// Detect reports whether root looks like it uses this language, via
	// cheap marker-file/extension checks — never a full parse. Used by
	// `ctx init`'s wizard and by Run's zero-config default (no
	// .cartograph.json present: every detected language runs).
	Detect func(root string) bool
}

// registry constructs every known language, configured against root
// (TypeScript's tsconfig.json, Go's go.mod module path — each read fresh
// per call, since a Language bundles config that can only be known once
// root is given). Order is insertion order; stable across calls.
func registry(root string) []Language {
	tsCfg, _ := loadTSConfig(root)
	goModule, _ := loadGoModule(root)
	csProjects := loadCSharpProjects(root)
	resolveProjects := make([]resolve.CSharpProject, len(csProjects))
	for i, p := range csProjects {
		resolveProjects[i] = resolve.CSharpProject{Dir: p.Dir, RootNamespace: p.RootNamespace}
	}
	return []Language{
		{
			Name:      "typescript",
			Extractor: ts.New(),
			Policy:    resolve.NewTSPolicy(tsCfg),
			Detect: func(root string) bool {
				return detectByMarkerOrExtension(root, []string{"tsconfig.json", "package.json"}, []string{".ts", ".tsx"})
			},
		},
		{
			Name:      "go",
			Extractor: golang.New(),
			Policy:    resolve.NewGoPolicy(goModule),
			Detect:    func(root string) bool { return detectByMarkerOrExtension(root, []string{"go.mod"}, []string{".go"}) },
		},
		{
			Name:      "csharp",
			Extractor: csharp.New(),
			Policy:    resolve.NewCSharpPolicy(resolveProjects),
			Detect:    func(root string) bool { return detectByMarkerOrExtension(root, nil, []string{".cs"}) },
		},
		{
			Name:      "python",
			Extractor: python.New(),
			Policy:    resolve.NewPythonPolicy(),
			Detect: func(root string) bool {
				return detectByMarkerOrExtension(root, []string{"setup.py", "pyproject.toml", "requirements.txt"}, []string{".py"})
			},
		},
	}
}

// AvailableLanguages lists every language this build knows how to index,
// each with whether it was detected in root — what `ctx init`'s wizard
// presents, and what a `ctx languages` status command reads.
type AvailableLanguage struct {
	Name     string
	Detected bool
}

func AvailableLanguages(root string) []AvailableLanguage {
	langs := registry(root)
	out := make([]AvailableLanguage, len(langs))
	for i, l := range langs {
		out[i] = AvailableLanguage{Name: l.Name, Detected: l.Detect(root)}
	}
	return out
}

// detectByMarkerOrExtension reports whether root contains any of the given
// top-level marker files, OR at least one file with a matching extension
// anywhere in the tree (a shallow, best-effort walk that stops at the
// first match — never a full parse, and respects the same exclusions a
// real index run would via internal/exclude, so it never descends into
// node_modules/vendor/bin/etc. while detecting).
func detectByMarkerOrExtension(root string, markers []string, extensions []string) bool {
	for _, m := range markers {
		if _, err := os.Stat(filepath.Join(root, m)); err == nil {
			return true
		}
	}
	found := false
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if info.IsDir() {
			if path != root && skipDetectionDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		for _, e := range extensions {
			if ext == e {
				found = true
				return filepath.SkipAll
			}
		}
		return nil
	})
	return found
}

// skipDetectionDir mirrors internal/exclude.SkipDir's directory blacklist
// without importing it, to avoid a dependency cycle risk purely for a
// cheap detection walk — kept as a small, duplicated constant list rather
// than restructuring internal/exclude's API for this one caller. If this
// list drifts from internal/exclude's, detection just becomes slightly
// less accurate, never incorrect in a way that breaks indexing itself
// (Run's own walk still uses internal/exclude directly).
func skipDetectionDir(name string) bool {
	switch name {
	case ".git", "node_modules", "dist", "build", "out", "bin", "coverage", "vendor", "generated", ".next", "target":
		return true
	}
	return len(name) > 0 && name[0] == '.'
}
