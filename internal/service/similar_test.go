package service

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/similar"
)

// duplicateFixtureSrc has one real near-duplicate pair (foo/bar: same
// body, different name — internal/similar's "renamed" category, its
// easiest beyond exact) plus one unrelated function, for the service
// layer's Similar/Duplicates/Decide methods to exercise end-to-end.
const duplicateFixtureSrc = `export function foo(items: number[]): number {
  let total = 0;
  let count = 0;
  for (const item of items) {
    total += item;
    count += 1;
  }
  return total / count;
}

export function bar(items: number[]): number {
  let total = 0;
  let count = 0;
  for (const item of items) {
    total += item;
    count += 1;
  }
  return total / count;
}

export class Unrelated {
  private headers: Record<string, string> = {};
  withHeader(name: string, value: string): Unrelated {
    this.headers[name] = value;
    return this;
  }
}
`

func TestService_Duplicates_FindsRenamedPair(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dup.ts"), []byte(duplicateFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	pairs, err := svc.Duplicates(root, repo, 0)
	if err != nil {
		t.Fatalf("Duplicates: %v", err)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected exactly one duplicate pair (foo/bar), got %+v", pairs)
	}
	if pairs[0].Pair.Overall < similar.DefaultThreshold {
		t.Errorf("expected the found pair to clear the default threshold, got Overall=%v", pairs[0].Pair.Overall)
	}
	if pairs[0].A.Name == "" || pairs[0].B.Name == "" {
		t.Errorf("expected both resolved entities' names to be populated, got %+v", pairs[0])
	}
}

func TestService_Similar_ScopesToOneEntity(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dup.ts"), []byte(duplicateFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	pairs, match, err := svc.Similar(root, repo, "foo", "")
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if match.Name != "foo" {
		t.Fatalf("expected the matched entity to be foo, got %q", match.Name)
	}
	if len(pairs) != 1 {
		t.Fatalf("expected exactly one pair involving foo, got %+v", pairs)
	}
	if pairs[0].Pair.A != match.ID && pairs[0].Pair.B != match.ID {
		t.Errorf("expected the returned pair to actually involve foo's entity ID, got %+v", pairs[0])
	}
}

func TestService_Decide_RemovesPairFromLaterDuplicatesCalls(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dup.ts"), []byte(duplicateFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}

	before, err := svc.Duplicates(root, repo, 0)
	if err != nil {
		t.Fatalf("Duplicates (before): %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("expected one pair before deciding, got %+v", before)
	}

	if err := svc.Decide(root, repo, "foo", "", "bar", "", similar.DecisionIntentional); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	after, err := svc.Duplicates(root, repo, 0)
	if err != nil {
		t.Fatalf("Duplicates (after): %v", err)
	}
	if len(after) != 0 {
		t.Errorf("expected the decided pair to no longer resurface, got %+v", after)
	}
}

func TestService_Decide_UnknownName_Errors(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "dup.ts"), []byte(duplicateFixtureSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := New()
	repo := RepoName(root)
	if _, err := svc.Index(t.Context(), root, repo); err != nil {
		t.Fatalf("Index: %v", err)
	}
	if err := svc.Decide(root, repo, "doesNotExist", "", "bar", "", similar.DecisionIgnore); err == nil {
		t.Fatal("expected an error deciding on an unmatched entity name")
	}
}
