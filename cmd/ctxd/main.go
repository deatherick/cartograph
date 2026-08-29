// Command ctxd is the context engine daemon — Phase 3d's vertical slice:
// index once, then watch the project's source (internal/watch) and
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
// No system-level installation (launchd/systemd), no multi-project
// registry, no daemon socket/RPC yet — this runs in the foreground for
// ONE project path, until interrupted. Those are Phase 9 scope, captured
// in docs/requirements/phase9-global-install-and-daemon.md at the user's
// explicit request but not built.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/deatherick/cartograph/internal/httpserver"
	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
	"github.com/deatherick/cartograph/internal/watch"
)

func main() {
	webAddr := flag.String("web", "127.0.0.1:7420", "address to serve the web UI on (127.0.0.1-only by default — see the project plan's permanent 'bind to localhost' restriction); empty disables it")
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: ctxd [--web addr] <path>")
		fmt.Fprintln(os.Stderr, "\nIndexes <path> once, then watches it and re-indexes on every change until interrupted (Ctrl+C).")
		fmt.Fprintln(os.Stderr, "Also serves a web UI (Phase 6) at --web (default 127.0.0.1:7420) unless --web=\"\".")
		os.Exit(2)
	}
	root := args[0]
	svc := service.New()
	repo := service.RepoName(root)

	if *webAddr != "" {
		go func() {
			handler := httpserver.New(svc, root, repo)
			fmt.Printf("ctxd: web UI at http://%s\n", *webAddr)
			if err := http.ListenAndServe(*webAddr, handler); err != nil { //nolint:gosec // 127.0.0.1 default binding is the security boundary here, not timeouts on a local single-user dev tool
				fmt.Fprintf(os.Stderr, "ctxd: web UI server error: %v\n", err)
			}
		}()
	}

	reindex := func(reason string) {
		fmt.Printf("ctxd: %s — indexing %s\n", reason, root)
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		stats, err := svc.Index(ctx, root, repo)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctxd: index error: %v\n", err)
			return
		}
		fmt.Print(render.IndexStats(stats))
	}

	reindex("initial index")

	w, err := watch.New(root, 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxd: starting watcher: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = w.Close() }()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)

	fmt.Printf("ctxd: watching %s for changes (Ctrl+C to stop)\n", root)
	for {
		select {
		case <-w.Events():
			reindex("change detected")
		case werr := <-w.Errors():
			fmt.Fprintf(os.Stderr, "ctxd: watch error: %v\n", werr)
		case <-sig:
			fmt.Println("ctxd: shutting down")
			return
		}
	}
}
