// Package tokencount provides a real BPE tokenizer for measuring the token
// cost of a payload, instead of a char-count heuristic.
//
// Grafel's own token-economy benchmark (cmd/bench-tokens) estimates tokens as
// len(s)/4 — an approximation whose error against a real tokenizer is never
// measured (see docs/research/06-medicion-de-tokens.md). That is fine for
// English prose and understates/overstates code unpredictably: identifiers,
// indentation, and punctuation tokenize very differently from prose.
//
// This package wraps github.com/pkoukk/tiktoken-go's cl100k_base encoding
// (the family used by GPT-4/3.5 and, close enough for estimation purposes,
// a reasonable proxy for Claude's tokenizer — no public Claude BPE table
// exists). The BPE rank table is vendored in assets/cl100k_base.tiktoken and
// embedded at compile time via go:embed, so counting tokens never requires
// network access — consistent with the project's local-first principle
// (see docs/adr/0001-go-core.md) and unlike tiktoken-go's default loader,
// which fetches the table over HTTPS on first use.
package tokencount

import (
	_ "embed"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

//go:embed assets/cl100k_base.tiktoken
var cl100kBase []byte

// embeddedBpeLoader satisfies tiktoken.BpeLoader by parsing the embedded
// asset regardless of the blobpath tiktoken-go asks for. It is only ever
// registered for the cl100k_base encoding (see init), so the blobpath is
// always the cl100k_base URL in practice; we ignore it and never touch the
// network.
type embeddedBpeLoader struct{}

func (embeddedBpeLoader) LoadTiktokenBpe(_ string) (map[string]int, error) {
	ranks := make(map[string]int)
	for _, line := range strings.Split(string(cl100kBase), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("tokencount: malformed bpe rank line: %q", line)
		}
		token, err := base64.StdEncoding.DecodeString(parts[0])
		if err != nil {
			return nil, fmt.Errorf("tokencount: decoding bpe token: %w", err)
		}
		rank, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil {
			return nil, fmt.Errorf("tokencount: parsing bpe rank: %w", err)
		}
		ranks[string(token)] = rank
	}
	return ranks, nil
}

var (
	initOnce sync.Once
	enc      *tiktoken.Tiktoken
	initErr  error
)

func encoding() (*tiktoken.Tiktoken, error) {
	initOnce.Do(func() {
		tiktoken.SetBpeLoader(embeddedBpeLoader{})
		enc, initErr = tiktoken.GetEncoding(tiktoken.MODEL_CL100K_BASE)
	})
	return enc, initErr
}

// Count returns the number of cl100k_base tokens in s. It never touches the
// network or the filesystem beyond the embedded asset compiled into the
// binary.
//
// If the embedded BPE table fails to parse (a packaging bug, not a runtime
// condition), Count falls back to the char/4 heuristic and the caller should
// treat the result as approximate — this should never happen outside a
// broken build and is deliberately not a panic, since ctxbench must keep
// producing a number even in that case.
func Count(s string) int {
	e, err := encoding()
	if err != nil {
		return len(s) / 4
	}
	return len(e.Encode(s, nil, nil))
}

// EstimatorError reports how far the char/4 heuristic (used elsewhere in the
// codebase and by Grafel's own benchmark) diverges from the real tokenizer
// on s. Returned as a ratio: estimate/actual. 1.0 means no error. Used by
// ctxbench to publish a measured deviation instead of an unbounded one (see
// docs/research/06-medicion-de-tokens.md).
func EstimatorError(s string) float64 {
	actual := Count(s)
	if actual == 0 {
		return 1.0
	}
	estimate := len(s) / 4
	return float64(estimate) / float64(actual)
}
