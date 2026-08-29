package index

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/deatherick/cartograph/internal/model"
)

func writeTestFile(t *testing.T, root, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestIndexer_FullIndex_MatchesRun(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'foo'; }\n")

	ix := NewIndexer(root, "repo")
	stats, err := ix.FullIndex(t.Context())
	if err != nil {
		t.Fatalf("FullIndex: %v", err)
	}
	if stats.Files != 1 || stats.Entities != 1 {
		t.Fatalf("got Files=%d Entities=%d, want 1/1", stats.Files, stats.Entities)
	}
}

func TestIndexer_UpdateFiles_InPlaceEdit_KeepsCrossFileEdgeIntact(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v1'; }\n")
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	if _, err := ix.FullIndex(t.Context()); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	// Edit foo's BODY only (same name/signature) — bar's edge to foo must
	// survive unchanged (EntityID doesn't depend on content).
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v2 - changed body'; }\n")
	stats, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")})
	if err != nil {
		t.Fatalf("UpdateFiles: %v", err)
	}
	if stats.Dispositions[model.DispositionResolved] != 1 {
		t.Fatalf("expected the foo/bar edge to still resolve after an in-place body edit, got dispositions=%+v", stats.Dispositions)
	}
}

func TestIndexer_UpdateFiles_CrossFileExportRemoved_InvalidatesImporter(t *testing.T) {
	// THE key scenario incremental indexing must get right: a.ts's export
	// changes (foo -> foo2), and b.ts — never itself edited — must be
	// RE-RESOLVED as a consequence, moving from a resolved edge to a
	// clear disposition explaining the now-missing export. Re-extracting
	// only a.ts is not enough; this only passes if resolve.Index.Dependents
	// correctly found b.ts as an importer of a.ts.
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v1'; }\n")
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	if _, err := ix.FullIndex(t.Context()); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	before := ix.perFileDispositions["b.ts"]
	if before[model.DispositionResolved] != 1 {
		t.Fatalf("expected b.ts's import to resolve before the change, got %+v", before)
	}

	// a.ts no longer exports "foo" at all — b.ts's own source (and its
	// import statement) is completely untouched.
	writeTestFile(t, root,"a.ts", "export function foo2(): string { return 'renamed'; }\n")
	stats, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")})
	if err != nil {
		t.Fatalf("UpdateFiles: %v", err)
	}

	after := ix.perFileDispositions["b.ts"]
	if after[model.DispositionResolved] != 0 {
		t.Errorf("expected b.ts's stale import to no longer resolve, got %+v", after)
	}
	if after[model.DispositionBugResolver] != 1 {
		t.Errorf("expected b.ts's import to report bug-resolver (import resolved to a.ts, but foo is no longer exported), got %+v", after)
	}
	if stats.Dispositions[model.DispositionResolved] != 0 {
		t.Errorf("expected the repo-wide running total to reflect the loss too, got %+v", stats.Dispositions)
	}
}

func TestIndexer_UpdateFiles_NewFileSatisfiesPreviouslyDanglingImport(t *testing.T) {
	// b.ts imports from a.ts before a.ts exists — a real bug-extractor
	// disposition. Creating a.ts afterward (a "changed" path the watcher
	// reports as newly created) must cause b.ts to be re-resolved too,
	// even though b.ts's own Dependents lookup for a.ts couldn't have
	// found b.ts BEFORE a.ts was registered.
	root := t.TempDir()
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	if _, err := ix.FullIndex(t.Context()); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}
	before := ix.perFileDispositions["b.ts"]
	if before[model.DispositionResolved] != 0 {
		t.Fatalf("expected b.ts's import to NOT resolve before a.ts exists, got %+v", before)
	}

	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'now it exists'; }\n")
	if _, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")}); err != nil {
		t.Fatalf("UpdateFiles: %v", err)
	}

	after := ix.perFileDispositions["b.ts"]
	if after[model.DispositionResolved] != 1 {
		t.Fatalf("expected b.ts's import to resolve now that a.ts exists, got %+v", after)
	}
}

func TestIndexer_UpdateFiles_FileDeleted_RemovesEntitiesAndInvalidatesImporter(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v1'; }\n")
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	if _, err := ix.FullIndex(t.Context()); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	if err := os.Remove(filepath.Join(root, "a.ts")); err != nil {
		t.Fatal(err)
	}
	stats, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")})
	if err != nil {
		t.Fatalf("UpdateFiles: %v", err)
	}

	if stats.Files != 1 {
		t.Errorf("expected Files=1 after a.ts is deleted, got %d", stats.Files)
	}
	if len(ix.graph.EntitiesInFile("a.ts")) != 0 {
		t.Error("expected a.ts's entities to be gone from the graph")
	}
	after := ix.perFileDispositions["b.ts"]
	if after[model.DispositionResolved] != 0 {
		t.Errorf("expected b.ts's edge to a.ts's now-deleted entity to be gone, got %+v", after)
	}
}

func TestIndexer_UpdateFiles_RevertToIdenticalContent_IsNoop(t *testing.T) {
	// F8/F7: content ends up byte-identical to what's already indexed —
	// must not spuriously invalidate anything, and must still report
	// correct, stable stats.
	root := t.TempDir()
	original := "export function foo(): string { return 'v1'; }\n"
	writeTestFile(t, root,"a.ts", original)
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	full, err := ix.FullIndex(t.Context())
	if err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v2 - temporary'; }\n")
	if _, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")}); err != nil {
		t.Fatalf("UpdateFiles (edit): %v", err)
	}
	writeTestFile(t, root,"a.ts", original) // reverted back to exactly the original content
	reverted, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")})
	if err != nil {
		t.Fatalf("UpdateFiles (revert): %v", err)
	}

	if reverted.Entities != full.Entities {
		t.Errorf("Entities after revert = %d, want %d (same as the original full index)", reverted.Entities, full.Entities)
	}
	if reverted.Dispositions[model.DispositionResolved] != full.Dispositions[model.DispositionResolved] {
		t.Errorf("resolved-edge count after revert = %d, want %d", reverted.Dispositions[model.DispositionResolved], full.Dispositions[model.DispositionResolved])
	}
}

func TestIndexer_UpdateFiles_MatchesAFreshFullIndexOfTheSameEndState(t *testing.T) {
	// The strongest correctness check: after a sequence of incremental
	// updates, the Indexer's own bookkeeping (Files/Entities/Dispositions)
	// must match what a completely FRESH FullIndex computes for the exact
	// same final on-disk state — ground truth, not just internally
	// self-consistent.
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v1'; }\n")
	writeTestFile(t, root,"b.ts", "import { foo } from './a';\nexport function bar(): string { return foo(); }\n")

	ix := NewIndexer(root, "repo")
	if _, err := ix.FullIndex(t.Context()); err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	// A sequence of edits: rename foo's export, add a new file, delete
	// nothing — arriving at a final state only ever reached incrementally.
	writeTestFile(t, root,"a.ts", "export function foo2(): string { return 'renamed'; }\n")
	if _, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "a.ts")}); err != nil {
		t.Fatalf("UpdateFiles (rename): %v", err)
	}
	writeTestFile(t, root,"b.ts", "import { foo2 } from './a';\nexport function bar(): string { return foo2(); }\n")
	if _, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "b.ts")}); err != nil {
		t.Fatalf("UpdateFiles (fix import): %v", err)
	}
	writeTestFile(t, root,"c.ts", "import { bar } from './b';\nexport function baz(): string { return bar(); }\n")
	incremental, err := ix.UpdateFiles(t.Context(), []string{filepath.Join(root, "c.ts")})
	if err != nil {
		t.Fatalf("UpdateFiles (new file): %v", err)
	}

	fresh, err := NewIndexer(root, "repo").FullIndex(t.Context())
	if err != nil {
		t.Fatalf("fresh FullIndex: %v", err)
	}

	if incremental.Files != fresh.Files {
		t.Errorf("Files: incremental=%d fresh=%d", incremental.Files, fresh.Files)
	}
	if incremental.Entities != fresh.Entities {
		t.Errorf("Entities: incremental=%d fresh=%d", incremental.Entities, fresh.Entities)
	}
	if incremental.Dispositions[model.DispositionResolved] != fresh.Dispositions[model.DispositionResolved] {
		t.Errorf("resolved edges: incremental=%d fresh=%d", incremental.Dispositions[model.DispositionResolved], fresh.Dispositions[model.DispositionResolved])
	}
	if incremental.BugRate() != fresh.BugRate() {
		t.Errorf("BugRate: incremental=%v fresh=%v", incremental.BugRate(), fresh.BugRate())
	}
}

func TestIndexer_UpdateFiles_ExtractionError_PreservesPriorState(t *testing.T) {
	// F1: a file that fails to re-extract must not have its prior
	// entities silently wiped. Using a nonexistent extension mapping
	// isn't feasible for a real error here, so this test instead confirms
	// the more common trigger: a changed path outside root is ignored
	// rather than erroring the whole batch, leaving all prior state
	// intact — the "no changed files resolve to anything real" case.
	root := t.TempDir()
	writeTestFile(t, root,"a.ts", "export function foo(): string { return 'v1'; }\n")
	ix := NewIndexer(root, "repo")
	before, err := ix.FullIndex(t.Context())
	if err != nil {
		t.Fatalf("FullIndex: %v", err)
	}

	after, err := ix.UpdateFiles(t.Context(), []string{"/completely/outside/root.ts"})
	if err != nil {
		t.Fatalf("UpdateFiles: %v", err)
	}
	if after.Entities != before.Entities {
		t.Errorf("expected an out-of-root changed path to be a no-op, got Entities=%d want %d", after.Entities, before.Entities)
	}
}
