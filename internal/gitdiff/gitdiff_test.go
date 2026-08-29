package gitdiff

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const sampleDiff = `diff --git a/foo.go b/foo.go
index abc123..def456 100644
--- a/foo.go
+++ b/foo.go
@@ -10,0 +11,3 @@ func something() {
+line1
+line2
+line3
@@ -25 +28 @@ func other() {
-old line
+new line
diff --git a/bar.go b/bar.go
index 111..222 100644
--- a/bar.go
+++ b/bar.go
@@ -5,3 +5,0 @@ func removedOnly() {
-a
-b
-c
`

func TestParseChangedRanges_MultipleFilesAndHunkShapes(t *testing.T) {
	changed := ParseChangedRanges([]byte(sampleDiff))

	foo := changed["foo.go"]
	if len(foo) != 2 {
		t.Fatalf("expected 2 hunks for foo.go, got %+v", foo)
	}
	if foo[0] != (Range{Start: 11, End: 13}) {
		t.Errorf("hunk 1: got %+v, want {11 13}", foo[0])
	}
	if foo[1] != (Range{Start: 28, End: 28}) {
		t.Errorf("hunk 2 (implicit count=1): got %+v, want {28 28}", foo[1])
	}

	bar := changed["bar.go"]
	if len(bar) != 1 {
		t.Fatalf("expected 1 hunk for bar.go, got %+v", bar)
	}
	if bar[0] != (Range{Start: 5, End: 5}) {
		t.Errorf("pure-deletion hunk (newCount=0): got %+v, want a single-line anchor at 5", bar[0])
	}
}

func TestParseChangedRanges_DeletedFile_HasNoRanges(t *testing.T) {
	diff := `diff --git a/gone.go b/gone.go
deleted file mode 100644
index abc..000
--- a/gone.go
+++ /dev/null
@@ -1,3 +0,0 @@
-x
-y
-z
`
	changed := ParseChangedRanges([]byte(diff))
	if len(changed["gone.go"]) != 0 {
		t.Fatalf("expected no ranges for a deleted file, got %+v", changed)
	}
}

func TestRange_Overlaps(t *testing.T) {
	r := Range{Start: 10, End: 20}
	cases := []struct {
		start, end int
		want       bool
	}{
		{5, 9, false},   // entirely before
		{21, 30, false}, // entirely after
		{10, 20, true},  // exact match
		{15, 15, true},  // fully contained
		{1, 100, true},  // fully contains r
		{20, 25, true},  // touches at the boundary
	}
	for _, c := range cases {
		if got := r.Overlaps(c.start, c.end); got != c.want {
			t.Errorf("Overlaps(%d,%d) = %v, want %v", c.start, c.end, got, c.want)
		}
	}
}

// TestDiff_RealGitRepo runs an actual `git diff` against a real temp
// repository — not just parsing canned text — to confirm Diff+
// ParseChangedRanges work end-to-end against real git output, not an
// assumption about its format.
func TestDiff_RealGitRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@test.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@test.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "test")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc One() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc One() {}\n\nfunc Two() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := Diff(dir, "HEAD")
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	changed := ParseChangedRanges(out)
	if len(changed["a.go"]) == 0 {
		t.Fatalf("expected at least one changed range for a.go, got %+v (raw diff: %s)", changed, out)
	}
}
