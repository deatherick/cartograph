package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/deatherick/cartograph/internal/resolve"
)

// tsconfigFile mirrors just the subset of tsconfig.json's shape this
// project uses. Real tsconfig files can be JSONC (comments, trailing
// commas) — not handled here, a documented gap (a config that fails to
// parse is skipped, not guessed at). `extends` (config inheritance) IS
// handled, below.
type tsconfigFile struct {
	Extends         string `json:"extends"`
	CompilerOptions struct {
		BaseURL string              `json:"baseUrl"`
		Paths   map[string][]string `json:"paths"`
	} `json:"compilerOptions"`
}

// loadTSConfig reads <root>/tsconfig.json, if present, and returns the
// resolver's TSConfig view of it. ok=false when the file doesn't exist or
// fails to parse — the caller proceeds without path-alias resolution
// rather than failing the whole index run over a malformed config.
//
// Follows a single `"extends"` chain (a bare relative path, e.g.
// "./tsconfig.base.json" — the dominant real-world form; a
// node_modules package specifier as an extends target is out of scope,
// same "whitelist not guess" bar as everywhere else in this project).
// Per tsconfig's own semantics, a child's `compilerOptions.baseUrl` /
// `.paths` each individually override the parent's when present, rather
// than merging key-by-key inside `paths` itself.
func loadTSConfig(root string) (resolve.TSConfig, bool) {
	tc, ok := loadTSConfigChain(filepath.Join(root, "tsconfig.json"), map[string]bool{})
	if !ok {
		return resolve.TSConfig{}, false
	}
	if tc.CompilerOptions.BaseURL == "" && len(tc.CompilerOptions.Paths) == 0 {
		return resolve.TSConfig{}, false
	}
	return resolve.TSConfig{BaseURL: tc.CompilerOptions.BaseURL, Paths: tc.CompilerOptions.Paths}, true
}

// loadTSConfigChain reads path and, if it declares "extends", recursively
// resolves and merges the parent config first — so the child's own
// baseUrl/paths (if set) take precedence. visited guards against a
// symlink or hand-edited cycle (extends chains referencing each other)
// turning into infinite recursion; a cycle is treated the same as a
// missing/malformed file — skipped, not guessed at.
func loadTSConfigChain(path string, visited map[string]bool) (tsconfigFile, bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return tsconfigFile{}, false
	}
	if visited[abs] {
		return tsconfigFile{}, false
	}
	visited[abs] = true

	data, err := os.ReadFile(abs)
	if err != nil {
		return tsconfigFile{}, false
	}
	var tc tsconfigFile
	if err := json.Unmarshal(data, &tc); err != nil {
		return tsconfigFile{}, false
	}

	if tc.Extends == "" {
		return tc, true
	}
	// Only a relative-path extends target is supported — a bare package
	// name (`"extends": "@tsconfig/node18/tsconfig.json"`) would need
	// node_modules resolution this project doesn't otherwise do anywhere,
	// so it's left unresolved rather than guessed at.
	if !strings.HasPrefix(tc.Extends, ".") {
		return tc, true
	}
	parentPath := filepath.Join(filepath.Dir(abs), tc.Extends)
	if filepath.Ext(parentPath) != ".json" {
		parentPath += ".json"
	}
	parent, ok := loadTSConfigChain(parentPath, visited)
	if !ok {
		// The extends target itself is missing/malformed: proceed with
		// just the child's own (still valid) settings rather than
		// discarding the whole config.
		return tc, true
	}
	merged := parent
	if tc.CompilerOptions.BaseURL != "" {
		merged.CompilerOptions.BaseURL = tc.CompilerOptions.BaseURL
	}
	if len(tc.CompilerOptions.Paths) > 0 {
		merged.CompilerOptions.Paths = tc.CompilerOptions.Paths
	}
	return merged, true
}
