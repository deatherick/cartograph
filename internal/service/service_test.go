package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// gitEnv is a stable author/committer identity so commits succeed in CI
// without relying on a global git config being present.
var gitEnv = append(os.Environ(),
	"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
	"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
)

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// impactFixtureSrc is a small call chain: c calls b calls a. Impact(a)
// should find b (direct) and c (transitive, depth 2) as its blast radius.
const impactFixtureSrc = `export function a(): void {}
export function b(): void { a(); }
export function c(): void { b(); }

describe("a coverage", () => {
  it("covers a via c", () => {
    c();
  });
});
`

func TestService_Impact_TransitiveClosureAndCoveringTests(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(impactFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	result, err := svc.Impact(root, repo, "a", "", 0)
	if err != nil {
		t.Fatalf("Impact: %v", err)
	}
	if result.Target.Name != "a" {
		t.Fatalf("got target %q, want a", result.Target.Name)
	}

	names := map[string]int{}
	for _, r := range result.Transitive {
		names[r.Entity.Name] = r.Depth
	}
	if names["b"] != 1 {
		t.Errorf("expected b as a direct caller (depth 1), got %+v", names)
	}
	if names["c"] != 2 {
		t.Errorf("expected c as a transitive caller (depth 2), got %+v", names)
	}

	if len(result.DirectCallers) != 1 || result.DirectCallers[0].Name != "b" {
		t.Errorf("expected DirectCallers=[b], got %+v", result.DirectCallers)
	}
}

func TestService_Stats_SurfacesPersistedBugRate(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(impactFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	indexStats, err := svc.Index(t.Context(), root, repo)
	if err != nil {
		t.Fatalf("Index: %v", err)
	}

	stats, err := svc.Stats(root, repo)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.Files != indexStats.Files {
		t.Errorf("Stats.Files = %d, want %d (the same count Index reported)", stats.Files, indexStats.Files)
	}
	if stats.BugRate != indexStats.BugRate() {
		t.Errorf("Stats.BugRate = %v, want %v (persisted, not recomputed differently)", stats.BugRate, indexStats.BugRate())
	}
	for d, n := range indexStats.Dispositions {
		if stats.Dispositions[d] != n {
			t.Errorf("Stats.Dispositions[%s] = %d, want %d", d, stats.Dispositions[d], n)
		}
	}
}

func TestService_Path_FindsShortestChain(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(impactFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	result, err := svc.Path(root, repo, "c", "", "a", "")
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if !result.Found {
		t.Fatal("expected a path from c to a")
	}
	if result.From.Name != "c" || result.To.Name != "a" {
		t.Fatalf("got From=%q To=%q, want From=c To=a", result.From.Name, result.To.Name)
	}
	names := make([]string, len(result.Path))
	for i, hop := range result.Path {
		names[i] = hop.Entity.Name
	}
	if len(names) != 2 || names[0] != "b" || names[1] != "a" {
		t.Errorf("expected path [b, a], got %+v", names)
	}
}

func TestService_Path_AmbiguousName_Errors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(impactFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if _, err := svc.Path(root, repo, "nonexistent", "", "a", ""); err == nil {
		t.Fatal("expected an error for an unmatched from-name")
	}
}

func TestService_ImpactFromGitDiff_MapsChangedLinesToEntities(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@test.com")
	runGit(t, root, "config", "user.name", "test")

	original := "export function a(): void {}\nexport function b(): void { a(); }\n"
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "initial")

	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	// Change only `a`'s body (uncommitted) — the diff should map to `a`,
	// and the aggregated impact should include `b` (a's caller).
	changed := "export function a(): void { console.log('changed'); }\nexport function b(): void { a(); }\n"
	if err := os.WriteFile(filepath.Join(root, "chain.ts"), []byte(changed), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := svc.ImpactFromGitDiff(root, repo, "HEAD", 0)
	if err != nil {
		t.Fatalf("ImpactFromGitDiff: %v", err)
	}
	if len(got.ChangedEntities) != 1 || got.ChangedEntities[0].Name != "a" {
		t.Fatalf("expected exactly `a` as changed, got %+v", got.ChangedEntities)
	}
	foundB := false
	for _, e := range got.ImpactedEntities {
		if e.Name == "b" {
			foundB = true
		}
	}
	if !foundB {
		t.Fatalf("expected `b` in the impacted set (it calls a), got %+v", got.ImpactedEntities)
	}
}

func TestService_ImpactFromGitDiff_NoChanges_IsEmptyNotError(t *testing.T) {
	root := t.TempDir()
	runGit(t, root, "init", "-q")
	runGit(t, root, "config", "user.email", "test@test.com")
	runGit(t, root, "config", "user.name", "test")
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function a(): void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "commit", "-q", "-m", "initial")

	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}
	got, err := svc.ImpactFromGitDiff(root, repo, "HEAD", 0)
	if err != nil {
		t.Fatalf("ImpactFromGitDiff: %v", err)
	}
	if len(got.ChangedEntities) != 0 {
		t.Fatalf("expected no changed entities with a clean working tree, got %+v", got.ChangedEntities)
	}
}

// suggestFixtureSrc gives Suggest something to rank: "greet" is an exact
// match for the query "greet", "greetAdmin" is a prefix match, and
// "farewellGreeting" only contains "greet" mid-name — three different
// tiers from one query, plus a same-named class to exercise the kind
// filter.
const suggestFixtureSrc = `export function greet(): void {}
export function greetAdmin(): void {}
export function farewellGreeting(): void {}
export class greet {}
`

func TestService_Suggest_RanksExactThenPrefixThenContains(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "greet.ts"), []byte(suggestFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := svc.Suggest(root, repo, "greet", "", 0)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("expected 4 matches across kinds, got %d: %+v", len(got), got)
	}
	// Tier 0 (exact, case-insensitive on bare name) sorts first: both the
	// function and the class named exactly "greet", ordered next by Kind
	// staying stable to file/name tiebreak (Class < Function alphabetically
	// by name is not guaranteed — assert the SET of the first two, not a
	// strict order between same-tier entries).
	firstTwo := map[string]bool{got[0].Name: true, got[1].Name: true}
	if !firstTwo["greet"] || got[0].Name != "greet" || got[1].Name != "greet" {
		t.Errorf("expected the two exact 'greet' matches first, got %+v", got[:2])
	}
	if got[2].Name != "greetAdmin" {
		t.Errorf("expected the prefix match 'greetAdmin' third, got %+v", got[2])
	}
	if got[3].Name != "farewellGreeting" {
		t.Errorf("expected the contains-only match 'farewellGreeting' last, got %+v", got[3])
	}
}

func TestService_Suggest_KindFilterAndLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "greet.ts"), []byte(suggestFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := svc.Suggest(root, repo, "greet", "Class", 0)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 1 || string(got[0].Kind) != "Class" {
		t.Fatalf("expected exactly the one Class match, got %+v", got)
	}

	limited, err := svc.Suggest(root, repo, "greet", "", 2)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(limited) != 2 {
		t.Fatalf("expected limit=2 to cap results at 2, got %d", len(limited))
	}
}

func TestService_Suggest_EmptyQuery_ReturnsNothing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "greet.ts"), []byte(suggestFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := svc.Suggest(root, repo, "   ", "", 0)
	if err != nil {
		t.Fatalf("Suggest: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected no matches for a blank query, got %+v", got)
	}
}

// TestService_Find_NoMatches_ReturnsEmptySliceNotNil guards a real crash
// found live: Find used to return a nil slice on zero matches, which
// encodes to JSON `null` — the Web UI's api.find is typed Entity[] (never
// nullable) and calls matches.length straight on the response, so a
// search with zero exact matches crashed with "Cannot read properties of
// null (reading 'length')" before the existing "No entity found" handling
// ever ran. json.Marshal is the one place this distinction actually
// matters (Go itself treats nil and empty slices interchangeably for
// len()/range), so assert on the marshaled bytes, not just len(got).
func TestService_Find_NoMatches_ReturnsEmptySliceNotNil(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.ts"), []byte("export function a(): void {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	got, err := svc.Find(root, repo, "doesNotExist")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got == nil {
		t.Fatal("expected a non-nil empty slice, got nil (would encode to JSON null)")
	}
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "[]" {
		t.Fatalf("expected Find's zero-match result to marshal to \"[]\", got %q", b)
	}
}
