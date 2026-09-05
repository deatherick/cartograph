package compile

import (
	"context"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/store"
)

func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "ts-basic")
}

// setupSnapshot indexes the synthetic fixture into a scratch $HOME so
// each test gets an isolated snapshot/ledger directory (store.RepoDir
// reads os.UserHomeDir()) without touching the real ~/.cartograph.
func setupSnapshot(t *testing.T) string {
	t.Helper()
	t.Setenv("HOME", t.TempDir())

	root := fixtureRoot(t)
	result, err := index.Run(context.Background(), root, "ts-basic")
	if err != nil {
		t.Fatalf("index.Run: %v", err)
	}
	path, err := store.SnapshotPath(root, "ts-basic")
	if err != nil {
		t.Fatal(err)
	}
	meta := store.Meta{Files: result.Stats.Files, Dispositions: result.Stats.Dispositions}
	if err := store.Write(path, "ts-basic", result.Graph, meta); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestCompile_SeedsOnExactNameMatch(t *testing.T) {
	root := setupSnapshot(t)
	cap, err := Compile(root, "ts-basic", "fix a bug in UserService.register", Options{Budget: 2000})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	found := false
	for _, it := range cap.Items {
		if it.Entity.Name == "register" && it.Category == "primary" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected 'register' to seed as a primary item, got items: %+v", cap.Items)
	}
}

// TestCompile_FileFilterScopesSeeding closes docs/MVP.md's own "a task
// capsule can't currently be scoped to 'only consider files matching X'"
// gap — fixtures/ts-basic has a `register` method in BOTH
// controllers/userController.ts and services/userService.ts, so
// FileFilter="services" must seed only the latter.
func TestCompile_FileFilterScopesSeeding(t *testing.T) {
	root := setupSnapshot(t)
	cap, err := Compile(root, "ts-basic", "fix a bug in register", Options{Budget: 2000, FileFilter: "services"})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	for _, it := range cap.Items {
		if it.Category != "primary" {
			continue
		}
		if !strings.Contains(it.Entity.Anchor.File, "services") {
			t.Errorf("expected every primary (seed) item to come from a file matching %q, got %s (file %s)",
				"services", it.Entity.Name, it.Entity.Anchor.File)
		}
	}
	var sawServiceRegister bool
	for _, it := range cap.Items {
		if it.Entity.Name == "register" && strings.Contains(it.Entity.Anchor.File, "services") {
			sawServiceRegister = true
		}
	}
	if !sawServiceRegister {
		t.Fatalf("expected services/userService.ts's own register to still seed, got items: %+v", cap.Items)
	}
}

func TestCompile_RelatedEntitiesAppearViaGraph(t *testing.T) {
	root := setupSnapshot(t)
	cap, err := Compile(root, "ts-basic", "explain UserService.register", Options{Budget: 3000})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	foundRelated := false
	for _, it := range cap.Items {
		if it.Category == "related" {
			foundRelated = true
		}
	}
	if !foundRelated {
		t.Fatalf("expected at least one related item pulled in via graph expansion, got: %+v", cap.Items)
	}
}

func TestCompile_RespectsBudget(t *testing.T) {
	root := setupSnapshot(t)
	const budget = 200
	cap, err := Compile(root, "ts-basic", "UserService register order placeOrder", Options{Budget: budget})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if cap.Used > budget {
		t.Fatalf("capsule used %d tokens, exceeding budget %d", cap.Used, budget)
	}
	sum := 0
	for _, it := range cap.Items {
		sum += it.Tokens
	}
	if sum != cap.Used {
		t.Fatalf("Used (%d) does not match sum of item tokens (%d)", cap.Used, sum)
	}
}

func TestCompile_LargerBudgetNeverScoresWorse(t *testing.T) {
	root := setupSnapshot(t)
	small, err := Compile(root, "ts-basic", "UserService register order placeOrder describeOrder", Options{Budget: 150})
	if err != nil {
		t.Fatal(err)
	}
	large, err := Compile(root, "ts-basic", "UserService register order placeOrder describeOrder", Options{Budget: 3000})
	if err != nil {
		t.Fatal(err)
	}
	scoreOf := func(c *Capsule) float64 {
		var s float64
		for _, it := range c.Items {
			s += it.Score
		}
		return s
	}
	if scoreOf(large) < scoreOf(small) {
		t.Fatalf("a larger budget scored worse: small=%.1f large=%.1f", scoreOf(small), scoreOf(large))
	}
}

func TestCompile_LedgerAvoidsResendingWithinSession(t *testing.T) {
	root := setupSnapshot(t)
	session := "test-session"

	first, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000, SessionID: session})
	if err != nil {
		t.Fatalf("first Compile: %v", err)
	}
	if first.Used == 0 {
		t.Fatal("expected the first call to deliver something")
	}

	second, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000, SessionID: session})
	if err != nil {
		t.Fatalf("second Compile: %v", err)
	}

	if second.Used >= first.Used {
		t.Fatalf("expected the second call in the same session to cost fewer tokens (ledger dedup), got first=%d second=%d", first.Used, second.Used)
	}

	foundAlreadySent := false
	for _, it := range second.Items {
		if it.AlreadySent {
			foundAlreadySent = true
			if it.Level != LevelName {
				t.Errorf("an already-sent item should degrade to LevelName, got %v for %s", it.Level, it.Entity.Qualified)
			}
		}
	}
	if !foundAlreadySent {
		t.Fatal("expected at least one item to be marked AlreadySent on the second call")
	}
}

func TestCompile_LedgerHandlesAreStableAcrossCalls(t *testing.T) {
	root := setupSnapshot(t)
	session := "handle-session"

	first, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	handles := map[string]string{}
	for _, it := range first.Items {
		handles[string(it.Entity.ID)] = it.Handle
	}

	second, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000, SessionID: session})
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range second.Items {
		if want, ok := handles[string(it.Entity.ID)]; ok && want != it.Handle {
			t.Errorf("handle for %s changed across calls: %q then %q", it.Entity.Qualified, want, it.Handle)
		}
	}
}

func TestCompile_NoSessionIsStateless(t *testing.T) {
	root := setupSnapshot(t)
	first, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Compile(root, "ts-basic", "UserService register", Options{Budget: 2000})
	if err != nil {
		t.Fatal(err)
	}
	if first.Used != second.Used {
		t.Fatalf("stateless calls (no session) should cost the same every time: first=%d second=%d", first.Used, second.Used)
	}
}

func TestCompile_NoMatchesReturnsEmptyNotError(t *testing.T) {
	root := setupSnapshot(t)
	cap, err := Compile(root, "ts-basic", "zzzznonexistentxyzzy", Options{Budget: 2000})
	if err != nil {
		t.Fatalf("Compile should not error on no matches: %v", err)
	}
	if len(cap.Items) != 0 || cap.Considered != 0 {
		t.Fatalf("expected an empty capsule for a task matching nothing, got: %+v", cap)
	}
}

func TestTokenizeTask_SplitsCamelCase(t *testing.T) {
	terms := tokenizeTask("fix punchRestriction bug")
	want := map[string]bool{"fix": true, "punchrestriction": true, "punch": true, "restriction": true, "bug": true}
	got := map[string]bool{}
	for _, t := range terms {
		got[t] = true
	}
	for w := range want {
		if !got[w] {
			t.Errorf("expected term %q in tokenized output %v", w, terms)
		}
	}
}

func TestMatchScore_DoesNotMatchFilePathPortion(t *testing.T) {
	// Regression for a real seeding bug found via `ctx context "add
	// validation to placeOrder"`: an entity whose FILE happens to be
	// named validation.ts, but whose own symbol name has nothing to do
	// with "validation" (e.g. a coincidentally-colocated helper), was
	// scoring purely off the file path. matchScore must only consider
	// the symbol path after "#", never the file path before it.
	unrelated := model.Entity{Name: "helper", Qualified: "src/utils/validation.ts#helper"}
	terms := tokenizeTask("add validation to placeOrder")
	if score := matchScore(unrelated, terms, nil); score != 0 {
		t.Fatalf("expected zero match score for a symbol unrelated to \"validation\", coincidentally in validation.ts, got %.1f", score)
	}

	// A term that genuinely matches the SYMBOL name (not just the file
	// it happens to live in) must still score — e.g. a class actually
	// named ValidationError.
	real := model.Entity{Name: "ValidationError", Qualified: "src/utils/validation.ts#ValidationError"}
	if score := matchScore(real, terms, nil); score == 0 {
		t.Fatal("expected a real symbol-name match to score above zero")
	}
}

func TestTermWeights_RareTermWeighsMoreThanCommonTerm(t *testing.T) {
	// "handler" appears in five entities' names; "punchcard" in exactly
	// one. A task mentioning both should trust the rarer, more specific
	// term far more than the generic one.
	all := []model.Entity{
		{Name: "orderHandler"}, {Name: "userHandler"}, {Name: "authHandler"},
		{Name: "eventHandler"}, {Name: "errorHandler"},
		{Name: "punchcardValidator"},
	}
	weights := termWeights(all, []string{"handler", "punchcard"})
	if weights["punchcard"] <= weights["handler"] {
		t.Fatalf("expected the rare term to weigh more: handler=%.3f punchcard=%.3f", weights["handler"], weights["punchcard"])
	}
}

func TestMatchScore_RareTermMatchOutranksCommonTermMatch(t *testing.T) {
	// Two entities each match exactly one term from the task — but one
	// term ("handler") is generic across this repo and the other
	// ("punchcard") is specific. The specific match should score higher,
	// even though both are the same KIND of match (exact bare-name).
	all := []model.Entity{
		{Name: "orderHandler"}, {Name: "userHandler"}, {Name: "authHandler"},
		{Name: "eventHandler"}, {Name: "errorHandler"},
		{Name: "punchcardValidator"},
	}
	terms := []string{"handler", "punchcard"}
	idf := termWeights(all, terms)

	genericMatch := model.Entity{Name: "handler", Qualified: "a.ts#handler"}
	specificMatch := model.Entity{Name: "punchcard", Qualified: "b.ts#punchcard"}

	if matchScore(specificMatch, terms, idf) <= matchScore(genericMatch, terms, idf) {
		t.Fatalf("expected the specific term's match to outscore the generic term's match")
	}
}

// TestMatchScore_DoesNotMatchAcrossWordBoundary is the real-fix
// regression for the seeding gap ADR-0022 documented and left open: raw
// substring containment let a task term match ACROSS a word boundary
// purely by character coincidence, at FULL weight — "articles" (from a
// task's own prose) matched "ArticleSchema" because its lowercased,
// UNSPLIT form ("articleschema") literally contains "articles" as a run
// of characters spanning "article"+"s"chema, an accident of English
// pluralization bumping into an unrelated word's leading letter. Once
// tokenized, "ArticleSchema" splits into "article"+"schema" — "articles"
// no longer matches the WHOLE unsplit name at all (an entity genuinely
// named "articles" would still match it, at full weight, exactly as
// intended); it only reaches the entity through the real singular
// "article" sub-word, at the weaker STEMMED tier, a real but reduced
// signal rather than the inflated full-weight match the original bug
// produced.
func TestMatchScore_DoesNotMatchAcrossWordBoundary(t *testing.T) {
	terms := tokenizeTask("dump the whole articles table")

	viaWordBoundaryAccident := model.Entity{Name: "ArticleSchema", Qualified: "src/models#ArticleSchema"}
	viaRealWholeNameMatch := model.Entity{Name: "articles", Qualified: "src/models#articles"}
	idf := map[string]float64{"articles": 1, "dump": 1, "whole": 1, "table": 1}

	if matchScore(viaWordBoundaryAccident, terms, idf) >= matchScore(viaRealWholeNameMatch, terms, idf) {
		t.Fatalf("expected the word-boundary accident to score BELOW a real whole-name match: accident=%.2f real=%.2f",
			matchScore(viaWordBoundaryAccident, terms, idf), matchScore(viaRealWholeNameMatch, terms, idf))
	}

	// The stemmed tier is real, not zero — "article" (the entity's own
	// singular sub-word) is a genuine morphological relationship, not an
	// accident, and must still contribute something.
	if score := matchScore(viaWordBoundaryAccident, terms, idf); score == 0 {
		t.Fatal("expected the real singular/plural sub-word relationship (\"articles\"~\"article\") to still score via stemMatch")
	}
}

// TestMatchScore_StemMatchesMorphologicalVariants verifies the
// gerund/plural stemming tier catches the exact real-world cases found
// investigating this gap: a task written in prose ("placing", a gerund;
// "percentages", a plural) matching code written in its base form
// ("place", "percent").
func TestMatchScore_StemMatchesMorphologicalVariants(t *testing.T) {
	placeOrder := model.Entity{Name: "placeOrder", Qualified: "src/services#OrderService.placeOrder"}
	terms := tokenizeTask("Add a test asserting that placing an order is rejected.")
	if score := matchScore(placeOrder, terms, nil); score == 0 {
		t.Fatal("expected \"placing\" to stem-match \"placeOrder\"'s own \"place\" token")
	}

	formatPercent := model.Entity{Name: "formatPercent", Qualified: "src/utils#formatPercent"}
	terms2 := tokenizeTask("adding a new one for percentages")
	if score := matchScore(formatPercent, terms2, nil); score == 0 {
		t.Fatal("expected \"percentages\" to stem-match \"formatPercent\"'s own \"percent\" token")
	}
}

// TestMatchScore_ExactMatchOutranksStemMatch verifies stemWeight keeps a
// stemmed (morphological-variant) match strictly weaker than an exact
// whole-word match — a real but weaker signal, never equally trusted.
func TestMatchScore_ExactMatchOutranksStemMatch(t *testing.T) {
	terms := []string{"orders"}
	idf := map[string]float64{"orders": 1}
	exact := model.Entity{Name: "ordersForUser", Qualified: "src/services#OrderService.ordersForUser"}
	stemmed := model.Entity{Name: "orderTotalCents", Qualified: "src/models#orderTotalCents"}
	if matchScore(exact, terms, idf) <= matchScore(stemmed, terms, idf) {
		t.Fatalf("expected an exact token match to outscore a stemmed one: exact=%.2f stemmed=%.2f",
			matchScore(exact, terms, idf), matchScore(stemmed, terms, idf))
	}
}
