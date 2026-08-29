// Command ctxbench is the token-economy benchmark harness. It is built
// before the product itself (Phase 0b) precisely so the project has a
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
// Once the Context Compiler exists (Phase 2), this same harness gains a
// --capsule mode that additionally reports tokens_capsule, recall@gold, and
// precision@gold side by side with these baselines — a token-savings number
// is never reported without recall next to it (see
// docs/research/06-token-measurement.md).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/deatherick/cartograph/internal/compile"
	"github.com/deatherick/cartograph/internal/exclude"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/store"
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
	ID             string
	TracedTokens   int
	OracleTokens   int
	GoldFileCount  int
	EstimatorRatio float64 // char/4 estimate ÷ real tokenizer count, on gold files combined
	Err            string
}

func main() {
	baseline := flag.Bool("baseline", false, "compute and report baseline token cost (grep+read) for the task set")
	capsule := flag.Bool("capsule", false, "compute and report Context Compiler capsule cost, recall@gold, and precision@gold for the task set")
	budgetFlag := flag.Int("budget", 2500, "token budget per task for --capsule")
	tasksPath := flag.String("tasks", "fixtures/tasks/ts-basic.json", "path to the task set JSON")
	fixturesRoot := flag.String("fixtures-root", "fixtures", "root directory containing named fixtures")
	flag.Parse()

	if !*baseline && !*capsule {
		fmt.Fprintln(os.Stderr, "ctxbench: no mode selected. Use --baseline and/or --capsule.")
		os.Exit(2)
	}

	ts, err := loadTaskSet(*tasksPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "ctxbench: %v\n", err)
		os.Exit(1)
	}

	fixtureDir := filepath.Join(*fixturesRoot, ts.Fixture)

	var baselineResults []taskResult
	if *baseline {
		baselineResults = make([]taskResult, 0, len(ts.Tasks))
		for _, t := range ts.Tasks {
			baselineResults = append(baselineResults, runTask(fixtureDir, t))
		}
	}

	var capsuleResults []capsuleResult
	if *capsule {
		capsuleResults, err = runCapsuleMode(fixtureDir, ts, *budgetFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "ctxbench: --capsule: %v\n", err)
			os.Exit(1)
		}
	}

	if *baseline {
		printReport(ts, baselineResults)
	}
	if *capsule {
		if *baseline {
			fmt.Println()
		}
		printCapsuleReport(ts, capsuleResults, *budgetFlag, baselineResults)
	}
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
//
// The walk goes through internal/exclude so that a real cloned repository
// (dependency directories, lockfiles, binary data files) does not pollute
// the traced baseline with noise nobody would actually grep through — see
// internal/exclude's package doc for why this exists.
func grepFixture(root, pattern string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", fmt.Errorf("invalid pattern: %w", err)
	}

	var matches []string
	err = exclude.WalkSource(root, func(path string, content []byte) error {
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

type capsuleResult struct {
	ID         string
	Tokens     int
	RecallGold float64 // fraction of gold_files with >=1 capsule item
	Precision  float64 // fraction of capsule items whose file is in gold_files
	ItemCount  int
	Err        string
}

// runCapsuleMode freshly indexes fixtureDir (so the benchmark never
// measures against a stale snapshot from a previous run) and compiles a
// capsule for every task, computing recall@gold/precision@gold against
// the same gold_files used by --baseline. Every capsule call uses no
// session (SessionID: "") — the Context Ledger's savings are a separate,
// multi-call dimension this per-task benchmark does not measure; see
// docs/research/06-token-measurement.md's note that ledger savings need
// their own measurement.
func runCapsuleMode(fixtureDir string, ts *taskSet, budget int) ([]capsuleResult, error) {
	result, err := index.Run(context.Background(), fixtureDir, ts.Fixture)
	if err != nil {
		return nil, fmt.Errorf("indexing %s: %w", fixtureDir, err)
	}
	snapPath, err := store.SnapshotPath(fixtureDir, ts.Fixture)
	if err != nil {
		return nil, err
	}
	if err := store.Write(snapPath, ts.Fixture, result.Graph); err != nil {
		return nil, fmt.Errorf("writing snapshot: %w", err)
	}

	out := make([]capsuleResult, 0, len(ts.Tasks))
	for _, t := range ts.Tasks {
		cr := capsuleResult{ID: t.ID}
		capsule, cerr := compile.Compile(fixtureDir, ts.Fixture, t.Prompt, compile.Options{Budget: budget})
		if cerr != nil {
			cr.Err = cerr.Error()
			out = append(out, cr)
			continue
		}
		cr.Tokens = capsule.Used
		cr.ItemCount = len(capsule.Items)
		cr.RecallGold, cr.Precision = scoreAgainstGold(capsule, t.GoldFiles)
		out = append(out, cr)
	}
	return out, nil
}

// scoreAgainstGold computes recall@gold (fraction of gold_files with at
// least one capsule item from that file) and precision@gold (fraction of
// capsule items whose file is one of the gold_files). Gold sets are
// whole-file, per fixtures/tasks/*.json's own documented V0 granularity —
// see that file's "note" field — so both metrics operate at file
// resolution, not per-entity/per-line.
func scoreAgainstGold(cap *compile.Capsule, goldFiles []string) (recall, precision float64) {
	gold := map[string]bool{}
	for _, f := range goldFiles {
		gold[filepath.ToSlash(f)] = true
	}
	covered := map[string]bool{}
	inGoldCount := 0
	for _, it := range cap.Items {
		f := filepath.ToSlash(it.Entity.Anchor.File)
		if gold[f] {
			covered[f] = true
			inGoldCount++
		}
	}
	if len(gold) > 0 {
		recall = float64(len(covered)) / float64(len(gold))
	}
	if len(cap.Items) > 0 {
		precision = float64(inGoldCount) / float64(len(cap.Items))
	}
	return recall, precision
}

func printCapsuleReport(ts *taskSet, results []capsuleResult, budget int, baselineResults []taskResult) {
	fmt.Printf("# Context Compiler capsule report — fixture %q, budget %d\n\n", ts.Fixture, budget)
	fmt.Println("| Task | Capsule tokens | Items | recall@gold | precision@gold |")
	fmt.Println("|---|---:|---:|---:|---:|")

	var sumTokens, sumItems int
	var sumRecall, sumPrecision float64
	failed := 0
	scored := 0
	for _, r := range results {
		if r.Err != "" {
			fmt.Printf("| %s | — | — | — | ERROR: %s |\n", r.ID, r.Err)
			failed++
			continue
		}
		fmt.Printf("| %s | %d | %d | %.2f | %.2f |\n", r.ID, r.Tokens, r.ItemCount, r.RecallGold, r.Precision)
		sumTokens += r.Tokens
		sumItems += r.ItemCount
		sumRecall += r.RecallGold
		sumPrecision += r.Precision
		scored++
	}

	fmt.Println()
	fmt.Printf("Total capsule tokens: %d\n", sumTokens)
	if scored > 0 {
		avgRecall := sumRecall / float64(scored)
		avgPrecision := sumPrecision / float64(scored)
		fmt.Printf("Average recall@gold:    %.2f\n", avgRecall)
		fmt.Printf("Average precision@gold: %.2f\n", avgPrecision)

		if baselineResults != nil {
			var baselineTraced int
			for _, br := range baselineResults {
				baselineTraced += br.TracedTokens
			}
			if baselineTraced > 0 {
				reduction := 1 - float64(sumTokens)/float64(baselineTraced)
				fmt.Println()
				fmt.Printf("Reduction vs traced baseline: %.1f%% (capsule %d tok vs baseline %d tok)\n", reduction*100, sumTokens, baselineTraced)
				fmt.Printf("Phase 2 exit criterion: >=70%% reduction AND recall@gold >=0.85 — this run: %.1f%% reduction, %.2f recall %s\n",
					reduction*100, avgRecall, passFail(reduction >= 0.70 && avgRecall >= 0.85))
			}
		}
	}
	if failed > 0 {
		fmt.Printf("\n%d task(s) failed to compile — see ERROR rows above.\n", failed)
	}
}

func passFail(ok bool) string {
	if ok {
		return "[PASS]"
	}
	return "[FAIL]"
}

func printReport(ts *taskSet, results []taskResult) {
	fmt.Printf("# Token-economy baseline — fixture %q\n\n", ts.Fixture)
	fmt.Printf("%d tasks. Tokens counted with cl100k_base (real BPE, embedded offline — see internal/tokencount).\n", len(results))
	fmt.Println("capsule tokens: N/A — Context Compiler not implemented yet (Phase 2). This report establishes the baseline it will be measured against.")
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
	fmt.Println("These two numbers are what the Context Compiler (Phase 2) must beat, together with")
	fmt.Println("recall@gold ≥ 0.85 — a token-savings number is never reported without recall")
	fmt.Println("next to it (docs/research/06-token-measurement.md). Target: ≥70% reduction vs the")
	fmt.Println("traced baseline, at recall@gold ≥ 0.85.")
}
