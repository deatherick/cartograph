package similar

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/store"
)

// buildSnapshot indexes a real TS fixture (via internal/index, the same
// pipeline the whole product uses) and opens it as a store.Snapshot —
// Find's actual input type, so these tests exercise the real extractor's
// Anchor/ContentHash output, not a hand-built fixture that might not
// match reality.
func buildSnapshot(t *testing.T, root string) *store.Snapshot {
	t.Helper()
	result, err := index.Run(t.Context(), root, "repo")
	if err != nil {
		t.Fatalf("index.Run: %v", err)
	}
	path := filepath.Join(t.TempDir(), "graph.bin")
	if err := store.Write(path, "repo", result.Graph, store.Meta{}); err != nil {
		t.Fatal(err)
	}
	snap, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return snap
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestFind_ExactDuplicate_ScoresOverallOne(t *testing.T) {
	root := t.TempDir()
	body := `export function computeDiscount(price: number, pct: number): number {
  const discount = price * (pct / 100);
  const rounded = Math.round(discount * 100) / 100;
  return price - rounded;
}
`
	writeFile(t, root, "a.ts", body)
	writeFile(t, root, "b.ts", "export "+body[len("export "):]) // identical body, different declared name below
	// Give the second one a different function name but IDENTICAL body
	// text otherwise — same Anchor.ContentHash requires identical
	// (trimmed) source text, so keep the body byte-identical and only the
	// file differs.
	writeFile(t, root, "b.ts", body)

	snap := buildSnapshot(t, root)
	pairs, err := Find(snap, root, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	found := false
	for _, p := range pairs {
		if p.Exact {
			found = true
			if p.Overall != 1.0 || p.Structural != 1.0 || p.Behavioral != 1.0 {
				t.Errorf("expected an exact pair to score 1.0 on every dimension, got %+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("expected at least one exact-duplicate pair among %d pairs found, got %+v", len(pairs), pairs)
	}
}

func TestFind_NearDuplicate_ScoresHighButNotExact(t *testing.T) {
	// A realistic copy-paste-and-rename: only the function's OWN name
	// differs; every internal variable name is untouched. This is the
	// case this V0's non-identifier-normalizing tokenizer (see
	// tokenize.go's doc) handles well — a small, localized token change
	// rather than every internal identifier changing too (the harder
	// "renamed" case the package doc already names as a documented gap).
	root := t.TempDir()
	writeFile(t, root, "a.ts", `export function computeTotal(items: number[]): number {
  let sum = 0;
  let count = 0;
  for (const item of items) {
    sum += item;
    count += 1;
  }
  return sum / count;
}
`)
	writeFile(t, root, "b.ts", `export function computeTotalV2(items: number[]): number {
  let sum = 0;
  let count = 0;
  for (const item of items) {
    sum += item;
    count += 1;
  }
  return sum / count;
}
`)

	snap := buildSnapshot(t, root)
	pairs, err := Find(snap, root, 0.3)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(pairs) == 0 {
		t.Fatal("expected the near-duplicate (renamed variables, same structure) pair to be found")
	}
	p := pairs[0]
	if p.Exact {
		t.Error("expected NOT an exact match (different identifier names change the token stream)")
	}
	if p.Structural < candidateJaccardFloor {
		t.Errorf("expected a meaningfully high structural score, got %v", p.Structural)
	}
}

func TestFind_UnrelatedFunctions_AreNotPaired(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.ts", `export function computeTotal(items: number[]): number {
  let sum = 0;
  for (const item of items) {
    sum += item;
  }
  return sum;
}
`)
	writeFile(t, root, "b.ts", `export class HttpRequestBuilder {
  private headers: Record<string, string> = {};
  withHeader(name: string, value: string): HttpRequestBuilder {
    this.headers[name] = value;
    return this;
  }
}
`)

	snap := buildSnapshot(t, root)
	pairs, err := Find(snap, root, DefaultThreshold)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, p := range pairs {
		t.Errorf("expected no pair to clear the default threshold for two unrelated snippets, got %+v", p)
	}
}

func TestFind_TrivialEntities_AreFilteredOut(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.ts", `export function getName() { return 1; }
export function getLabel() { return 1; }
`)
	snap := buildSnapshot(t, root)
	pairs, err := Find(snap, root, 0)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	for _, p := range pairs {
		t.Errorf("expected trivially short getters to be filtered by minBodyTokens, got a pair: %+v", p)
	}
}

func TestFind_UnreadableSourceFile_IsSkippedNotFatal(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.ts", `export function foo(): number { return 1 + 2 + 3 + 4 + 5 + 6 + 7; }`)
	snap := buildSnapshot(t, root)
	// Delete the source after indexing — Find must not error, just skip it.
	if err := os.Remove(filepath.Join(root, "a.ts")); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(snap, root, 0); err != nil {
		t.Fatalf("expected Find to tolerate an unreadable source file, got error: %v", err)
	}
}

func TestPair_Key_IsOrderIndependent(t *testing.T) {
	p1 := Pair{A: "aaa", B: "bbb"}
	p2 := Pair{A: "bbb", B: "aaa"}
	if p1.Key() != p2.Key() {
		t.Errorf("expected Key() to be order-independent, got %q vs %q", p1.Key(), p2.Key())
	}
}
