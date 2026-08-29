// Command ctxd is the context engine daemon — Phase 3d's vertical slice,
// since ADR-0020 upgraded to TRUE per-file incremental indexing: index
// once, then watch each project's source (internal/watch) and re-index
// automatically whenever it changes, re-processing only the files a
// change actually affects (internal/index.Indexer) instead of re-walking
// and re-resolving the whole project every time.
//
// Multi-project since ADR-0019: ctxd accepts more than one <path>
// argument, one project each, and watches all of them concurrently from
// one process. Each <path> also accepts a name registered via `ctx
// project add` (internal/project.Resolve), same as every `ctx` CLI
// command already does.
//
// Still no system-level installation (launchd/systemd) and no daemon
// socket/RPC — every project's status is reachable only through the same
// HTTP server the web UI already uses (--web). Phase 9 scope (global
// install, running as a system service) is captured in
// docs/requirements/phase9-global-install-and-daemon.md at the user's
// explicit request but not built.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/deatherick/cartograph/internal/httpserver"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/project"
	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
	"github.com/deatherick/cartograph/internal/store"
	"github.com/deatherick/cartograph/internal/watch"
)

func main() {
	webAddr := flag.String("web", "127.0.0.1:7420", "address to serve the web UI on (127.0.0.1-only by default — see the project plan's permanent 'bind to localhost' restriction); empty disables it")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ctxd [--web addr] <path> [<path>...]")
		fmt.Fprintln(os.Stderr, "\nIndexes each <path> once, then watches it and incrementally re-indexes on every change until interrupted (Ctrl+C).")
		fmt.Fprintln(os.Stderr, "Each <path> also accepts a name registered via `ctx project add`.")
		fmt.Fprintln(os.Stderr, "Also serves a web UI (Phase 6) at --web (default 127.0.0.1:7420) unless --web=\"\", with a project switcher when more than one <path> is given.")
		os.Exit(2)
	}

	svc := service.New() // the HTTP/Web UI read side only — never used for indexing itself, see watchProject
	handles := make([]httpserver.Project, len(args))
	for i, arg := range args {
		root := project.Resolve(arg)
		repo := service.RepoName(root)
		name := repo
		if root != arg {
			name = arg // arg was a registered project name — keep it as the friendlier identity
		}
		handles[i] = httpserver.Project{Name: name, Repo: repo, Root: root, Ops: opstatus.New()}
	}

	if *webAddr != "" {
		go func() {
			handler := httpserver.New(svc, handles)
			fmt.Printf("ctxd: web UI at http://%s (operational status: /api/operations)\n", *webAddr)
			if err := http.ListenAndServe(*webAddr, handler); err != nil { //nolint:gosec // 127.0.0.1 default binding is the security boundary here, not timeouts on a local single-user dev tool
				fmt.Fprintf(os.Stderr, "ctxd: web UI server error: %v\n", err)
			}
		}()
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	done := make(chan struct{})

	var wg sync.WaitGroup
	for _, h := range handles {
		wg.Add(1)
		go func(h httpserver.Project) {
			defer wg.Done()
			watchProject(done, h)
		}(h)
	}

	<-sig
	fmt.Println("ctxd: shutting down")
	close(done)
	wg.Wait()
}

// watchProject runs one project's whole lifecycle: an initial full index
// (index.Indexer.FullIndex), then incremental updates
// (index.Indexer.UpdateFiles) as its watcher reports changed paths, each
// persisted via store.Write, until done is closed. Runs in its own
// goroutine so ctxd's multiple projects (ADR-0019) watch fully
// concurrently, each with its own opstatus.Tracker (h.Ops) and its own
// live Indexer (h.Root/h.Repo scope everything to this one project —
// nothing here is shared across projects).
func watchProject(done <-chan struct{}, h httpserver.Project) {
	label := h.Repo
	if h.Name != h.Repo {
		label = h.Name + "/" + h.Repo
	}
	ix := index.NewIndexer(h.Root, h.Repo)

	// persist writes ix's current graph to the same snapshot path
	// internal/service.Index would have used — internal/httpserver's read
	// side (svc.Stats/svc.Related/...) opens that same path, so it never
	// needs to know an Indexer exists at all.
	persist := func(stats index.Stats) error {
		path, err := store.SnapshotPath(h.Root, h.Repo)
		if err != nil {
			return fmt.Errorf("resolving snapshot path: %w", err)
		}
		meta := store.Meta{Files: stats.Files, Dispositions: stats.Dispositions}
		if err := store.Write(path, h.Repo, ix.Graph(), meta); err != nil {
			return fmt.Errorf("persisting snapshot: %w", err)
		}
		return nil
	}

	// finish is the shared tail of both the initial index and every
	// incremental update: persist, then record success/failure to h.Ops
	// (a persist failure always wins over a milder per-file extraction
	// error, since it means the snapshot on disk did NOT change — the
	// most important fact an operator needs), then print the same
	// IndexStats summary a full `ctx index` run would show.
	finish := func(reason string, stats index.Stats, updateErr error) {
		if err := persist(stats); err != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: %v\n", label, err)
			h.Ops.RecordReindexFailure(reason, err)
			return
		}
		if updateErr != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: %s: %v\n", label, reason, updateErr)
			h.Ops.RecordReindexFailure(reason, updateErr)
		} else {
			h.Ops.RecordReindexSuccess(reason, stats)
		}
		fmt.Print(render.IndexStats(stats))
	}

	fmt.Printf("ctxd[%s]: initial index — indexing %s\n", label, h.Root)
	func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats, err := ix.FullIndex(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: index error: %v\n", label, err)
			h.Ops.RecordReindexFailure("initial index", err)
			return
		}
		finish("initial index", stats, nil)
	}()

	w, err := watch.New(h.Root, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxd[%s]: starting watcher: %v\n", label, err)
		return
	}
	defer func() { _ = w.Close() }()
	h.Ops.SetWatching(true)

	fmt.Printf("ctxd[%s]: watching %s for changes\n", label, h.Root)
	for {
		select {
		case paths := <-w.Events():
			func() {
				ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
				defer cancel()
				stats, updateErr := ix.UpdateFiles(ctx, paths)
				finish("change detected", stats, updateErr)
			}()
		case werr := <-w.Errors():
			fmt.Fprintf(os.Stderr, "ctxd[%s]: watch error: %v\n", label, werr)
			h.Ops.RecordWatchError(werr)
		case <-done:
			h.Ops.SetWatching(false)
			return
		}
	}
}
