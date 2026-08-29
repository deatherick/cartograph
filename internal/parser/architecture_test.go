package parser

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArchitectureBoundary_NoTreeSitterOutsideParser enforces the rule
// stated in the package doc: no .go file outside internal/parser/** may
// import go-tree-sitter or any tree-sitter-<lang> grammar module. This is a
// textual/import check, not a build-graph check, matching exactly the kind
// of migration pain Grafel documented in ADR-0023 (245 files, 1,758 call
// sites touching the sitter.Node type because the binding leaked past its
// wrapper). Catching a leak here costs one grep; catching it after 200
// files depend on it costs a rewrite.
func TestArchitectureBoundary_NoTreeSitterOutsideParser(t *testing.T) {
	repoRoot := findRepoRoot(t)
	parserDir := filepath.Join(repoRoot, "internal", "parser")

	var violations []string
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if info.Name() == ".git" || info.Name() == "bin" || info.Name() == "fixtures" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		// Anything inside internal/parser/** is the wrapper itself — allowed.
		if strings.HasPrefix(path, parserDir+string(filepath.Separator)) || path == parserDir {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(content), `"github.com/tree-sitter/go-tree-sitter"`) ||
			strings.Contains(string(content), `tree-sitter/tree-sitter-`) {
			rel, _ := filepath.Rel(repoRoot, path)
			violations = append(violations, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking repo: %v", err)
	}
	if len(violations) > 0 {
		t.Fatalf("tree-sitter types leaked outside internal/parser/** in: %v", violations)
	}
}

// findRepoRoot walks up from the working directory to find go.mod. Tests
// run with the package directory as cwd, so this reaches the repo root
// reliably without hardcoding a path.
func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root (no go.mod found)")
		}
		dir = parent
	}
}
