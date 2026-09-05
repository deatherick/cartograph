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
// /api/duplicates, /api/similar, and /api/decide (a Web UI Duplicates
// view, closing docs/MVP.md's own "data exists since ADR-0021, but no UI
// panel reads it yet" gap) serve what internal/service's
// Duplicates/Similar/Decide already answer — the same Phase 5 similarity
// engine `ctx duplicates`/`similar`/`decide` use. Everything else here
// (Overview, Search, Entity Inspector, a bounded Graph view scoped to one
// entity's neighborhood — never the whole repo at once, a hundreds-of-
// nodes force layout is neither useful nor fast) predates that.
package httpserver

import (
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/service"
	"github.com/deatherick/cartograph/internal/similar"
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

// ProjectRegistry is a thread-safe, LIVE list of projects a running ctxd
// serves — mutable so a project can be added or removed while the server
// is already handling requests (ADR-0026, closing the "a real `ctxd
// project add/list` for an already-running daemon" gap docs/MVP.md and
// docs/requirements/phase9-global-install-and-daemon.md both named). A
// plain []Project (New's own shape before ADR-0026) would need the whole
// server rebuilt to reflect a change — every handler here instead reads a
// fresh Snapshot() per request, cheap at the scale a single local daemon
// ever serves (a handful of projects, not thousands).
type ProjectRegistry struct {
	mu       sync.RWMutex
	projects []Project
}

// NewProjectRegistry builds a registry seeded with initial — the set
// ctxd was told to watch at startup (from its own argv or, with none
// given, every project in `~/.cartograph/projects.json`, see cmd/ctxd's
// own doc). Later Set/Remove calls mutate it as projects are added to or
// removed from a live daemon.
func NewProjectRegistry(initial []Project) *ProjectRegistry {
	r := &ProjectRegistry{}
	r.projects = append([]Project(nil), initial...)
	sortProjects(r.projects)
	return r
}

// Snapshot returns every currently-registered project, sorted by Name for
// a stable, predictable /api/projects listing and a stable "first project
// is the default" choice — a copy, safe for the caller to read without
// holding any lock.
func (r *ProjectRegistry) Snapshot() []Project {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]Project(nil), r.projects...)
}

// Set adds p, or replaces the existing entry with the same Name — the
// same "re-adding points it at a new location" semantics
// internal/project.Add already uses, kept consistent here.
func (r *ProjectRegistry) Set(p Project) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, existing := range r.projects {
		if existing.Name == p.Name {
			r.projects[i] = p
			sortProjects(r.projects)
			return
		}
	}
	r.projects = append(r.projects, p)
	sortProjects(r.projects)
}

// Remove unregisters the project named name — a no-op if it wasn't
// registered, the same "removing something not there still achieves the
// caller's goal" convention internal/project.Remove uses.
func (r *ProjectRegistry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.projects[:0]
	for _, p := range r.projects {
		if p.Name != name {
			out = append(out, p)
		}
	}
	r.projects = out
}

func sortProjects(projects []Project) {
	sort.Slice(projects, func(i, j int) bool { return projects[i].Name < projects[j].Name })
}

// New builds the HTTP handler: the embedded static frontend at "/", and
// its JSON API under "/api/". registry must never be empty AT THE TIME OF
// A REQUEST for the no-?project= default to resolve — New itself doesn't
// enforce that at construction (unlike the old static-slice New, which
// panicked on an empty projects at startup): a registry that starts empty
// and gets its first project added moments later via Set is a real,
// intended shape now (a system-service ctxd started before `ctx project
// add` has ever been run), not a construction-time error. An actually
// empty registry at REQUEST time reports a clear 503, not a panic — see
// resolveProject below.
func New(svc *service.Service, registry *ProjectRegistry) http.Handler {
	mux := http.NewServeMux()

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		// Only possible if the embed directive itself is broken — a
		// packaging bug, not a runtime condition, matching how
		// internal/parser/ts and internal/parser/golang treat a malformed
		// embedded query: panic loudly at startup, not per-request.
		panic("httpserver: embedding web assets: " + err.Error())
	}
	mux.Handle("/", spaFallback(static))

	// resolveProject picks which registered Project a request means: its
	// ?project= name if given and known, the sole default (the first
	// project in the current, freshly-read Snapshot — sorted by Name, so
	// "first" is stable across requests even as projects are added/
	// removed) if omitted, or a clear 400 if a name was given but isn't
	// registered — never a silent fallback to the default in that case,
	// since that would make a typo look like a query against the wrong
	// project instead of an obvious error. A registry with NO projects at
	// all (a system-service ctxd started before anything was ever
	// registered) reports 503, not a panic or an index-out-of-range.
	resolveProject := func(w http.ResponseWriter, r *http.Request) (Project, bool) {
		projects := registry.Snapshot()
		if len(projects) == 0 {
			http.Error(w, "no projects registered yet — run `ctx project add` or pass one to ctxd directly", http.StatusServiceUnavailable)
			return Project{}, false
		}
		name := r.URL.Query().Get("project")
		if name == "" {
			return projects[0], true
		}
		for _, p := range projects {
			if p.Name == name {
				return p, true
			}
		}
		http.Error(w, "unknown project "+strconv.Quote(name), http.StatusBadRequest)
		return Project{}, false
	}

	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		type projectSummary struct {
			Name     string `json:"name"`
			Repo     string `json:"repo"`
			Root     string `json:"root"`
			Watching bool   `json:"watching"`
		}
		projects := registry.Snapshot()
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

	// /api/duplicates and /api/similar are the Web UI's Duplicates view
	// (docs/MVP.md's own "Duplicates view — its data now exists (ADR-0021),
	// but no UI panel reads it yet" gap, closed here) — the same
	// `service.Duplicates`/`service.Similar` the CLI's `ctx
	// duplicates`/`ctx similar` already call, so behavior never diverges
	// between the two interfaces (this package's own standing rule).
	mux.HandleFunc("/api/duplicates", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		threshold := 0.0
		if v := r.URL.Query().Get("threshold"); v != "" {
			if f, perr := strconv.ParseFloat(v, 64); perr == nil {
				threshold = f
			}
		}
		pairs, err := svc.Duplicates(p.Root, p.Repo, threshold)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, pairs)
	})

	mux.HandleFunc("/api/similar", func(w http.ResponseWriter, r *http.Request) {
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		pairs, match, err := svc.Similar(p.Root, p.Repo, name, r.URL.Query().Get("file"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, struct {
			Match model.Entity               `json:"match"`
			Pairs []service.PairWithEntities `json:"pairs"`
		}{Match: match, Pairs: pairs})
	})

	// /api/decide is this server's first (and so far only) mutating
	// endpoint — POST, not GET, deliberately: every other handler above
	// only ever reads the snapshot, but recording a human's decision on a
	// pair (never resurface it via Duplicates/Similar again) is a real
	// state change, and a GET a browser/proxy might prefetch or cache
	// must never carry that risk.
	mux.HandleFunc("/api/decide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed — use POST", http.StatusMethodNotAllowed)
			return
		}
		p, ok := resolveProject(w, r)
		if !ok {
			return
		}
		q := r.URL.Query()
		nameA, nameB := q.Get("nameA"), q.Get("nameB")
		if nameA == "" || nameB == "" {
			http.Error(w, "missing ?nameA=/&nameB=", http.StatusBadRequest)
			return
		}
		decision, ok := similar.ParseDecision(q.Get("decision"))
		if !ok {
			http.Error(w, fmt.Sprintf("invalid ?decision= — must be one of: %s", similar.ValidDecisions()), http.StatusBadRequest)
			return
		}
		if err := svc.Decide(p.Root, p.Repo, nameA, q.Get("fileA"), nameB, q.Get("fileB"), decision); err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, struct {
			OK bool `json:"ok"`
		}{OK: true})
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

// spaFallback wraps an http.FileServer over the embedded React build so a
// direct request for a CLIENT-SIDE route (react-router's /graph, /impact,
// /duplicates — anything the browser's own history/URL bar names, not a
// link the SPA itself navigated to) serves index.html instead of a bare
// 404. A plain http.FileServer only knows about real files in the
// embedded fs — it has no idea /duplicates is a route react-router
// resolves client-side, not a path on disk. Found live, not in a code
// review: opening http://127.0.0.1:7420/duplicates directly (a fresh
// page load, exactly what a bookmark or a page refresh does) 404'd, and
// so did the PRE-EXISTING /graph and /impact routes — this was already a
// real bug before this ADR's Duplicates page existed, just never
// surfaced because every route was always reached by first loading `/`
// and clicking through, never by a fresh load.
//
// A request whose path has a file extension (`.js`, `.css`, an image, ...)
// is assumed to be a real static asset and gets the file server's own
// 404 if it's genuinely missing — only an extension-less path (a route)
// falls back to index.html, so a typo'd asset URL still fails loudly
// instead of silently serving the SPA shell.
func spaFallback(fsys fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(fsys))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(r.URL.Path, "/")
		if p == "" {
			fileServer.ServeHTTP(w, r)
			return
		}
		if _, err := fs.Stat(fsys, p); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		if strings.Contains(path.Base(p), ".") {
			// Looks like a real asset request (has a file extension) that
			// genuinely doesn't exist — a real 404, not a client route.
			fileServer.ServeHTTP(w, r)
			return
		}
		index, err := fsys.Open("index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer func() { _ = index.Close() }()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.Copy(w, index)
	})
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
