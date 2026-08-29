package httpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/service"
)

// setup indexes a small real TypeScript fixture and returns an httptest
// server backed by internal/httpserver — an integration test against the
// real service layer, not mocked handlers, matching internal/mcpserver's
// own testing convention (real transports, real service calls).
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
	return httptest.NewServer(New(svc, root, repo))
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

// TestHTTPServer_MissingIndex_ReturnsClearError confirms the "run ctx
// index first" contract survives through HTTP, not just the CLI/MCP
// paths — a project with no snapshot yet must not panic or 500.
func TestHTTPServer_MissingIndex_ReturnsClearError(t *testing.T) {
	root := t.TempDir()
	svc := service.New()
	srv := httptest.NewServer(New(svc, root, service.RepoName(root)))
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
