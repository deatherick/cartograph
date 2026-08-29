// Package httpserver is the thin HTTP adapter over internal/service that
// serves Phase 6's web UI — the same "CLI/MCP/HTTP are thin adapters,
// never duplicating logic" rule internal/mcpserver already follows,
// applied to HTTP. Every handler here calls straight into
// internal/service; none of them compute anything internal/service
// doesn't already expose.
//
// Multi-project since ADR-0019: New takes a slice of Project (name, repo,
// root, and an optional per-project opstatus.Tracker) rather than one
// fixed root/repo pair. Every /api/ handler reads an optional ?project=
// query parameter to pick which registered project it answers for
// (defaulting to the first one in the slice, so a single-project caller —
// still the common case — never has to pass it). /api/projects lists what
// is available, for a frontend project switcher to populate itself with.
//
// Duplicates and Impact-by-entity-name views from the original
// requirements capture are also deferred — they depend on Phase 4 (impact
// analysis, since done) and Phase 5 (similarity engine, not yet); this
// only serves what internal/service can already answer: Overview, Search,
// Entity Inspector, and a bounded Graph view scoped to one entity's
// neighborhood (never the whole repo at once — a hundreds-of-nodes force
// layout is neither useful nor fast).
package httpserver

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/service"
)

//go:embed web
var webFS embed.FS

// Project is one project New serves — the daemon-side analog of a single
// row in internal/project's CLI-only registry (ADR-0016), except this one
// is live: Ops is set once that project is actually being watched.
type Project struct {
	// Name identifies this project in the ?project= query parameter and
	// in /api/projects' listing — the registered short name if the caller
	// resolved one (internal/project.Resolve), else the repo's own name
	// (service.RepoName's derivation), so a single unregistered project
	// still gets a sensible default identity.
	Name string
	Repo string
	Root string
	// Ops is optional (nil is fine) — a project New was told about but
	// that has no daemon lifecycle to report (e.g. a caller that only
	// ever calls svc.Index once, never watches). Its /api/operations
	// slice reports 404 in that case, same as the pre-multi-project
	// behavior (ADR-0018).
	Ops *opstatus.Tracker
}

// New builds the HTTP handler: the embedded static frontend at "/", and
// its JSON API under "/api/". projects must be non-empty; the first
// element is the default project used whenever a request's ?project=
// is empty or omitted, so a single-project caller (still the common case)
// never needs to pass it at all. See the package doc for the multi-project
// query-parameter contract.
func New(svc *service.Service, projects []Project) http.Handler {
	if len(projects) == 0 {
		panic("httpserver: New requires at least one Project")
	}
	mux := http.NewServeMux()

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		// Only possible if the embed directive itself is broken — a
		// packaging bug, not a runtime condition, matching how
		// internal/parser/ts and internal/parser/golang treat a malformed
		// embedded query: panic loudly at startup, not per-request.
		panic("httpserver: embedding web assets: " + err.Error())
	}
	mux.Handle("/", http.FileServer(http.FS(static)))

	byName := make(map[string]Project, len(projects))
	for _, p := range projects {
		byName[p.Name] = p
	}
	// resolveProject picks which registered Project a request means: its
	// ?project= name if given and known, the sole default (projects[0])
	// if omitted, or a clear 400 if a name was given but isn't registered
	// — never a silent fallback to the default in that case, since that
	// would make a typo look like a query against the wrong project
	// instead of an obvious error.
	resolveProject := func(w http.ResponseWriter, r *http.Request) (Project, bool) {
		name := r.URL.Query().Get("project")
		if name == "" {
			return projects[0], true
		}
		p, ok := byName[name]
		if !ok {
			http.Error(w, "unknown project "+strconv.Quote(name), http.StatusBadRequest)
			return Project{}, false
		}
		return p, true
	}

	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		type projectSummary struct {
			Name     string `json:"name"`
			Repo     string `json:"repo"`
			Root     string `json:"root"`
			Watching bool   `json:"watching"`
		}
		out := make([]projectSummary, len(projects))
		for i, p := range projects {
			s := projectSummary{Name: p.Name, Repo: p.Repo, Root: p.Root}
			if p.Ops != nil {
				s.Watching = p.Ops.Snapshot().Watching
			}
			out[i] = s
		}
		writeJSON(w, out)
	})

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		g, err := svc.Graph(p.Root, p.Repo)
		if err != nil {
			writeError(w, err)
			return
		}
		byKind := map[model.Kind]int{}
		for _, e := range g.Entities {
			byKind[e.Kind]++
		}
		writeJSON(w, struct {
			Repo     string             `json:"repo"`
			Entities int                `json:"entities"`
			Edges    int                `json:"edges"`
			ByKind   map[model.Kind]int `json:"byKind"`
		}{Repo: p.Repo, Entities: len(g.Entities), Edges: len(g.Edges), ByKind: byKind})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		g, err := svc.Graph(p.Root, p.Repo)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, g)
	})

	mux.HandleFunc("/api/find", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		entities, err := svc.Find(p.Root, p.Repo, name)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, entities)
	})

	mux.HandleFunc("/api/inspect", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		insp, err := svc.Inspect(p.Root, p.Repo, name, r.URL.Query().Get("file"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, insp)
	})

	mux.HandleFunc("/api/related", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		depth := 2
		if v := r.URL.Query().Get("depth"); v != "" {
			if d, perr := strconv.Atoi(v); perr == nil && d > 0 {
				depth = d
			}
		}
		related, err := svc.Related(p.Root, p.Repo, name, r.URL.Query().Get("file"), depth)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, related)
	})

	mux.HandleFunc("/api/impact", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		depth := 0 // full transitive closure by default
		if v := r.URL.Query().Get("depth"); v != "" {
			if d, perr := strconv.Atoi(v); perr == nil {
				depth = d
			}
		}
		if gitRef, ok := r.URL.Query()["gitDiff"]; ok {
			result, err := svc.ImpactFromGitDiff(p.Root, p.Repo, gitRef[0], depth)
			if err != nil {
				writeError(w, err)
				return
			}
			writeJSON(w, result)
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name= (or ?gitDiff=<ref>)", http.StatusBadRequest)
			return
		}
		result, err := svc.Impact(p.Root, p.Repo, name, r.URL.Query().Get("file"), depth)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})

	mux.HandleFunc("/api/source", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		src, entity, err := svc.Source(p.Root, p.Repo, name, r.URL.Query().Get("file"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, struct {
			Entity model.Entity `json:"entity"`
			Source string       `json:"source"`
		}{Entity: entity, Source: src})
	})

	mux.HandleFunc("/api/operations", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		if p.Ops == nil {
			http.Error(w, "operations status not available (no daemon running for this project)", http.StatusNotFound)
			return
		}
		writeJSON(w, p.Ops.Snapshot())
	})

	return mux
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// writeError reports a service-layer error as JSON with a 4xx status —
// every error internal/service returns today is a caller-fixable
// condition (ambiguous name, no snapshot, not found), never an internal
// fault, so 400 is used uniformly rather than guessing at a more specific
// code per error string.
func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(struct {
		Error string `json:"error"`
	}{Error: err.Error()})
}
