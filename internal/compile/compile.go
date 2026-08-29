// Package compile is the Context Compiler — the project's central bet
// (see the master plan's Phase 2 framing: "el vertical slice"). It turns
// a free-text task plus a budget into a token-dense capsule: the minimum
// useful context for an agent to start work, ranked by relevance and
// fitted to the budget by a real knapsack, never a naive truncation.
//
// V0 SCOPING, stated plainly:
//   - Seeding is term-overlap matching over Entity.Name/Qualified, not
//     BM25/FTS5 — consistent with docs/adr/0006's search-scope deferral.
//     A real ranking function is a documented follow-up once this slice
//     proves the pipeline end-to-end.
//   - Graph expansion decays by hop depth only; centrality/PageRank
//     terms are not computed yet (internal/graph's package doc already
//     defers this) and git-recency is not implemented (no git-metadata
//     extraction exists yet — Phase 4 scope).
//   - The source ladder's L1/L2 "signature" rendering reads the entity's
//     first source line rather than a reconstructed signature string,
//     since internal/parser/ts does not populate Entity.Signature yet.
//   - The budgeter assigns each entity ONE natural rung up front
//     (primary -> skeleton, related -> signature) and runs a real 0/1
//     knapsack over that fixed set — not a per-item multi-rung optimizer.
//     Documented as the next refinement, not attempted here.
package compile

import (
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/deatherick/cartograph/internal/ledger"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/srcread"
	"github.com/deatherick/cartograph/internal/store"
	"github.com/deatherick/cartograph/internal/tokencount"
)

// Level is one rung of the source ladder — how much of an entity is
// rendered. Mirrors internal/ledger.Level (ledger has no dependency on
// this package, so the two are kept as parallel small enums rather than
// one imported from the other).
type Level int

const (
	LevelName Level = iota
	LevelSignature
	LevelSkeleton
	LevelBody
)

func (l Level) String() string {
	switch l {
	case LevelName:
		return "name"
	case LevelSignature:
		return "signature"
	case LevelSkeleton:
		return "skeleton"
	case LevelBody:
		return "body"
	}
	return "unknown"
}

func toLedgerLevel(l Level) ledger.Level {
	switch l {
	case LevelName:
		return ledger.LevelName
	case LevelSignature:
		return ledger.LevelSignature
	case LevelSkeleton:
		return ledger.LevelSkeleton
	case LevelBody:
		return ledger.LevelBody
	}
	return ledger.LevelNone
}

func fromLedgerLevel(l ledger.Level) Level {
	switch l {
	case ledger.LevelSignature:
		return LevelSignature
	case ledger.LevelSkeleton:
		return LevelSkeleton
	case ledger.LevelBody, ledger.LevelFile:
		return LevelBody
	}
	return LevelName
}

// Item is one entry of a compiled Capsule.
type Item struct {
	Handle       string
	Entity       model.Entity
	Category     string // "primary" | "related"
	Level        Level
	Text         string
	Tokens       int
	Score        float64
	AlreadySent  bool // true when the ledger already had this at >= Level
}

// Capsule is the Context Compiler's output.
type Capsule struct {
	Task      string
	Budget    int
	Used      int
	SessionID string
	Items     []Item
	// Considered is how many candidates the ranker produced before
	// budgeting — reported so a caller can tell "nothing relevant found"
	// apart from "budget too small for what was found".
	Considered int
}

// Options configures one Compile call.
type Options struct {
	Budget    int
	SessionID string // "" disables the ledger — every call is stateless
	MaxSeeds  int    // top-N seed matches to expand from; 0 uses a sane default
	MaxDepth  int    // graph expansion depth from each seed; 0 uses a sane default
}

const (
	defaultMaxSeeds = 5
	defaultMaxDepth = 2
	// primaryBudgetShare reserves this fraction of the budget for primary
	// (seed) items before related items compete for the rest — the
	// pragmatic stand-in for the plan's "minimum quota per category" so a
	// budget doesn't get consumed entirely by a wide, shallow spray of
	// related entities before any primary one lands.
	primaryBudgetShare = 0.6
)

// Compile ranks entities against task, fits them to opts.Budget via a
// real knapsack, and returns the resulting capsule. root/repo identify
// the already-indexed snapshot to compile against (see internal/service —
// Compile does not index; a missing snapshot is the same "run ctx index
// first" error every other read path returns).
func Compile(root, repo, task string, opts Options) (*Capsule, error) {
	if opts.MaxSeeds <= 0 {
		opts.MaxSeeds = defaultMaxSeeds
	}
	if opts.MaxDepth <= 0 {
		opts.MaxDepth = defaultMaxDepth
	}

	snapPath, err := store.SnapshotPath(root, repo)
	if err != nil {
		return nil, err
	}
	snap, err := store.Open(snapPath)
	if err != nil {
		return nil, fmt.Errorf("no index found for %s — run `ctx index %s` first (%w)", root, root, err)
	}

	candidates := rank(snap, task, opts.MaxSeeds, opts.MaxDepth)

	var led *ledger.State
	var ledgerPath string
	if opts.SessionID != "" {
		repoDir, derr := store.RepoDir(root, repo)
		if derr != nil {
			return nil, derr
		}
		ledgerPath = ledger.Path(repoDir, opts.SessionID)
		led, err = ledger.Load(ledgerPath)
		if err != nil {
			return nil, fmt.Errorf("compile: loading session ledger: %w", err)
		}
	}

	items := renderCandidates(root, candidates, led)

	selected := budget(items, opts.Budget)

	used := 0
	for i := range selected {
		if led != nil {
			selected[i].Handle = led.HandleFor(selected[i].Entity.ID)
			led.MarkDelivered(selected[i].Entity.ID, toLedgerLevel(selected[i].Level))
		} else {
			selected[i].Handle = fmt.Sprintf("E%d", i+1)
		}
		used += selected[i].Tokens
	}

	if led != nil {
		if err := ledger.Save(ledgerPath, led); err != nil {
			return nil, fmt.Errorf("compile: saving session ledger: %w", err)
		}
	}

	return &Capsule{
		Task: task, Budget: opts.Budget, Used: used, SessionID: opts.SessionID,
		Items: selected, Considered: len(candidates),
	}, nil
}

// --- Ranking ---

type candidate struct {
	entity   model.Entity
	category string
	score    float64
}

var wordRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9]*`)

// tokenizeTask splits task into lowercase words, and also splits each
// camelCase/PascalCase identifier-shaped word into its sub-words (e.g.
// "punchRestriction" -> "punch", "restriction") so a task written in
// prose still matches identifiers written in code-casing.
func tokenizeTask(task string) []string {
	var out []string
	for _, w := range wordRe.FindAllString(task, -1) {
		out = append(out, strings.ToLower(w))
		for _, sub := range splitCamel(w) {
			if len(sub) > 1 {
				out = append(out, strings.ToLower(sub))
			}
		}
	}
	return out
}

var camelBoundary = regexp.MustCompile(`[A-Z]?[a-z0-9]+|[A-Z]+(?:[A-Z][a-z]|$)`)

func splitCamel(s string) []string {
	return camelBoundary.FindAllString(s, -1)
}

// matchScore scores e against task terms: an exact bare-name match (case-
// insensitive) is weighted far above a partial substring/word overlap in
// the qualified name, so a task that names a symbol directly seeds on
// that symbol before anything merely related by vocabulary.
func matchScore(e model.Entity, terms []string) float64 {
	nameLower := strings.ToLower(e.Name)
	// symbolPathLower is the part of Qualified AFTER "#" — the symbol's
	// own path within its file (e.g. "userservice.register"), never the
	// file path before "#". Matching against the raw Qualified string
	// let a task term like "validation" incidentally match every entity
	// in validation.ts (the FILE name) regardless of what the entity
	// actually was — found live via `ctx context "add validation to
	// placeOrder"` seeding on unrelated constructors purely because they
	// lived in validation.ts.
	symbolPathLower := strings.ToLower(e.Qualified)
	if i := strings.IndexByte(e.Qualified, '#'); i >= 0 {
		symbolPathLower = strings.ToLower(e.Qualified[i+1:])
	}
	var score float64
	seen := map[string]bool{}
	for _, t := range terms {
		if seen[t] {
			continue
		}
		seen[t] = true
		if t == nameLower {
			score += 10
			continue
		}
		if strings.Contains(nameLower, t) {
			score += 3
			continue
		}
		if strings.Contains(symbolPathLower, t) {
			score += 1
		}
	}
	return score
}

// rank produces every candidate entity for task: seed matches (top
// maxSeeds by score) plus their graph neighborhood out to maxDepth,
// scored by seed score decayed per hop.
func rank(snap *store.Snapshot, task string, maxSeeds, maxDepth int) []candidate {
	terms := tokenizeTask(task)
	all := snap.All()

	var seeds []candidate
	for _, e := range all {
		if e.Kind == model.KindTest {
			continue // test labels are not useful seeds for a code task
		}
		if s := matchScore(e, terms); s > 0 {
			seeds = append(seeds, candidate{e, "primary", s})
		}
	}
	sort.Slice(seeds, func(i, j int) bool { return seeds[i].score > seeds[j].score })
	if len(seeds) > maxSeeds {
		seeds = seeds[:maxSeeds]
	}

	visited := map[model.EntityID]bool{}
	out := make([]candidate, 0, len(seeds))
	for _, s := range seeds {
		visited[s.entity.ID] = true
		out = append(out, s)
	}

	// relevanceFloor discards related candidates too weak to be worth a
	// budget slot at any size — without this, a knapsack with room to
	// spare (a budget larger than the few genuinely relevant items need)
	// includes every BFS-reachable neighbor just because it's cheap, not
	// because it's useful. Found measuring against ctxbench: recall was
	// already at target (0.92) but precision@gold averaged 0.33 and the
	// token-reduction target was missed (44.9% of the required 70%)
	// because the capsule was full of low-value depth-2 neighbors. The
	// floor is relative to the best seed score in this task's candidate
	// set, not an absolute number, so it scales with how confident the
	// seeding itself was.
	var topSeedScore float64
	for _, s := range seeds {
		if s.score > topSeedScore {
			topSeedScore = s.score
		}
	}
	// 0.3 was reached by measuring, not guessed: 0.25 gave 64.7%
	// reduction at 0.87 recall; 0.4 (which structurally excludes almost
	// all depth-2 relations, since their max possible decay is 0.6²=0.36)
	// collapsed to depth=1-equivalent behavior — 82.9% reduction but
	// recall fell to 0.68, below the 0.85 floor. 0.3 sits in the gap
	// between those two failure modes and is the first value tested that
	// cleared both exit-criterion thresholds at once (70.7% reduction,
	// 0.87 recall — see docs/adr/0007-context-compiler-vertical-slice.md).
	const relevanceFloorRatio = 0.3

	for _, s := range seeds {
		for _, r := range snap.Related(s.entity.ID, maxDepth) {
			if visited[r.Entity.ID] {
				continue
			}
			decay := math.Pow(0.6, float64(r.Depth))
			score := s.score * decay
			if score < topSeedScore*relevanceFloorRatio {
				continue
			}
			visited[r.Entity.ID] = true
			out = append(out, candidate{r.Entity, "related", score})
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].score > out[j].score })
	return out
}

// --- Rendering ---

func renderCandidates(root string, cands []candidate, led *ledger.State) []Item {
	items := make([]Item, 0, len(cands))
	for _, c := range cands {
		level := LevelSkeleton
		if c.category == "related" {
			level = LevelSignature
		}

		alreadySent := false
		if led != nil {
			prior := fromLedgerLevel(led.DeliveredAt(c.entity.ID))
			if led.DeliveredAt(c.entity.ID) != ledger.LevelNone && prior >= level {
				alreadySent = true
				level = LevelName // a pointer costs almost nothing
			}
		}

		text := render(root, c.entity, level)
		items = append(items, Item{
			Entity: c.entity, Category: c.category, Level: level,
			Text: text, Tokens: tokencount.Count(text), Score: c.score,
			AlreadySent: alreadySent,
		})
	}
	return items
}

// render is intentionally infallible: a missing/moved source file (the
// working tree changed since `ctx index` ran — a known staleness gap, see
// docs/adr/0005-snapshot-persistence.md) degrades the rendering to the
// entity's qualified name rather than failing the whole capsule over one
// unreadable file.
func render(root string, e model.Entity, level Level) string {
	switch level {
	case LevelName:
		return e.Qualified
	case LevelSignature:
		line, err := readFirstLineSafe(root, e)
		if err != nil {
			return e.Qualified
		}
		return e.Qualified + "  " + line
	case LevelSkeleton:
		line, _ := readFirstLineSafe(root, e) // "" on error, rendered without a line rather than failing
		return e.Qualified + "  " + line
	case LevelBody:
		src, err := srcread.Lines(filepath.Join(root, filepath.FromSlash(e.Anchor.File)), e.Anchor.StartLine, e.Anchor.EndLine)
		if err != nil {
			return e.Qualified
		}
		return src
	}
	return e.Qualified
}

func readFirstLineSafe(root string, e model.Entity) (string, error) {
	line, err := srcread.FirstLine(filepath.Join(root, filepath.FromSlash(e.Anchor.File)), e.Anchor.StartLine)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// --- Budgeting ---

// budget is a real 0/1 knapsack maximizing total score under a token
// budget — "the budget doesn't cut at the end, it's a deterministic
// allocator" per the project plan. Run in two passes so no category is
// starved: primary items compete for primaryBudgetShare of the budget
// first, then related items compete for whatever remains (including any
// primary share left unspent) — the pragmatic stand-in for a true
// multi-constraint solver (see the package doc's V0 scoping note).
func budget(items []Item, total int) []Item {
	if total <= 0 {
		return nil
	}
	var primary, related []Item
	for _, it := range items {
		if it.Category == "primary" {
			primary = append(primary, it)
		} else {
			related = append(related, it)
		}
	}

	primaryBudget := int(float64(total) * primaryBudgetShare)
	chosenPrimary, spentPrimary := knapsack(primary, primaryBudget)
	remaining := total - spentPrimary
	chosenRelated, _ := knapsack(related, remaining)

	return append(chosenPrimary, chosenRelated...)
}

// knapsack is a standard 0/1 integer-weight DP knapsack: maximize sum of
// Score subject to sum of Tokens <= capacity. Items with zero or negative
// cost are impossible here (tokencount.Count is never negative), so the
// DP table is capacity+1 rows deep — fine at the token budgets this
// project targets (hundreds to low thousands).
func knapsack(items []Item, capacity int) ([]Item, int) {
	if capacity <= 0 || len(items) == 0 {
		return nil, 0
	}
	// The DP table is O(items * capacity); a caller-supplied budget can
	// be arbitrarily large even when the actual candidate items are cheap
	// (e.g. --budget 1000000 against a dozen small entities), so clamp
	// the table to the sum of item costs — the true useful capacity —
	// rather than the raw requested budget.
	totalCost := 0
	for _, it := range items {
		totalCost += it.Tokens
	}
	if capacity > totalCost {
		capacity = totalCost
	}
	if capacity <= 0 {
		return nil, 0
	}
	n := len(items)
	// dp[i][c] = best score using the first i items within capacity c.
	dp := make([][]float64, n+1)
	for i := range dp {
		dp[i] = make([]float64, capacity+1)
	}
	for i := 1; i <= n; i++ {
		cost := items[i-1].Tokens
		val := items[i-1].Score
		for c := 0; c <= capacity; c++ {
			dp[i][c] = dp[i-1][c]
			if cost <= c {
				if alt := dp[i-1][c-cost] + val; alt > dp[i][c] {
					dp[i][c] = alt
				}
			}
		}
	}
	// Backtrack to find which items were chosen.
	var chosen []Item
	c := capacity
	spent := 0
	for i := n; i > 0; i-- {
		if dp[i][c] != dp[i-1][c] {
			chosen = append(chosen, items[i-1])
			c -= items[i-1].Tokens
			spent += items[i-1].Tokens
		}
	}
	return chosen, spent
}
