// Command ctx is the CLI for the context engine. Phase 1 implements the
// static-map subcommands (index/find/related/stats); Phase 2 adds
// context/expand/session once the Context Compiler exists. See
// internal/service's package doc for the current persistence scope gap
// (every invocation re-indexes from scratch — no daemon, no store yet).
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/service"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	svc := service.New()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(ctx, svc, os.Args[2:])
	case "find":
		err = runFind(ctx, svc, os.Args[2:])
	case "related":
		err = runRelated(ctx, svc, os.Args[2:])
	case "stats":
		err = runStats(ctx, svc, os.Args[2:])
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
  ctx index <path>              index a repo and print summary stats
  ctx find <path> <name>        find every entity with this bare name
  ctx related <path> <name> [--depth N]   entities within N hops (default 2)
  ctx stats <path>              print index statistics (entities, edges, bug_rate)`)
}

// repoName derives a stable repo identity from the given path when the
// caller has no better name — the last path component.
func repoName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Base(root)
	}
	return filepath.Base(strings.TrimRight(abs, string(filepath.Separator)))
}

func runIndex(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx index <path>")
	}
	root := args[0]
	stats, err := svc.Stats(ctx, root, repoName(root))
	if err != nil {
		return err
	}
	printStats(stats)
	return nil
}

func runFind(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx find <path> <name>")
	}
	root, name := args[0], args[1]
	entities, err := svc.Find(ctx, root, repoName(root), name)
	if err != nil {
		return err
	}
	if len(entities) == 0 {
		fmt.Printf("no entity named %q found\n", name)
		return nil
	}
	for _, e := range entities {
		fmt.Printf("%-10s %-40s %s:%d-%d\n", e.Kind, e.Qualified, e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine)
	}
	return nil
}

func runRelated(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx related <path> <name> [--depth N]")
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
	related, err := svc.Related(ctx, root, repoName(root), name, depth)
	if err != nil {
		return err
	}
	if len(related) == 0 {
		fmt.Printf("no related entities found within %d hop(s) of %q\n", depth, name)
		return nil
	}
	for _, r := range related {
		fmt.Printf("[depth %d] %-10s %-40s via %s (%s, conf=%.2f)\n",
			r.Depth, r.Entity.Kind, r.Entity.Qualified, r.Via.Kind, r.Via.Provenance, r.Via.Confidence)
	}
	return nil
}

func runStats(ctx context.Context, svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx stats <path>")
	}
	root := args[0]
	stats, err := svc.Stats(ctx, root, repoName(root))
	if err != nil {
		return err
	}
	printStats(stats)
	return nil
}

func printStats(s index.Stats) {
	fmt.Printf("files:          %d\n", s.Files)
	fmt.Printf("entities:       %d\n", s.Entities)
	fmt.Printf("resolved edges: %d\n", s.ResolvedEdges)
	fmt.Printf("bug_rate:       %.1f%%\n", s.BugRate()*100)
	fmt.Printf("duration:       %s\n", s.Duration)
	fmt.Println("dispositions:")
	for _, d := range []string{"resolved", "external-known", "external-unknown", "dynamic", "ambiguous", "bug-extractor", "bug-resolver", "unimplemented", "unclassified"} {
		if n, ok := s.Dispositions[model.Disposition(d)]; ok {
			fmt.Printf("  %-18s %d\n", d, n)
		}
	}
}
