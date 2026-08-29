// Package httpserver is the thin HTTP adapter over internal/service that
// serves Phase 6's web UI — the same "CLI/MCP/HTTP are thin adapters,
// never duplicating logic" rule internal/mcpserver already follows,
// applied to HTTP. Every handler here calls straight into
// internal/service; none of them compute anything internal/service
// doesn't already expose.
//
// V0 scope, stated plainly (docs/requirements/phase6-web-ui.md): this
// serves ONE project, fixed at construction time — root/repo are set once
// by New's caller (cmd/ctxd), matching that binary's own current
// single-project scope (no multi-project registry yet, ADR-0012). A
// "Projects" view/endpoint is deferred until that exists. Duplicates and
// Impact views from the original requirements capture are also deferred —
// they depend on Phase 4 (impact analysis) and Phase 5 (similarity
// engine), neither built yet; this only serves what internal/service can
// already answer: Overview, Search, Entity Inspector, and a bounded Graph
// view scoped to one entity's neighborhood (never the whole repo at
// once — a hundreds-of-nodes force layout is neither useful nor fast).
package httpserver

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strconv"

	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/service"
)

//go:embed web
var webFS embed.FS

// New builds the HTTP handler: the embedded static frontend at "/", and
// its JSON API under "/api/". root/repo are fixed for the lifetime of the
// handler — see the package doc's V0 scope note.
func New(svc *service.Service, root, repo string) http.Handler {
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

	mux.HandleFunc("/api/stats", func(w http.ResponseWriter, r *http.Request) {
		g, err := svc.Graph(root, repo)
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
		}{Repo: repo, Entities: len(g.Entities), Edges: len(g.Edges), ByKind: byKind})
	})

	mux.HandleFunc("/api/graph", func(w http.ResponseWriter, r *http.Request) {
		g, err := svc.Graph(root, repo)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, g)
	})

	mux.HandleFunc("/api/find", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		entities, err := svc.Find(root, repo, name)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, entities)
	})

	mux.HandleFunc("/api/inspect", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		insp, err := svc.Inspect(root, repo, name, r.URL.Query().Get("file"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, insp)
	})

	mux.HandleFunc("/api/related", func(w http.ResponseWriter, r *http.Request) {
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
		related, err := svc.Related(root, repo, name, r.URL.Query().Get("file"), depth)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, related)
	})

	mux.HandleFunc("/api/impact", func(w http.ResponseWriter, r *http.Request) {
		depth := 0 // full transitive closure by default
		if v := r.URL.Query().Get("depth"); v != "" {
			if d, perr := strconv.Atoi(v); perr == nil {
				depth = d
			}
		}
		if gitRef, ok := r.URL.Query()["gitDiff"]; ok {
			result, err := svc.ImpactFromGitDiff(root, repo, gitRef[0], depth)
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
		result, err := svc.Impact(root, repo, name, r.URL.Query().Get("file"), depth)
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, result)
	})

	mux.HandleFunc("/api/source", func(w http.ResponseWriter, r *http.Request) {
		name := r.URL.Query().Get("name")
		if name == "" {
			http.Error(w, "missing ?name=", http.StatusBadRequest)
			return
		}
		src, entity, err := svc.Source(root, repo, name, r.URL.Query().Get("file"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, struct {
			Entity model.Entity `json:"entity"`
			Source string       `json:"source"`
		}{Entity: entity, Source: src})
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
