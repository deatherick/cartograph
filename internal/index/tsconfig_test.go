package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadTSConfig_Extends_InheritsBaseURLAndPaths(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "tsconfig.base.json"), `{
		"compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
	}`)
	writeJSON(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./tsconfig.base.json"
	}`)

	tc, ok := loadTSConfig(root)
	if !ok {
		t.Fatal("expected loadTSConfig to succeed via the extends chain")
	}
	if tc.BaseURL != "." {
		t.Errorf("expected inherited BaseURL %q, got %q", ".", tc.BaseURL)
	}
	if got := tc.Paths["@/*"]; len(got) != 1 || got[0] != "src/*" {
		t.Errorf("expected inherited paths[@/*]=[src/*], got %v", got)
	}
}

func TestLoadTSConfig_Extends_ChildOverridesParent(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "tsconfig.base.json"), `{
		"compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
	}`)
	writeJSON(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./tsconfig.base.json",
		"compilerOptions": { "paths": { "@app/*": ["app/*"] } }
	}`)

	tc, ok := loadTSConfig(root)
	if !ok {
		t.Fatal("expected loadTSConfig to succeed")
	}
	// Child declared its own paths, so it wins wholesale (not merged
	// key-by-key with the parent's paths object) — matching tsconfig's
	// own compilerOptions-level override semantics.
	if _, stillHasParentAlias := tc.Paths["@/*"]; stillHasParentAlias {
		t.Errorf("expected the child's own paths to fully replace the parent's, got %v", tc.Paths)
	}
	if got := tc.Paths["@app/*"]; len(got) != 1 || got[0] != "app/*" {
		t.Errorf("expected child's own paths[@app/*]=[app/*], got %v", got)
	}
	// BaseURL wasn't redeclared by the child, so it's still inherited.
	if tc.BaseURL != "." {
		t.Errorf("expected inherited BaseURL %q, got %q", ".", tc.BaseURL)
	}
}

func TestLoadTSConfig_Extends_MissingParent_KeepsChildSettings(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./does-not-exist.json",
		"compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
	}`)

	tc, ok := loadTSConfig(root)
	if !ok {
		t.Fatal("expected loadTSConfig to still succeed using only the child's own settings")
	}
	if tc.BaseURL != "." {
		t.Errorf("expected child's own BaseURL %q, got %q", ".", tc.BaseURL)
	}
}

func TestLoadTSConfig_Extends_Cycle_DoesNotHang(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "a.json"), `{ "extends": "./b.json" }`)
	writeJSON(t, filepath.Join(root, "b.json"), `{ "extends": "./a.json" }`)
	writeJSON(t, filepath.Join(root, "tsconfig.json"), `{
		"extends": "./a.json",
		"compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
	}`)

	tc, ok := loadTSConfig(root)
	if !ok {
		t.Fatal("expected loadTSConfig to terminate and still return the child's own settings")
	}
	if tc.BaseURL != "." {
		t.Errorf("expected child's own BaseURL %q, got %q", ".", tc.BaseURL)
	}
}

func TestLoadTSConfig_NoExtends_StillWorks(t *testing.T) {
	root := t.TempDir()
	writeJSON(t, filepath.Join(root, "tsconfig.json"), `{
		"compilerOptions": { "baseUrl": ".", "paths": { "@/*": ["src/*"] } }
	}`)
	tc, ok := loadTSConfig(root)
	if !ok {
		t.Fatal("expected loadTSConfig to succeed")
	}
	if tc.BaseURL != "." || tc.Paths["@/*"][0] != "src/*" {
		t.Errorf("unexpected result: %+v", tc)
	}
}

func writeJSON(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
