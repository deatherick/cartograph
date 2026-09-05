package index

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCSharpProjects_ParsesProjectReferences(t *testing.T) {
	root := t.TempDir()
	mustMkdirAll(t, filepath.Join(root, "src", "Core"))
	mustMkdirAll(t, filepath.Join(root, "src", "Web"))
	mustWriteFile(t, filepath.Join(root, "src", "Core", "Core.csproj"), `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <RootNamespace>App.Core</RootNamespace>
  </PropertyGroup>
</Project>`)
	// Backslash path separators, exactly as real .csproj files on disk
	// use (verified against eShopOnWeb's own real files) — not a
	// forward-slash convenience invented for this test.
	mustWriteFile(t, filepath.Join(root, "src", "Web", "Web.csproj"), `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <RootNamespace>App.Web</RootNamespace>
  </PropertyGroup>
  <ItemGroup>
    <ProjectReference Include="..\Core\Core.csproj" />
  </ItemGroup>
</Project>`)

	projects := loadCSharpProjects(root)
	byDir := map[string]csharpProject{}
	for _, p := range projects {
		byDir[p.Dir] = p
	}

	web, ok := byDir["src/Web"]
	if !ok {
		t.Fatalf("expected a project at src/Web, got: %+v", projects)
	}
	if len(web.ProjectReferences) != 1 || web.ProjectReferences[0] != "src/Core" {
		t.Errorf("got ProjectReferences %v, want [src/Core]", web.ProjectReferences)
	}

	core, ok := byDir["src/Core"]
	if !ok {
		t.Fatalf("expected a project at src/Core, got: %+v", projects)
	}
	if len(core.ProjectReferences) != 0 {
		t.Errorf("expected src/Core to have no ProjectReferences, got %v", core.ProjectReferences)
	}
}

func TestLoadCSharpProjects_NoProjectReferences_IsNilNotError(t *testing.T) {
	root := t.TempDir()
	mustWriteFile(t, filepath.Join(root, "Solo.csproj"), `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <RootNamespace>Solo</RootNamespace>
  </PropertyGroup>
</Project>`)

	projects := loadCSharpProjects(root)
	if len(projects) != 1 {
		t.Fatalf("got %d projects, want 1: %+v", len(projects), projects)
	}
	if len(projects[0].ProjectReferences) != 0 {
		t.Errorf("expected no ProjectReferences, got %v", projects[0].ProjectReferences)
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
