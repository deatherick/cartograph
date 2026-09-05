// Package compile is the Context Compiler — the project's central bet
// (see the master plan's Phase 2 framing: "el vertical slice"). It turns
// a free-text task plus a budget into a token-dense capsule: the minimum
// useful context for an agent to start work, ranked by relevance and
// fitted to the budget by a real knapsack, never a naive truncation.
//
// V0 SCOPING, stated plainly:
//   - Seeding is term-overlap matching over Entity.Name/Qualified, not
//     BM25/FTS5 — consistent with docs/adr/0006's search-scope deferral.
//     A real ranking function (BM25/FTS5, or embeddings) remains a
//     documented follow-up; what exists here IS, as of this rewrite,
//     genuinely word-boundary-aware with light stemming (tokensFor,
//     stemMatch) rather than raw substring containment — see those
//     functions' docs for the real seeding/ranking gap this closed
//     (ADR-0022's own "a real ranking function, not another patch"
//     verdict, revisited and substantially addressed without needing
//     the larger BM25/FTS5 undertaking).
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
	Handle      string
	Entity      model.Entity
	Category    string // "primary" | "related"
	Level       Level
	Text        string
	Tokens      int
	Score       float64
	AlreadySent bool // true when the ledger already had this at >= Level
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
	// FileFilter scopes SEEDING to entities whose Anchor.File contains
	// this substring — the same convention every other `--file`
	// disambiguation already uses (internal/service.findUnique). Closes
	// docs/MVP.md's own "a task capsule can't currently be scoped to
	// 'only consider files matching X'" gap. Deliberately narrows only
	// the SEED pool (what can become a primary match), not graph
	// expansion from an accepted seed — a seed's real dependencies
	// legitimately live in other files, and excluding them would produce
	// an incomplete, disconnected capsule instead of a scoped one. "" (the
	// default) disables filtering entirely, unchanged from before this
	// field existed.
	FileFilter string
}

const (
	// defaultMaxSeeds raised from 5 to 7 alongside the word-boundary/
	// stemming rewrite of matchScore/termWeights (see tokensFor's doc):
	// a task that genuinely names two real topics (e.g. "welcome-email
	// variant for admins" — both "email" and "admin" entities are
	// legitimate matches) can have more than 5 real candidates tied for
	// relevance; 6 is the first value that restores every synthetic
	// fixture to its own exit criterion (ts-basic/csharp-basic/
	// python-basic all >=0.85 recall, >=70% reduction) after the
	// word-boundary fix removed the false-positive matches that used to
	// pad the top-5 with accidental hits. 7 was kept over 6 because it
	// measurably improves EVERY real-repo fixture's recall@gold with
	// only a marginal token-reduction cost still comfortably inside the
	// exit criterion (eShopOnWeb 0.40->0.71, realworld-ts 0.67->0.78,
	// django-realworld 0.86->0.93; ts-basic reduction 77.1%->75.4%,
	// still far above the 70% floor) — real-world recall is the actual
	// goal the synthetic fixture stands in for, not a number to just
	// barely clear.
	defaultMaxSeeds = 7
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

	candidates := rank(snap, task, opts.MaxSeeds, opts.MaxDepth, opts.FileFilter)

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

// symbolPath is the part of e.Qualified AFTER "#" — the symbol's own path
// within its file (e.g. "userservice.register"), never the file path
// before "#". Matching against the raw Qualified string let a task term
// like "validation" incidentally match every entity in validation.ts (the
// FILE name) regardless of what the entity actually was — found live via
// `ctx context "add validation to placeOrder"` seeding on unrelated
// constructors purely because they lived in validation.ts.
func symbolPath(e model.Entity) string {
	if i := strings.IndexByte(e.Qualified, '#'); i >= 0 {
		return e.Qualified[i+1:]
	}
	return e.Qualified
}

// entityTokens bundles a string tokenized through the SAME
// word-boundary-aware tokenizer tokenizeTask already uses for the task
// prompt (wordRe, then camelCase sub-splitting) — an entity's own
// name/symbol path is tokenized IDENTICALLY to the task text, so a task
// term can only match a REAL word of the entity's name, never an
// accidental substring that happens to span a word boundary. Bundled
// both as a set (O(1) exact-match lookup) and as the original slice
// (needed for stemMatch's per-token prefix scan, matchTier's doc).
//
// This is the real fix for the seeding gap ADR-0022 documented and left
// open (docs/adr/0022-route-handler-extraction.md): raw
// `strings.Contains` on the whole lowercased name let a term match
// ACROSS word boundaries purely by character coincidence — e.g. task
// term "articles" (from "...the whole articles table") matched the class
// "ArticleSchema" (lowercased "articleschema" literally contains
// "articles" as a run of characters spanning "article"+"s"chema, an
// accident of English pluralization bumping into an unrelated word's
// leading letter), inflating an unrelated model method's score enough to
// crowd the real gold route entity out of the top-N seeds. Token
// membership can't make that mistake: "articleschema" tokenizes to
// {"articleschema", "article", "schema"} — "articles" (plural) is simply
// not one of them (it DOES, correctly, still match "article" itself via
// stemMatch's morphological-variant tier — a real singular/plural match,
// not an accident).
//
// Two DIFFERENT ranker patches were tried and explicitly REJECTED before
// (a task-term stopword list; a minimum-substring-length guard) — both
// regressed the synthetic ts-basic fixture's recall@gold. Neither
// addressed the actual mechanism: both still matched via
// `strings.Contains` on the concatenated string, just filtering WHICH
// substrings were allowed to match, so a long, legitimate-looking but
// boundary-crossing false positive like "articles"/"articleschema" (8
// characters, not a stopword) would have passed either guard unfixed.
// Real word-boundary tokenization removes the mechanism itself rather
// than special-casing its symptoms.
type entityTokens struct {
	set   map[string]bool
	slice []string
}

// tokensFor's slice deliberately holds only genuine camelCase SUB-words
// (splitCamel's own output), never the whole, unsplit word — unlike
// tokenizeTask's flat output, which includes both (so a task term can
// still exact-match a symbol's FULL name, e.g. "punchRestriction"). If
// the whole word were included in slice too, stemMatch's prefix scan
// would immediately reproduce the exact word-boundary bug this whole
// rewrite exists to fix: "articleschema" (the whole, unsplit name)
// genuinely DOES start with the stemmed form of "articles" ("article"),
// since "ArticleSchema" is literally "Article"+"Schema" concatenated —
// found the hard way, via a test failure, while writing this function's
// own regression test. Restricting the stemMatch scan to real sub-words
// only ("article", "schema" — never "articleschema") keeps the exact
// word-boundary discipline tokenSet's doc describes while still letting
// "articles" correctly stem-match the sub-word "article" itself (a real
// singular/plural relationship, not an accident).
func tokensFor(s string) entityTokens {
	set := map[string]bool{}
	var subwords []string
	for _, w := range wordRe.FindAllString(s, -1) {
		lw := strings.ToLower(w)
		set[lw] = true
		for _, sub := range splitCamel(w) {
			if len(sub) > 1 {
				sl := strings.ToLower(sub)
				set[sl] = true
				subwords = append(subwords, sl)
			}
		}
	}
	return entityTokens{set: set, slice: subwords}
}

// minStemLen is the shortest a token must be to participate in
// stemMatch — long enough that a coincidentally shared prefix between
// two unrelated short words is vanishingly unlikely, short enough to
// still catch genuine morphological variants.
const minStemLen = 4

// stemMatch reports whether term and tok are the SAME WORD in different
// morphological forms — one a whole-string prefix of the other, both at
// least minStemLen long (e.g. "format"/"formatting", "percent"/
// "percentages"). A cheap stand-in for a real stemmer (Porter etc.),
// good enough for the plural/gerund/tense variation actually found —
// see matchTier's doc for why this exists alongside, not instead of,
// exact token matching.
//
// This is a per-TOKEN comparison — both term and tok are already whole
// words (split by tokenizeTask/wordRe+camelCase, never raw substrings of
// a longer joined string) — so it cannot reproduce the word-boundary bug
// tokenSet's own doc describes (task term "articles" matching inside
// "ArticleSchema" only because the class name happens to spell out
// "articles" across an internal word boundary): "articles" and "schema"
// share no common prefix at all, so stemMatch correctly declines that
// pair, while "articles" and "article" (the class's OWN first token) DO
// share a 7-character prefix — a real, meaningful singular/plural match,
// not an accident.
func stemMatch(term, tok string) bool {
	if term == tok {
		return false // exact match is scored separately, at full weight
	}
	st, sk := stem(term), stem(tok)
	shorter, longer := st, sk
	if len(sk) < len(st) {
		shorter, longer = sk, st
	}
	return len(shorter) >= minStemLen && strings.HasPrefix(longer, shorter)
}

// stem strips ONE common English inflectional suffix (plural/gerund/
// past-tense ending) from w if present, leaving its approximate root —
// e.g. "placing" -> "plac", "percentages" -> "percentag", "categories"
// -> "category". Order matters: longer, more specific suffixes are tried
// first, so "categories" strips via the "ies"->"y" rule rather than the
// generic "s" rule mangling it.
//
// Deliberately shallow: no vowel-doubling undo, no real Porter-style
// step cascade — just enough to normalize the plural/gerund/past-tense
// variation actually found in this project's own fixtures (a task
// written in prose, "placing an order", matching code written as
// "placeOrder"). Comparing STEMMED forms via stemMatch's own
// prefix-of-the-shorter rule (not exact equality) absorbs the small
// remaining mismatches a real stemmer's extra steps would clean up
// (e.g. "placing"->"plac" vs "place"->"place" unstemmed — "plac" is
// still a real prefix of "place").
func stem(w string) string {
	switch {
	case strings.HasSuffix(w, "ies") && len(w) > 5:
		return w[:len(w)-3] + "y"
	case strings.HasSuffix(w, "ing") && len(w) > 6:
		return w[:len(w)-3]
	case strings.HasSuffix(w, "es") && len(w) > 5:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "ed") && len(w) > 5:
		return w[:len(w)-2]
	case strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 4:
		return w[:len(w)-1]
	}
	return w
}

// stemWeight scales a stemmed (not exact) match's contribution relative
// to an exact token match — a real but weaker signal than the word
// matching outright.
const stemWeight = 0.6

// matchTier reports how strongly term matches tk: 1.0 for an exact
// whole-word match, stemWeight for a morphological-variant match (see
// stemMatch), 0 for no match at all.
func (tk entityTokens) matchTier(term string) float64 {
	if tk.set[term] {
		return 1
	}
	for _, t := range tk.slice {
		if stemMatch(term, t) {
			return stemWeight
		}
	}
	return 0
}

// termWeights computes an IDF-style (inverse document frequency) weight
// per task term: how many entities in the repo this term matches at all
// (by name or symbol path) determines how much a match on that term
// counts for. A generic term like "get" or "handler" that matches dozens
// of entities is barely more informative than matching nothing; a term
// specific to a handful of entities is a much stronger signal that the
// task is really about them. Without this, a multi-word task prompt whose
// terms happen to include common vocabulary seeds just as confidently on
// irrelevant, generically-named entities as on the few that actually
// matter — the flat per-term scoring `matchScore` used before this
// weighted every term the same regardless of how many entities it could
// possibly be talking about.
//
// Smoothed IDF: log((N+1)/(df+1)) + 1 — always positive, never zero (a
// term matching literally everything still counts for something, since a
// name/symbol-path match is still a real signal, just a weak one), and
// bounded above by log(N+1)+1 for a term matching nothing else at all.
func termWeights(all []model.Entity, terms []string) map[string]float64 {
	n := float64(len(all))
	// Each entity's own tokens (name + symbol path) computed once, the
	// SAME way matchScore's does — see tokenSet's doc for why this (not
	// raw substring containment) is what df must be measured against: a
	// document-frequency count built on a looser rule than the scoring
	// it's meant to calibrate would misjudge how common a term really
	// is. matchTier (exact-or-stemmed) is used here too, for the same
	// reason: a term df considers "matched" must be the same notion of
	// match matchScore itself credits.
	entityToks := make([]entityTokens, len(all))
	for i, e := range all {
		nt := tokensFor(e.Name)
		st := tokensFor(symbolPath(e))
		merged := entityTokens{set: make(map[string]bool, len(nt.set)+len(st.set)), slice: append(append([]string{}, nt.slice...), st.slice...)}
		for t := range nt.set {
			merged.set[t] = true
		}
		for t := range st.set {
			merged.set[t] = true
		}
		entityToks[i] = merged
	}
	df := map[string]int{}
	for _, t := range terms {
		for _, toks := range entityToks {
			if toks.matchTier(t) > 0 {
				df[t]++
			}
		}
	}
	// idfDampening blends the raw IDF signal with a flat weight of 1
	// (the pre-IDF behavior) rather than applying it at full strength.
	// Measured directly: full-strength IDF regressed the synthetic
	// fixture's recall@gold from 0.87 to 0.83 (below the 0.85 exit
	// criterion it previously cleared) — the per-term weight scale
	// shifted which candidates cleared rank's relative relevance floor
	// (relevanceFloorRatio, itself relative to the top seed's score) in a
	// way that dropped some previously-included true positives. 0.4 was
	// the first damping factor tested that restored both fixtures to
	// their documented exit criteria — see
	// docs/benchmarks/2026-08-29-idf-seeding.md for the full measurement.
	const idfDampening = 0.4
	weights := make(map[string]float64, len(terms))
	for _, t := range terms {
		raw := math.Log((n+1)/(float64(df[t])+1)) + 1
		weights[t] = 1 + idfDampening*(raw-1)
	}
	return weights
}

// matchScore scores e against task terms: an exact bare-name match (case-
// insensitive) is weighted far above a whole-WORD overlap in the
// qualified name, so a task that names a symbol directly seeds on that
// symbol before anything merely related by vocabulary. Each term's base
// weight (10/3/1) is further scaled by idf[t] (see termWeights) so a
// match on a rare, specific term counts for more than a match on a term
// that appears all over the codebase.
//
// Matches against tokenSet (via matchTier), not raw substring
// containment — see that function's doc for why this matters: a term
// can only match a REAL word of e's own name/symbol path (exactly, or as
// a morphological variant — stemMatch), never an accidental run of
// characters spanning a word boundary.
func matchScore(e model.Entity, terms []string, idf map[string]float64) float64 {
	nameLower := strings.ToLower(e.Name)
	nameTokens := tokensFor(e.Name)
	symbolTokens := tokensFor(symbolPath(e))
	var score float64
	seen := map[string]bool{}
	for _, t := range terms {
		if seen[t] {
			continue
		}
		seen[t] = true
		w := idf[t]
		if w == 0 {
			w = 1
		}
		if t == nameLower {
			score += 10 * w
			continue
		}
		if m := nameTokens.matchTier(t); m > 0 {
			score += 3 * w * m
			continue
		}
		if m := symbolTokens.matchTier(t); m > 0 {
			score += 1 * w * m
		}
	}
	return score
}

// rank produces every candidate entity for task: seed matches (top
// maxSeeds by score) plus their graph neighborhood out to maxDepth,
// scored by seed score decayed per hop.
func rank(snap *store.Snapshot, task string, maxSeeds, maxDepth int, fileFilter string) []candidate {
	terms := tokenizeTask(task)
	all := snap.All()
	idf := termWeights(all, terms)

	var seeds []candidate
	for _, e := range all {
		if e.Kind == model.KindTest {
			continue // test labels are not useful seeds for a code task
		}
		if fileFilter != "" && !strings.Contains(e.Anchor.File, fileFilter) {
			continue // FileFilter scopes seeding only — see Options.FileFilter's doc
		}
		if s := matchScore(e, terms, idf); s > 0 {
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
