package index

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fixtureRoot resolves fixtures/ts-basic relative to this test file, so the
// test works regardless of the working directory `go test` is invoked from.
func fixtureRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "fixtures", "ts-basic")
}

func TestRun_SyntheticFixture(t *testing.T) {
	root := fixtureRoot(t)
	result, err := Run(context.Background(), root, "ts-basic")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	if result.Stats.Files == 0 {
		t.Fatal("expected at least one file to be indexed")
	}
	if result.Stats.Entities == 0 {
		t.Fatal("expected at least one entity to be extracted")
	}
	if result.Stats.ResolvedEdges == 0 {
		t.Fatal("expected at least one resolved edge (same-file/import-table calls exist in this fixture)")
	}

	// Phase 1 exit criterion (see the project plan): bug_rate <= 15% for
	// TypeScript. Report it even on failure so a regression is legible,
	// not just "false".
	if rate := result.Stats.BugRate(); rate > 0.15 {
		t.Errorf("bug_rate %.1f%% exceeds the Phase 1 exit criterion of 15%% — dispositions: %+v", rate*100, result.Stats.Dispositions)
	}

	t.Logf("files=%d entities=%d resolved_edges=%d bug_rate=%.1f%% dispositions=%+v duration=%s",
		result.Stats.Files, result.Stats.Entities, result.Stats.ResolvedEdges,
		result.Stats.BugRate()*100, result.Stats.Dispositions, result.Stats.Duration)
}

// TestRun_RealWorldRepo runs the same pipeline against the external
// reference repo cloned for docs/benchmarks (see docs/benchmarks/README.md)
// — never vendored into this repo, so the test skips cleanly when it
// isn't present rather than failing CI.
func TestRun_RealWorldRepo(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot resolve home directory")
	}
	root := filepath.Join(home, "code", "_ref", "realworld-ts")
	if _, statErr := os.Stat(root); statErr != nil {
		t.Skipf("real-world reference repo not present at %s (see docs/benchmarks/README.md to clone it)", root)
	}

	result, err := Run(context.Background(), root, "realworld-ts")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}

	t.Logf("files=%d entities=%d resolved_edges=%d bug_rate=%.1f%% dispositions=%+v duration=%s",
		result.Stats.Files, result.Stats.Entities, result.Stats.ResolvedEdges,
		result.Stats.BugRate()*100, result.Stats.Dispositions, result.Stats.Duration)
}
