// Command ctxbench is the token-economy benchmark harness. It is built
// before the product itself (Fase 0b) precisely so the project has a
// measuring stick from day one — see docs/adr and docs/research/06 for why
// Grafel's own bench-tokens tool (tokens only, no correctness signal) is not
// enough on its own.
//
// --baseline computes, per task in the task set, what it costs an agent to
// answer WITHOUT the Context Engine:
//
//   - "traced" cost: the grep steps authored for the task are actually run
//     against the fixture tree (not simulated/guessed), and their matched
//     output is counted, plus a full read of the task's gold files — the
//     files a real trace would converge on and read in full once grep
//     pointed at them.
//   - "oracle" cost: a conservative lower bound — reading only the gold
//     files in full, with no exploration tax at all. An agent can never do
//     better than this without the graph, even with perfect luck.
//
// Once the Context Compiler exists (Fase 2), this same harness gains a
// --capsule mode that additionally reports tokens_capsule, recall@gold, and
// precision@gold side by side with these baselines — an ahorro de tokens
// number is never reported without recall next to it (see
// docs/research/06-medicion-de-tokens.md).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/deatherick/cartograph/internal/tokencount"
)

type grepStep struct {
	Pattern string `json:"pattern"`
	Note    string `json:"note"`
}

type task struct {
	ID        string     `json:"id"`
	Prompt    string     `json:"prompt"`
	GoldFiles []string   `json:"gold_files"`
	GrepSteps []grepStep `json:"grep_steps"`
}

type taskSet struct {
	Fixture string `json:"fixture"`
	Note    string `json:"note"`
	Tasks   []task `json:"tasks"`
}

type taskResult struct {
	ID               string
	TracedTokens     int
	OracleTokens     int
	GoldFileCount    int
	EstimatorRatio   float64 // char/4 estimate ÷ real tokenizer count, on gold files combined
	Err              string
}

func main() {
	baseline := flag.Bool("baseline", false, "compute and report baseline token cost (grep+read) for the task set")
	tasksPath := flag.String("tasks", "fixtures/tasks/ts-basic.json", "path to the task set JSON")
	fixturesRoot := flag.String("fixtures-root", "fixtures", "root directory containing named fixtures")
	flag.Parse()

	if !*baseline {
		fmt.Fprintln(os.Stderr, "ctxbench: no mode selected. Use --baseline. (--capsule arrives with the Context Compiler in Fase 2.)")
		os.Exit(2)
	}

	ts, err := loadTaskSet(*tasksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxbench: %v\n", err)
		os.Exit(1)
	}

	fixtureDir := filepath.Join(*fixturesRoot, ts.Fixture)
	results := make([]taskResult, 0, len(ts.Tasks))
	for _, t := range ts.Tasks {
		r := runTask(fixtureDir, t)
		results = append(results, r)
	}

	printReport(ts, results)
}

func loadTaskSet(path string) (*taskSet, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading task set: %w", err)
	}
	var ts taskSet
	if err := json.Unmarshal(b, &ts); err != nil {
		return nil, fmt.Errorf("parsing task set: %w", err)
	}
	if len(ts.Tasks) == 0 {
		return nil, fmt.Errorf("task set %s has no tasks", path)
	}
	return &ts, nil
}

func runTask(fixtureDir string, t task) taskResult {
	r := taskResult{ID: t.ID, GoldFileCount: len(t.GoldFiles)}

	// Traced baseline: run every grep step for real against the fixture
	// tree, count the tokens of the matched output (what a human/agent
	// actually sees on screen), then add a full read of the gold files —
	// the files the trace is defined to converge on.
	tracedTokens := 0
	for _, step := range t.GrepSteps {
		out, err := grepFixture(fixtureDir, step.Pattern)
		if err != nil {
			r.Err = fmt.Sprintf("grep step %q: %v", step.Pattern, err)
			return r
		}
		tracedTokens += tokencount.Count(out)
	}

	goldTokens, goldText, err := readGoldFiles(fixtureDir, t.GoldFiles)
	if err != nil {
		r.Err = err.Error()
		return r
	}

	r.TracedTokens = tracedTokens + goldTokens
	r.OracleTokens = goldTokens
	r.EstimatorRatio = tokencount.EstimatorError(goldText)
	return r
}

// grepFixture walks the fixture tree and returns matched lines rendered as
// "relative/path.ts:N: <line content>", the same shape a human running
// ripgrep would read on screen. Matching is line-based and regex-based,
// mirroring grep -n semantics closely enough for token-cost purposes.
func grepFixture(root, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var matches []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		for i, line := range strings.Split(string(content), "\n") {
			if re.MatchString(line) {
				matches = append(matches, fmt.Sprintf("%s:%d: %s", rel, i+1, line))
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(matches)
	return strings.Join(matches, "\n"), nil
}

func readGoldFiles(fixtureDir string, goldFiles []string) (tokens int, combined string, err error) {
	var sb strings.Builder
	for _, gf := range goldFiles {
		path := filepath.Join(fixtureDir, filepath.FromSlash(gf))
		content, rerr := os.ReadFile(path)
		if rerr != nil {
			return 0, "", fmt.Errorf("reading gold file %s: %w", gf, rerr)
		}
		sb.Write(content)
		sb.WriteByte('\n')
	}
	combined = sb.String()
	return tokencount.Count(combined), combined, nil
}

func printReport(ts *taskSet, results []taskResult) {
	fmt.Printf("# Token-economy baseline — fixture %q\n\n", ts.Fixture)
	fmt.Printf("%d tasks. Tokens counted with cl100k_base (real BPE, embedded offline — see internal/tokencount).\n", len(results))
	fmt.Println("capsule tokens: N/A — Context Compiler not implemented yet (Fase 2). This report establishes the baseline it will be measured against.")
	fmt.Println()
	fmt.Println("| Task | Gold files | Oracle tokens | Traced tokens | char/4 estimator ratio |")
	fmt.Println("|---|---:|---:|---:|---:|")

	var sumOracle, sumTraced int
	failed := 0
	for _, r := range results {
		if r.Err != "" {
			fmt.Printf("| %s | — | — | — | ERROR: %s |\n", r.ID, r.Err)
			failed++
			continue
		}
		fmt.Printf("| %s | %d | %d | %d | %.2f |\n", r.ID, r.GoldFileCount, r.OracleTokens, r.TracedTokens, r.EstimatorRatio)
		sumOracle += r.OracleTokens
		sumTraced += r.TracedTokens
	}

	fmt.Println()
	fmt.Printf("Total oracle tokens (lower bound, no exploration tax): %d\n", sumOracle)
	fmt.Printf("Total traced tokens (grep exploration + gold reads):   %d\n", sumTraced)
	if failed > 0 {
		fmt.Printf("\n%d task(s) failed to run — see ERROR rows above.\n", failed)
		os.Exit(1)
	}

	fmt.Println()
	fmt.Println("These two numbers are what the Context Compiler (Fase 2) must beat, together with")
	fmt.Println("recall@gold ≥ 0.85 — an ahorro-de-tokens number is never reported without recall")
	fmt.Println("next to it (docs/research/06-medicion-de-tokens.md). Target: ≥70% reduction vs the")
	fmt.Println("traced baseline, at recall@gold ≥ 0.85.")
}
