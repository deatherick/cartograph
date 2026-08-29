// Command ctxd is the context engine daemon — Phase 3d's vertical slice:
// index once, then watch each project's source (internal/watch) and
// re-index automatically whenever it changes, so a snapshot never goes
// stale while ctxd is running for that project.
//
// SCOPING, stated plainly: this re-indexes the WHOLE project on every
// change, not just the changed file(s). The project plan's own stated
// restriction is "never a full reindex for a small change" — this V0
// deliberately does exactly that, for one measured reason: on this
// project's own real source (58 files across two languages), a full
// index run takes well under a second (see ADR-0010's self-hosting
// numbers), so a debounced full reindex delivers the actual user-facing
// value (no staleness detection, ADR-0010/docs/MVP.md's known issue) at
// negligible cost at today's scale. True per-file incremental indexing —
// content-hash re-anchoring, invalidating only the entities/edges a
// changed file's exports touch — is real, substantially harder work
// (the resolver's same-file/same-package/import tiers mean one file's
// export list changing can affect edges in OTHER files) and is a
// documented, deferred follow-up, not silently skipped: see
// docs/MVP.md's known issues for exactly this gap and the descriptor-
// scaling caveat internal/watch's own package doc already states for
// large repos.
//
// Multi-project since ADR-0019: ctxd accepts more than one <path>
// argument, one project each, and watches all of them concurrently from
// one process — the daemon-side registry ADR-0012/ADR-0016 both named as
// a distinct, harder problem from the CLI-only `ctx project` registry.
// Each <path> also accepts a name registered via `ctx project add`
// (internal/project.Resolve), same as every `ctx` CLI command already
// does.
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
	"github.com/deatherick/cartograph/internal/opstatus"
	"github.com/deatherick/cartograph/internal/project"
	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
	"github.com/deatherick/cartograph/internal/watch"
)

func main() {
	webAddr := flag.String("web", "127.0.0.1:7420", "address to serve the web UI on (127.0.0.1-only by default — see the project plan's permanent 'bind to localhost' restriction); empty disables it")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ctxd [--web addr] <path> [<path>...]")
		fmt.Fprintln(os.Stderr, "\nIndexes each <path> once, then watches it and re-indexes on every change until interrupted (Ctrl+C).")
		fmt.Fprintln(os.Stderr, "Each <path> also accepts a name registered via `ctx project add`.")
		fmt.Fprintln(os.Stderr, "Also serves a web UI (Phase 6) at --web (default 127.0.0.1:7420) unless --web=\"\", with a project switcher when more than one <path> is given.")
		os.Exit(2)
	}

	svc := service.New()
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
			watchProject(done, svc, h)
		}(h)
	}

	<-sig
	fmt.Println("ctxd: shutting down")
	close(done)
	wg.Wait()
}

// watchProject indexes root once, then watches it and re-indexes on every
// change until done is closed — one project's whole lifecycle, run in its
// own goroutine so ctxd's multiple projects (ADR-0019) watch fully
// concurrently, each with its own opstatus.Tracker (h.Ops).
func watchProject(done <-chan struct{}, svc *service.Service, h httpserver.Project) {
	label := h.Repo
	if h.Name != h.Repo {
		label = h.Name + "/" + h.Repo
	}

	reindex := func(reason string) {
		fmt.Printf("ctxd[%s]: %s — indexing %s\n", label, reason, h.Root)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats, err := svc.Index(ctx, h.Root, h.Repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctxd[%s]: index error: %v\n", label, err)
			h.Ops.RecordReindexFailure(reason, err)
			return
		}
		h.Ops.RecordReindexSuccess(reason, stats)
		fmt.Print(render.IndexStats(stats))
	}

	reindex("initial index")

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
		case <-w.Events():
			reindex("change detected")
		case werr := <-w.Errors():
			fmt.Fprintf(os.Stderr, "ctxd[%s]: watch error: %v\n", label, werr)
			h.Ops.RecordWatchError(werr)
		case <-done:
			h.Ops.SetWatching(false)
			return
		}
	}
}
