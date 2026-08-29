package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_LoadSave_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadConfig(dir); ok {
		t.Fatal("expected no config in a fresh temp dir")
	}
	want := Config{Languages: []string{"go", "typescript"}}
	if err := SaveConfig(dir, want); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, ok := LoadConfig(dir)
	if !ok {
		t.Fatal("expected LoadConfig to find the just-saved config")
	}
	if len(got.Languages) != 2 || got.Languages[0] != "go" || got.Languages[1] != "typescript" {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestConfig_MalformedFile_FailsSoftNotHard(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, ConfigFileName), "not json"); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadConfig(dir); ok {
		t.Fatal("expected ok=false for a malformed config file, not a panic or a zero-value success")
	}
}

func TestEnabledLanguages_ZeroConfig_IsEveryDetectedLanguage(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, "go.mod"), "module example.com/x\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}
	langs := enabledLanguages(dir)
	if len(langs) != 1 || langs[0].Name != "go" {
		t.Fatalf("expected only go detected, got %+v", names(langs))
	}
}

func TestEnabledLanguages_ExplicitConfig_Narrows(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(filepath.Join(dir, "go.mod"), "module example.com/x\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "main.go"), "package main\nfunc main() {}\n"); err != nil {
		t.Fatal(err)
	}
	if err := writeFile(filepath.Join(dir, "tsconfig.json"), "{}"); err != nil {
		t.Fatal(err)
	}
	// Both go and typescript are detectable here, but the config explicitly
	// narrows to go only — a user opting a language OUT must actually
	// disable it, not just have it filtered from output.
	if err := SaveConfig(dir, Config{Languages: []string{"go"}}); err != nil {
		t.Fatal(err)
	}
	langs := enabledLanguages(dir)
	if len(langs) != 1 || langs[0].Name != "go" {
		t.Fatalf("expected config to narrow to go only, got %+v", names(langs))
	}
}

func TestEnabledLanguages_UnknownNameInConfig_IsIgnoredNotFatal(t *testing.T) {
	dir := t.TempDir()
	if err := SaveConfig(dir, Config{Languages: []string{"cobol"}}); err != nil {
		t.Fatal(err)
	}
	langs := enabledLanguages(dir) // must not panic
	if len(langs) != 0 {
		t.Fatalf("expected zero languages for an unrecognized config entry, got %+v", names(langs))
	}
}

func names(langs []Language) []string {
	out := make([]string, len(langs))
	for i, l := range langs {
		out[i] = l.Name
	}
	return out
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}
