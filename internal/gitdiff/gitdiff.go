// Package gitdiff maps a `git diff` into the NEW (working-tree/target-ref)
// side's changed line ranges per file — the one thing internal/impact's
// git-diff-driven mode needs (docs/MVP.md's Phase 4 scope: "ctx impact
// --git-diff [ref]"). Deliberately minimal: this parses unified diff text
// with a regex over hunk headers, not a full diff/patch library — the
// project has no other use for one, and hunk headers are a small, stable
// format (see docs/research for this project's general "no dependency
// until a real need exists" discipline).
package gitdiff

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// Range is an inclusive line range on the NEW side of a diff.
type Range struct {
	Start, End int
}

// hunkHeader matches a unified diff hunk header: `@@ -old[,oldCount]
// +new[,newCount] @@ optional trailing context`. Only the new side is
// captured — internal/impact maps against the CURRENT snapshot, which
// reflects the new side's line numbers, not the old side's.
var hunkHeader = regexp.MustCompile(`^@@ -\d+(?:,\d+)? \+(\d+)(?:,(\d+))? @@`)

// Diff runs `git -C root diff --unified=0 <args...>` and returns its raw
// output. Zero context lines (--unified=0) keeps hunks tight to only the
// actually-changed lines — internal/impact wants "what changed," not
// surrounding untouched code. args is passed through verbatim: empty
// means "working tree vs the index" (git's own default); callers wanting
// "working tree vs last commit" pass "HEAD"; a historical retrospective
// passes something like "HEAD~3..HEAD" as one arg.
func Diff(root string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-C", root, "diff", "--unified=0"}, args...)
	cmd := exec.Command("git", gitArgs...) //nolint:gosec // root/args come from this project's own CLI flags, not untrusted input
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("gitdiff: git diff failed: %w (stderr: %s)", err, string(exitErr.Stderr))
		}
		return nil, fmt.Errorf("gitdiff: running git diff: %w", err)
	}
	return out, nil
}

// ParseChangedRanges reads unified diff output and returns, per file, the
// changed line ranges on the new side. A pure-deletion hunk (new count
// explicitly 0 — lines removed with nothing added back) has no real "new"
// range; it is approximated as a single-line anchor at the position the
// deletion occurred, documented here rather than silently dropped: an
// entity whose body still spans that position is still worth flagging as
// impacted, even though the exact deleted content is gone from the new
// side to point at precisely.
func ParseChangedRanges(diffOutput []byte) map[string][]Range {
	changed := map[string][]Range{}
	var currentFile string

	scanner := bufio.NewScanner(bytes.NewReader(diffOutput))
	// Individual lines in a diff (long minified files, generated code)
	// can exceed bufio.Scanner's default 64KB token limit — bump it well
	// past anything this project's own real usage has hit.
	buf := make([]byte, 0, 64*1024)
	scanner.Buffer(buf, 10*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "+++ "):
			path := strings.TrimPrefix(line, "+++ ")
			path = strings.TrimPrefix(path, "b/")
			if path == "/dev/null" {
				currentFile = "" // the file was deleted entirely — no new side to map
			} else {
				currentFile = path
			}
		case strings.HasPrefix(line, "@@ "):
			if currentFile == "" {
				continue
			}
			m := hunkHeader.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			newStart, _ := strconv.Atoi(m[1])
			newCount := 1
			if m[2] != "" {
				newCount, _ = strconv.Atoi(m[2])
			}
			var r Range
			if newCount == 0 {
				r = Range{Start: newStart, End: newStart} // pure-deletion approximation, see doc
			} else {
				r = Range{Start: newStart, End: newStart + newCount - 1}
			}
			changed[currentFile] = append(changed[currentFile], r)
		}
	}
	return changed
}

// Overlaps reports whether entity line range [entStart, entEnd] overlaps
// r — the test internal/impact uses to decide whether a changed range
// touches a given entity's anchor.
func (r Range) Overlaps(entStart, entEnd int) bool {
	return entStart <= r.End && r.Start <= entEnd
}
