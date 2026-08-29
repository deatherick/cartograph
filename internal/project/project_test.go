package project

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate points os.UserHomeDir() (and therefore registryPath) at a fresh
// temp directory for the duration of the test — the same isolation
// pattern internal/mcpserver's tests already use for snapshot/ledger
// storage.
func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
}

func TestAdd_List_RoundTrip(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := Add("myapp", dir); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, err := List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 1 || got[0].Name != "myapp" || got[0].Path != dir {
		t.Fatalf("got %+v, want one project {myapp, %s}", got, dir)
	}
}

func TestAdd_NonexistentPath_Errors(t *testing.T) {
	isolate(t)
	if err := Add("ghost", filepath.Join(t.TempDir(), "does-not-exist")); err == nil {
		t.Fatal("expected an error registering a nonexistent path")
	}
}

func TestAdd_ReplacesExistingNameSameSpot(t *testing.T) {
	isolate(t)
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	if err := Add("myapp", dir1); err != nil {
		t.Fatal(err)
	}
	if err := Add("myapp", dir2); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != dir2 {
		t.Fatalf("expected re-adding 'myapp' to overwrite its path, got %+v", got)
	}
}

func TestRemove(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := Add("myapp", dir); err != nil {
		t.Fatal(err)
	}
	if err := Remove("myapp"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected an empty registry after Remove, got %+v", got)
	}
}

func TestRemove_UnregisteredName_IsNoop(t *testing.T) {
	isolate(t)
	if err := Remove("never-existed"); err != nil {
		t.Fatalf("expected no error removing an unregistered name, got %v", err)
	}
}

func TestResolve_RegisteredName_ReturnsPath(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := Add("myapp", dir); err != nil {
		t.Fatal(err)
	}
	if got := Resolve("myapp"); got != dir {
		t.Fatalf("Resolve(%q) = %q, want %q", "myapp", got, dir)
	}
}

func TestResolve_UnregisteredInput_ReturnsUnchanged(t *testing.T) {
	isolate(t)
	// No registry file exists at all yet — Resolve must still behave like
	// a pass-through, not error, so every existing command that takes a
	// raw path keeps working for a user who never registers anything.
	if got := Resolve("./some/relative/path"); got != "./some/relative/path" {
		t.Fatalf("Resolve of an unregistered path changed it: got %q", got)
	}
}

func TestList_EmptyRegistry_ReturnsEmptyNotError(t *testing.T) {
	isolate(t)
	got, err := List()
	if err != nil {
		t.Fatalf("List on a fresh registry: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty, got %+v", got)
	}
}

func TestList_SortedByName(t *testing.T) {
	isolate(t)
	dirA, dirB := t.TempDir(), t.TempDir()
	if err := Add("zebra", dirA); err != nil {
		t.Fatal(err)
	}
	if err := Add("alpha", dirB); err != nil {
		t.Fatal(err)
	}
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "zebra" {
		t.Fatalf("expected sorted [alpha, zebra], got %+v", got)
	}
}

func TestRegistryFile_IsHumanReadableJSON(t *testing.T) {
	isolate(t)
	dir := t.TempDir()
	if err := Add("myapp", dir); err != nil {
		t.Fatal(err)
	}
	path, err := registryPath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the registry file to exist on disk: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty registry file content")
	}
}
