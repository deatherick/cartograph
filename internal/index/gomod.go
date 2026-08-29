package index

import (
	"os"
	"path/filepath"
	"strings"
)

// loadGoModule reads the module path out of <root>/go.mod's `module`
// directive — the one piece of information Go's import-path resolution
// needs (internal/resolve's SetGoModule), the same role loadTSConfig plays
// for TypeScript's baseUrl/paths. Deliberately minimal: a real go.mod
// parser (golang.org/x/mod/modfile) would also handle `replace`/`require`
// directives, which this project has no current use for — reading the
// module line by hand avoids a dependency for one string. ok=false when the
// file is missing or has no module directive; the caller proceeds without
// Go import resolution rather than failing the whole index run.
func loadGoModule(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "module "); ok {
			return strings.TrimSpace(after), true
		}
	}
	return "", false
}
