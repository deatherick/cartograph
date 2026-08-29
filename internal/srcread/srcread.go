// Package srcread reads a line range from a source file on disk. Shared
// by internal/service (ctx source) and internal/compile (the source
// ladder's body/skeleton rungs) so the two don't duplicate the same
// bufio.Scanner loop — the project's own "never duplicate logic between
// consumers" rule, applied one layer below the CLI/service boundary it
// usually shows up at.
package srcread

import (
	"bufio"
	"os"
	"strings"
)

// Lines returns the 1-indexed [startLine, endLine] span of path, both
// inclusive.
func Lines(path string, startLine, endLine int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()

	var out strings.Builder
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if line < startLine {
			continue
		}
		if line > endLine {
			break
		}
		out.WriteString(scanner.Text())
		out.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return out.String(), nil
}

// FirstLine returns just line startLine — the cheapest possible
// "signature-like" rendering of an entity without a dedicated Signature
// field (internal/parser/ts does not populate one; see
// internal/compile's package doc for why this is today's V0 mechanism
// rather than a proper reconstructed signature).
func FirstLine(path string, startLine int) (string, error) {
	return Lines(path, startLine, startLine)
}
