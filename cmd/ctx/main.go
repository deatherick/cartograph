// Command ctx is the CLI for the context engine. Phase 1 implements the
// static-map subcommands: `index` runs the full pipeline and persists a
// snapshot (internal/store); find/related/stats read that snapshot
// instead of re-indexing. Phase 2 adds `context`, wired to
// internal/compile — the Context Compiler — and an MCP server
// (cmd/ctxmcp) so an agent, not just this CLI, can use the same
// service layer. All formatting lives in internal/render, shared with
// the MCP server so output is not duplicated between interfaces.
package main

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/deatherick/cartograph/internal/render"
	"github.com/deatherick/cartograph/internal/service"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	svc := service.New()

	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(svc, os.Args[2:])
	case "context":
		err = runContext(svc, os.Args[2:])
	case "find":
		err = runFind(svc, os.Args[2:])
	case "inspect":
		err = runInspect(svc, os.Args[2:])
	case "related":
		err = runRelated(svc, os.Args[2:])
	case "source":
		err = runSource(svc, os.Args[2:])
	case "stats":
		err = runStats(svc, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctx: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `ctx — local code context engine (Phase 1: static map only)

Usage:
  ctx index <path>              index a repo, persist a snapshot, print run stats
  ctx context <path> "<task>" --budget N [--session ID]   compile a token-budgeted capsule
  ctx find <path> <name>        find every entity with this bare name (reads snapshot)
  ctx inspect <path> <name>     full detail on one entity: signature, fan-in, fan-out
  ctx related <path> <name> [--depth N]   entities within N hops (reads snapshot)
  ctx source <path> <name>      print the entity's source lines
  ctx stats <path>              print snapshot summary (reads snapshot)

find/related/stats read the snapshot persisted by the last `+"`ctx index`"+` run — they
do not re-index. Run `+"`ctx index <path>`"+` first, and again after the source changes.`)
}

// flagValue returns the value following flag in args, or "" if flag is
// absent — a small shared helper so --file (and, later, other optional
// flags) don't need bespoke parsing in every subcommand.
func flagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func runIndex(svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx index <path>")
	}
	root := args[0]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	stats, err := svc.Index(ctx, root, service.RepoName(root))
	if err != nil {
		return err
	}
	fmt.Print(render.IndexStats(stats))
	return nil
}

func runContext(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf(`usage: ctx context <path> "<task>" [--budget N] [--session ID]`)
	}
	root, task := args[0], args[1]
	budget := 2500
	if v := flagValue(args, "--budget"); v != "" {
		if b, perr := strconv.Atoi(v); perr == nil {
			budget = b
		}
	}
	session := flagValue(args, "--session")

	capsule, err := svc.Context(root, service.RepoName(root), task, budget, session)
	if err != nil {
		return err
	}
	fmt.Print(render.Capsule(capsule))
	return nil
}

func runFind(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx find <path> <name>")
	}
	root, name := args[0], args[1]
	entities, err := svc.Find(root, service.RepoName(root), name)
	if err != nil {
		return err
	}
	fmt.Print(render.Entities(name, entities))
	return nil
}

func runInspect(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx inspect <path> <name> [--file <substring>]")
	}
	root, name := args[0], args[1]
	insp, err := svc.Inspect(root, service.RepoName(root), name, flagValue(args, "--file"))
	if err != nil {
		return err
	}
	fmt.Print(render.Inspection(insp))
	return nil
}

func runSource(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx source <path> <name> [--file <substring>]")
	}
	root, name := args[0], args[1]
	src, e, err := svc.Source(root, service.RepoName(root), name, flagValue(args, "--file"))
	if err != nil {
		return err
	}
	fmt.Print(render.Source(e, src))
	return nil
}

func runRelated(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx related <path> <name> [--depth N] [--file <substring>]")
	}
	root, name := args[0], args[1]
	depth := 2
	for i, a := range args {
		if a == "--depth" && i+1 < len(args) {
			if d, perr := strconv.Atoi(args[i+1]); perr == nil {
				depth = d
			}
		}
	}
	related, err := svc.Related(root, service.RepoName(root), name, flagValue(args, "--file"), depth)
	if err != nil {
		return err
	}
	fmt.Print(render.Related(name, depth, related))
	return nil
}

func runStats(svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx stats <path>")
	}
	root := args[0]
	stats, err := svc.Stats(root, service.RepoName(root))
	if err != nil {
		return err
	}
	fmt.Print(render.Stats(stats))
	return nil
}
