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
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/project"
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
	case "init":
		err = runInit(os.Args[2:])
	case "languages":
		err = runLanguages(os.Args[2:])
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
	case "impact":
		err = runImpact(svc, os.Args[2:])
	case "path":
		err = runPath(svc, os.Args[2:])
	case "project":
		err = runProject(os.Args[2:])
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
	fmt.Fprintln(os.Stderr, `ctx — local code context engine

Usage:
  ctx init <path> [--yes] [--languages a,b]   wizard: detect languages, write .cartograph.json
  ctx languages <path>           show which languages are detected/enabled for this repo
  ctx index <path>              index a repo, persist a snapshot, print run stats
  ctx context <path> "<task>" --budget N [--session ID]   compile a token-budgeted capsule
  ctx find <path> <name>        find every entity with this bare name (reads snapshot)
  ctx inspect <path> <name>     full detail on one entity: signature, fan-in, fan-out
  ctx related <path> <name> [--depth N]   entities within N hops (reads snapshot)
  ctx source <path> <name>      print the entity's source lines
  ctx stats <path>              print snapshot summary (reads snapshot)
  ctx impact <path> <name> [--depth N] [--file <substring>]   blast radius: what depends on this
  ctx impact <path> --git-diff [ref]   blast radius of every entity a git diff touched (default: HEAD)
  ctx path <path> <fromName> <toName> [--from-file <sub>] [--to-file <sub>]   shortest path between two entities
  ctx project add <name> <path>    register <path> under <name>
  ctx project list                 show every registered project
  ctx project remove <name>        unregister <name>

find/related/stats read the snapshot persisted by the last `+"`ctx index`"+` run — they
do not re-index. Run `+"`ctx index <path>`"+` first, and again after the source changes.

Every <path> argument above also accepts a name registered via `+"`ctx project add`"+` —
`+"`ctx index myapp`"+` works exactly like `+"`ctx index ~/code/myapp`"+` once registered.`)
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

// runInit is the project-setup wizard: detect which languages this repo
// uses, let the user confirm or customize the list, and persist the
// choice to .cartograph.json (internal/index.ConfigFileName) — editable
// by hand afterward, or by re-running this wizard. Every language is
// opt-in/opt-out independently (internal/index.Language), so a user who
// doesn't want a language's detection active can exclude it here (a
// lighter, faster index), and one who wants everything enabled can pass
// --languages with every known name.
func runInit(args []string) error {
	fs := struct {
		yes       bool
		languages string
		path      string
	}{path: "."}
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--yes", "-y":
			fs.yes = true
		case "--languages":
			if i+1 < len(args) {
				fs.languages = args[i+1]
				i++
			}
		default:
			positional = append(positional, args[i])
		}
	}
	if len(positional) > 0 {
		fs.path = positional[0]
	}
	root := fs.path

	if _, ok := index.LoadConfig(root); ok {
		fmt.Printf("%s already exists at %s.\n", index.ConfigFileName, root)
		if !fs.yes && isInteractiveStdin() {
			fmt.Print("Reconfigure? [y/N]: ")
			line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
			if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "y") {
				fmt.Println("Left unchanged.")
				return nil
			}
		} else if !fs.yes {
			fmt.Println("Non-interactive and no --yes given — leaving it unchanged. Pass --yes to reconfigure anyway.")
			return nil
		}
	}

	available := index.AvailableLanguages(root)

	var chosen []string
	switch {
	case fs.languages != "":
		for _, name := range strings.Split(fs.languages, ",") {
			if name = strings.TrimSpace(name); name != "" {
				chosen = append(chosen, name)
			}
		}
	case fs.yes || !isInteractiveStdin():
		for _, l := range available {
			if l.Detected {
				chosen = append(chosen, l.Name)
			}
		}
		if !fs.yes {
			fmt.Println("Non-interactive terminal — enabling every detected language without prompting (pass --yes to silence this note, or --languages to choose explicitly).")
		}
	default:
		fmt.Println("Detected languages for", root+":")
		for _, l := range available {
			mark := " "
			if l.Detected {
				mark = "x"
			}
			fmt.Printf("  [%s] %s\n", mark, l.Name)
		}
		fmt.Print("Enable all detected languages? [Y/n], or type comma-separated names to customize: ")
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		line = strings.TrimSpace(line)
		switch strings.ToLower(line) {
		case "", "y", "yes":
			for _, l := range available {
				if l.Detected {
					chosen = append(chosen, l.Name)
				}
			}
		case "n", "no":
			fmt.Println("No languages enabled — edit .cartograph.json by hand, or re-run `ctx init --languages a,b` later.")
		default:
			for _, name := range strings.Split(line, ",") {
				if name = strings.TrimSpace(name); name != "" {
					chosen = append(chosen, name)
				}
			}
		}
	}

	sort.Strings(chosen)
	if err := index.SaveConfig(root, index.Config{Languages: chosen}); err != nil {
		return err
	}
	if len(chosen) == 0 {
		fmt.Printf("Wrote %s with no languages enabled — `ctx index` will index nothing until you edit it.\n", index.ConfigFileName)
	} else {
		fmt.Printf("Wrote %s — enabled: %s\n", index.ConfigFileName, strings.Join(chosen, ", "))
	}
	fmt.Println("Edit this file by hand anytime, or re-run `ctx init` to redo this wizard.")
	return nil
}

func runLanguages(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx languages <path>")
	}
	root := project.Resolve(args[0])
	cfg, hasConfig := index.LoadConfig(root)
	available := index.AvailableLanguages(root)

	enabledSet := map[string]bool{}
	if hasConfig && len(cfg.Languages) > 0 {
		for _, n := range cfg.Languages {
			enabledSet[n] = true
		}
	} else {
		for _, l := range available {
			if l.Detected {
				enabledSet[l.Name] = true
			}
		}
	}

	if hasConfig {
		fmt.Printf("%s found at %s:\n", index.ConfigFileName, root)
	} else {
		fmt.Printf("No %s at %s — showing the zero-config default (every detected language):\n", index.ConfigFileName, root)
	}
	for _, l := range available {
		status := "disabled"
		if enabledSet[l.Name] {
			status = "enabled"
		}
		detected := "not detected"
		if l.Detected {
			detected = "detected"
		}
		fmt.Printf("  %-12s %-9s (%s)\n", l.Name, status, detected)
	}
	if !hasConfig {
		fmt.Println("\nRun `ctx init` to persist this choice, or to customize it.")
	}
	return nil
}

// isInteractiveStdin reports whether stdin looks like a real terminal, not
// a pipe/redirect — used to decide whether runInit may prompt at all. A
// coding agent or CI script invoking `ctx init` never gets stuck waiting
// on input it cannot supply; it silently gets the zero-config default
// instead (loudly logged to stdout, never truly silent).
func isInteractiveStdin() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (stat.Mode() & os.ModeCharDevice) != 0
}

func runIndex(svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: ctx index <path>")
	}
	root := project.Resolve(args[0])
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
	root, task := project.Resolve(args[0]), args[1]
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
	root, name := project.Resolve(args[0]), args[1]
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
	root, name := project.Resolve(args[0]), args[1]
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
	root, name := project.Resolve(args[0]), args[1]
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
	root, name := project.Resolve(args[0]), args[1]
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
	root := project.Resolve(args[0])
	stats, err := svc.Stats(root, service.RepoName(root))
	if err != nil {
		return err
	}
	fmt.Print(render.Stats(stats))
	return nil
}

func runImpact(svc *service.Service, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf(`usage: ctx impact <path> <name> [--depth N] [--file <substring>]
   or: ctx impact <path> --git-diff [ref]   (ref defaults to HEAD)`)
	}
	root := project.Resolve(args[0])
	depth := 0 // full transitive closure by default — see service.Impact's doc
	if v := flagValue(args, "--depth"); v != "" {
		if d, perr := strconv.Atoi(v); perr == nil {
			depth = d
		}
	}

	if hasFlag(args, "--git-diff") {
		ref := optionalFlagValue(args, "--git-diff") // "" defaults to HEAD inside the service layer
		result, err := svc.ImpactFromGitDiff(root, service.RepoName(root), ref, depth)
		if err != nil {
			return err
		}
		fmt.Print(render.GitDiffImpact(result))
		return nil
	}

	if len(args) < 2 {
		return fmt.Errorf("usage: ctx impact <path> <name> [--depth N] [--file <substring>]")
	}
	name := args[1]
	result, err := svc.Impact(root, service.RepoName(root), name, flagValue(args, "--file"), depth)
	if err != nil {
		return err
	}
	fmt.Print(render.Impact(result))
	return nil
}

func runPath(svc *service.Service, args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: ctx path <path> <fromName> <toName> [--from-file <substring>] [--to-file <substring>]")
	}
	root, fromName, toName := project.Resolve(args[0]), args[1], args[2]
	result, err := svc.Path(root, service.RepoName(root), fromName, flagValue(args, "--from-file"), toName, flagValue(args, "--to-file"))
	if err != nil {
		return err
	}
	fmt.Print(render.Path(result))
	return nil
}

// hasFlag reports whether flag is present in args at all — for boolean
// flags like --git-diff, which may or may not carry a following value
// (a ref) depending on whether the caller wants the default (HEAD).
func hasFlag(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

// optionalFlagValue is flagValue's counterpart for a flag whose value is
// itself optional (--git-diff [ref]): the following arg is only treated
// as the value if it doesn't itself look like another flag — otherwise
// `ctx impact <path> --git-diff --depth 2` would wrongly consume
// "--depth" as the git ref.
func optionalFlagValue(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			return args[i+1]
		}
	}
	return ""
}

// runProject implements `ctx project add|list|remove` — the small, global
// (not per-repo) name-to-path registry every other command's <path>
// argument resolves through first (project.Resolve). Named explicitly in
// the master plan's CLI/UX scope and tracked as a known gap in
// docs/MVP.md until now.
func runProject(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf(`usage: ctx project add <name> <path>
   or: ctx project list
   or: ctx project remove <name>`)
	}
	switch args[0] {
	case "add":
		if len(args) < 3 {
			return fmt.Errorf("usage: ctx project add <name> <path>")
		}
		if err := project.Add(args[1], args[2]); err != nil {
			return err
		}
		abs, _ := filepath.Abs(args[2])
		fmt.Printf("Registered %q -> %s\n", args[1], abs)
		return nil
	case "list":
		projects, err := project.List()
		if err != nil {
			return err
		}
		if len(projects) == 0 {
			fmt.Println("No projects registered. Register one with `ctx project add <name> <path>`.")
			return nil
		}
		for _, p := range projects {
			fmt.Printf("%-20s %s\n", p.Name, p.Path)
		}
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: ctx project remove <name>")
		}
		if err := project.Remove(args[1]); err != nil {
			return err
		}
		fmt.Printf("Removed %q (if it was registered).\n", args[1])
		return nil
	default:
		return fmt.Errorf("unknown project subcommand %q — usage: ctx project add|list|remove", args[0])
	}
}
