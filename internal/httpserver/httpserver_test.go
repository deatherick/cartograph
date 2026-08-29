package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/service"
)

// setup indexes a small real TypeScript fixture and returns an httptest
// server backed by internal/httpserver — an integration test against the
// real service layer, not mocked handlers, matching internal/mcpserver's
// own testing convention (real transports, real service calls). Single
// project, ops omitted — every existing (pre-multi-project) test here
// keeps working against a one-element []Project with no ?project=
// needed, since resolveProject defaults to projects[0].
func setup(t *testing.T) *httptest.Server {
	t.Helper()
	root := t.TempDir()
	src := "export function helper(): string { return greet(); }\nexport function greet(): string { return 'hi'; }\n"
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.New()
	repo := service.RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return httptest.NewServer(New(svc, []Project{{Name: repo, Repo: repo, Root: root}}))
}

func TestHTTPServer_Stats(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	var got struct {
		Entities int
		Edges    int
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Entities != 2 {
		t.Fatalf("expected 2 entities, got %d", got.Entities)
	}
	if got.Edges != 1 {
		t.Fatalf("expected 1 edge (helper calls greet), got %d", got.Edges)
	}
}

func TestHTTPServer_Graph(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/graph")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var got service.Graph
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entities) != 2 || len(got.Edges) != 1 {
		t.Fatalf("got %d entities, %d edges", len(got.Entities), len(got.Edges))
	}
}

func TestHTTPServer_Inspect(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/inspect?name=greet")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	var insp service.Inspection
	if err := json.NewDecoder(res.Body).Decode(&insp); err != nil {
		t.Fatal(err)
	}
	if insp.Entity.Name != "greet" {
		t.Fatalf("got entity %q, want greet", insp.Entity.Name)
	}
	if len(insp.FanIn) != 1 {
		t.Fatalf("expected 1 fan-in edge (helper -> greet), got %d", len(insp.FanIn))
	}
}

func TestHTTPServer_Inspect_UnknownName_Is400(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/inspect?name=doesNotExist")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400", res.StatusCode)
	}
	var body struct{ Error string }
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error == "" {
		t.Fatal("expected a non-empty error message")
	}
}

func TestHTTPServer_Related(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/related?name=helper&depth=2")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var related []model.RelatedEntity
	if err := json.NewDecoder(res.Body).Decode(&related); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range related {
		if r.Entity.Name == "greet" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected greet in helper's related set, got %+v", related)
	}
}

func TestHTTPServer_Impact(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/impact?name=greet")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d", res.StatusCode)
	}
	var impact service.ImpactResult
	if err := json.NewDecoder(res.Body).Decode(&impact); err != nil {
		t.Fatal(err)
	}
	if impact.Target.Name != "greet" {
		t.Fatalf("got target %q, want greet", impact.Target.Name)
	}
	if len(impact.DirectCallers) != 1 || impact.DirectCallers[0].Name != "helper" {
		t.Fatalf("expected helper as greet's direct caller, got %+v", impact.DirectCallers)
	}
}

func TestHTTPServer_Source(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/source?name=greet")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var got struct {
		Entity model.Entity
		Source string
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Source == "" {
		t.Fatal("expected non-empty source")
	}
}

func TestHTTPServer_ServesEmbeddedFrontend(t *testing.T) {
	srv := setup(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d for /", res.StatusCode)
	}
	ct := res.Header.Get("Content-Type")
	if ct == "" {
		t.Fatal("expected a Content-Type header for the served index.html")
	}
}

func TestHTTPServer_Operations_NilTracker_Returns404(t *testing.T) {
	srv := setup(t) // setup passes no Ops for its one project
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/operations")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("got status %d, want 404 when no opstatus.Tracker was supplied", res.StatusCode)
	}
}

func TestHTTPServer_Operations_WithTracker_ReportsStatus(t *testing.T) {
	root := t.TempDir()
	src := "export function helper(): string { return 'hi'; }\n"
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := service.New()
	repo := service.RepoName(root)
	stats, err := svc.Index(t.Context(), root, repo)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	ops := opstatus.New()
	ops.SetWatching(true)
	ops.RecordReindexSuccess("initial index", stats)

	srv := httptest.NewServer(New(svc, []Project{{Name: repo, Repo: repo, Root: root, Ops: ops}}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/operations")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", res.StatusCode)
	}
	var got opstatus.Status
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Watching {
		t.Error("expected Watching=true")
	}
	if got.ReindexCount != 1 {
		t.Errorf("ReindexCount = %d, want 1", got.ReindexCount)
	}
	if got.LastReason != "initial index" {
		t.Errorf("LastReason = %q, want %q", got.LastReason, "initial index")
	}
}

// TestHTTPServer_MissingIndex_ReturnsClearError confirms the "run ctx
// index first" contract survives through HTTP, not just the CLI/MCP
// paths — a project with no snapshot yet must not panic or 500.
func TestHTTPServer_MissingIndex_ReturnsClearError(t *testing.T) {
	root := t.TempDir()
	svc := service.New()
	repo := service.RepoName(root)
	srv := httptest.NewServer(New(svc, []Project{{Name: repo, Repo: repo, Root: root}}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400 for a missing snapshot", res.StatusCode)
	}
}

// setupTwoProjects indexes two distinct, differently-sized TS fixtures and
// returns a server serving both — the multi-project scenario ADR-0019
// adds: same svc, two independent repos, selected via ?project=.
func setupTwoProjects(t *testing.T) (*httptest.Server, string, string) {
	t.Helper()
	svc := service.New()

	rootA := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootA, "a.ts"), []byte(
		"export function helper(): string { return greet(); }\nexport function greet(): string { return 'hi'; }\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	repoA := "project-a"
	if _, err := svc.Index(t.Context(), rootA, repoA); err != nil {
		t.Fatalf("Index A: %v", err)
	}

	rootB := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootB, "b.ts"), []byte(
		"export function alpha(): void {}\nexport function beta(): void {}\nexport function gamma(): void {}\n",
	), 0o644); err != nil {
		t.Fatal(err)
	}
	repoB := "project-b"
	if _, err := svc.Index(t.Context(), rootB, repoB); err != nil {
		t.Fatalf("Index B: %v", err)
	}

	opsA := opstatus.New()
	opsA.SetWatching(true)
	srv := httptest.NewServer(New(svc, []Project{
		{Name: "a", Repo: repoA, Root: rootA, Ops: opsA},
		{Name: "b", Repo: repoB, Root: rootB}, // no Ops — 404 on /api/operations?project=b
	}))
	return srv, "a", "b"
}

func TestHTTPServer_MultiProject_ListsBoth(t *testing.T) {
	srv, nameA, nameB := setupTwoProjects(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var got []struct {
		Name     string `json:"name"`
		Repo     string `json:"repo"`
		Watching bool   `json:"watching"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects listed, got %+v", got)
	}
	byName := map[string]bool{}
	for _, p := range got {
		byName[p.Name] = p.Watching
	}
	if watching, ok := byName[nameA]; !ok || !watching {
		t.Errorf("expected project %q listed and watching=true, got %+v", nameA, got)
	}
	if watching, ok := byName[nameB]; !ok || watching {
		t.Errorf("expected project %q listed and watching=false (no Ops), got %+v", nameB, got)
	}
}

func TestHTTPServer_MultiProject_StatsAreScopedPerProject(t *testing.T) {
	srv, nameA, nameB := setupTwoProjects(t)
	defer srv.Close()

	getEntities := func(project string) int {
		res, err := http.Get(srv.URL + "/api/stats?project=" + project)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = res.Body.Close() }()
		var got struct{ Entities int }
		if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		return got.Entities
	}

	if n := getEntities(nameA); n != 2 {
		t.Errorf("project a: expected 2 entities, got %d", n)
	}
	if n := getEntities(nameB); n != 3 {
		t.Errorf("project b: expected 3 entities, got %d", n)
	}

	// No ?project= at all must fall back to the first project (a), not error.
	res, err := http.Get(srv.URL + "/api/stats")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	var got struct{ Entities int }
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Entities != 2 {
		t.Errorf("expected default (no ?project=) to resolve to project a's 2 entities, got %d", got.Entities)
	}
}

func TestHTTPServer_MultiProject_UnknownProject_Is400(t *testing.T) {
	srv, _, _ := setupTwoProjects(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/stats?project=does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("got status %d, want 400 for an unregistered ?project=", res.StatusCode)
	}
}

func TestHTTPServer_MultiProject_OperationsIsPerProject(t *testing.T) {
	srv, nameA, nameB := setupTwoProjects(t)
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/operations?project=" + nameA)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("project a: got status %d, want 200 (it has an Ops tracker)", res.StatusCode)
	}

	res2, err := http.Get(srv.URL + "/api/operations?project=" + nameB)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res2.Body.Close() }()
	if res2.StatusCode != http.StatusNotFound {
		t.Fatalf("project b: got status %d, want 404 (no Ops tracker)", res2.StatusCode)
	}
}

func TestHTTPServer_New_PanicsWithNoProjects(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected New to panic when given an empty []Project")
		}
	}()
	New(service.New(), nil)
}
