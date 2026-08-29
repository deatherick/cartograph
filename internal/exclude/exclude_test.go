package exclude

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkipDir(t *testing.T) {
	cases := map[string]bool{
		".git":         true,
		".github":      true, // dot-dir category, not explicitly listed
		"node_modules": true,
		"dist":         true,
		"src":          false,
		"routes":       false,
	}
	for name, want := range cases {
		if got := SkipDir(name); got != want {
			t.Errorf("SkipDir(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSkipFile(t *testing.T) {
	if !SkipFile("package-lock.json") {
		t.Error("expected package-lock.json to be skipped")
	}
	if SkipFile("index.ts") {
		t.Error("did not expect index.ts to be skipped")
	}
}

func TestIsBinary(t *testing.T) {
	if IsBinary([]byte("export function foo() { return 1; }")) {
		t.Error("plain source text should not be detected as binary")
	}
	if !IsBinary([]byte{0x00, 0x01, 0x02, 'W', 'T', 0x00}) {
		t.Error("content with a NUL byte should be detected as binary")
	}
	if IsBinary(nil) {
		t.Error("empty content should not be detected as binary")
	}
}

// TestWalkSource_SkipsMongoDataDir is the case that motivated this package:
// cloning a real repo (typescript-node-express-realworld-example-app) for
// ctxbench surfaced a working-tree directory of MongoDB WiredTiger data
// files (binary .wt pages) that a naive walker reads as text.
func TestWalkSource_SkipsMongoDataDir(t *testing.T) {
	root := t.TempDir()

	mustWrite(t, filepath.Join(root, "src", "index.ts"), "export const x = 1;\n")
	mustWrite(t, filepath.Join(root, "conduit", "collection-1.wt"), string([]byte{0x00, 'W', 'T', 0x00, 'd', 'a', 't', 'a'}))
	mustWrite(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "module.exports = {};\n")
	mustWrite(t, filepath.Join(root, ".git", "HEAD"), "ref: refs/heads/main\n")
	mustWrite(t, filepath.Join(root, "package-lock.json"), `{"lockfileVersion": 3}`)

	var visited []string
	if err := WalkSource(root, func(path string, content []byte) error {
		rel, _ := filepath.Rel(root, path)
		visited = append(visited, filepath.ToSlash(rel))
		return nil
	}); err != nil {
		t.Fatalf("WalkSource: %v", err)
	}

	if len(visited) != 1 || visited[0] != "src/index.ts" {
		t.Fatalf("expected only src/index.ts to be visited, got %v", visited)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
