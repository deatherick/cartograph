// Command ctx is the CLI for the context engine. Phase 1 implements the
// static-map subcommands: `index` runs the full pipeline and persists a
// snapshot (internal/store); find/related/stats read that snapshot
// instead of re-indexing. Phase 2 adds context/expand/session once the
// Context Compiler exists.
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

	var err error
	switch os.Args[1] {
	case "index":
		err = runIndex(svc, os.Args[2:])
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
  ctx find <path> <name>        find every entity with this bare name (reads snapshot)
  ctx inspect <path> <name>     full detail on one entity: signature, fan-in, fan-out
  ctx related <path> <name> [--depth N]   entities within N hops (reads snapshot)
  ctx source <path> <name>      print the entity's source lines
  ctx stats <path>              print snapshot summary (reads snapshot)

find/related/stats read the snapshot persisted by the last `+"`ctx index`"+` run — they
do not re-index. Run `+"`ctx index <path>`"+` first, and again after the source changes.`)
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
	stats, err := svc.Index(ctx, root, repoName(root))
	if err != nil {
		return err
	}
	printIndexStats(stats)
	return nil
}

func runFind(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx find <path> <name>")
	}
	root, name := args[0], args[1]
	entities, err := svc.Find(root, repoName(root), name)
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

func runInspect(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx inspect <path> <name> [--file <substring>]")
	}
	root, name := args[0], args[1]
	insp, err := svc.Inspect(root, repoName(root), name, flagValue(args, "--file"))
	if err != nil {
		return err
	}
	e := insp.Entity
	fmt.Printf("%s %s\n", e.Kind, e.Qualified)
	if e.Signature != "" {
		fmt.Printf("  signature: %s\n", e.Signature)
	}
	fmt.Printf("  location:  %s:%d-%d\n", e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine)
	fmt.Printf("  fan-out (%d) — what it calls/extends/implements/uses:\n", len(insp.FanOut))
	for _, edge := range insp.FanOut {
		fmt.Printf("    -> %s %s (%s, conf=%.2f)\n", edge.Kind, edge.Dst, edge.Provenance, edge.Confidence)
	}
	fmt.Printf("  fan-in (%d) — who calls/extends/implements/uses it:\n", len(insp.FanIn))
	for _, edge := range insp.FanIn {
		fmt.Printf("    <- %s %s (%s, conf=%.2f)\n", edge.Kind, edge.Src, edge.Provenance, edge.Confidence)
	}
	return nil
}

func runSource(svc *service.Service, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: ctx source <path> <name> [--file <substring>]")
	}
	root, name := args[0], args[1]
	src, e, err := svc.Source(root, repoName(root), name, flagValue(args, "--file"))
	if err != nil {
		return err
	}
	fmt.Printf("# %s %s (%s:%d-%d)\n", e.Kind, e.Qualified, e.Anchor.File, e.Anchor.StartLine, e.Anchor.EndLine)
	fmt.Print(src)
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
	related, err := svc.Related(root, repoName(root), name, flagValue(args, "--file"), depth)
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

func runStats(svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx stats <path>")
	}
	root := args[0]
	stats, err := svc.Stats(root, repoName(root))
	if err != nil {
		return err
	}
	fmt.Printf("repo:     %s\n", stats.Repo)
	fmt.Printf("entities: %d\n", stats.Entities)
	return nil
}

func printIndexStats(s index.Stats) {
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
