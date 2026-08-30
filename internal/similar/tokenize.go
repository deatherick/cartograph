package similar

import (
	"hash/fnv"
	"unicode"
)

// shingleSize is how many consecutive normalized tokens form one shingle —
// 5 is a common choice in source-code near-duplicate detection literature:
// long enough that a shingle is a meaningfully specific fragment (not just
// "a comma and a paren"), short enough that two functions differing by one
// inserted statement still share most of their shingles.
const shingleSize = 5

// tokenize splits src into a normalized token stream for shingling: line
// (//) and block (/* */) comments stripped first (both TS and Go share
// this syntax — a generic normalization, not a per-language branch), then
// split into identifier runs, numeric literals (collapsed to the single
// placeholder "NUM" so two functions differing only in a constant still
// shingle identically), string/template literals (collapsed to "STR", for
// the same reason), and individual punctuation/operator characters.
// Whitespace is a separator only, never its own token.
//
// Deliberately simple, not a real per-language lexer, and NOT identifier-
// aware: two structurally identical functions with differently-named
// local variables ("renamed" clones, in the master plan's own evaluation
// taxonomy) tokenize as genuinely different token streams here — a real,
// documented V0 gap (see the package doc), not a silently accepted one.
// This also does not know about string literals containing characters
// that look like comment delimiters (e.g. a URL "http://…") — comment
// stripping runs on the raw text before tokenizing, so such a case is
// mis-stripped. Acceptable for a similarity HEURISTIC (worst case: two
// functions score slightly less similar than they should), not attempted
// to fix with a real lexer given the effort/value tradeoff.
func tokenize(src string) []string {
	runes := []rune(stripComments(src))
	var tokens []string
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case unicode.IsLetter(r) || r == '_':
			j := i
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j]) || runes[j] == '_') {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		case unicode.IsDigit(r):
			j := i
			for j < len(runes) && (unicode.IsDigit(runes[j]) || runes[j] == '.') {
				j++
			}
			tokens = append(tokens, "NUM")
			i = j
		case r == '"' || r == '\'' || r == '`':
			i = skipStringLiteral(runes, i, r)
			tokens = append(tokens, "STR")
		default:
			tokens = append(tokens, string(r))
			i++
		}
	}
	return tokens
}

// skipStringLiteral returns the index just past the string/template
// literal starting at i (runes[i] == quote), honoring backslash escapes.
// An unterminated literal (malformed/truncated source) simply consumes to
// the end — never an error, since this is a best-effort heuristic input,
// not a real parser.
func skipStringLiteral(runes []rune, i int, quote rune) int {
	j := i + 1
	for j < len(runes) {
		if runes[j] == '\\' && j+1 < len(runes) {
			j += 2
			continue
		}
		if runes[j] == quote {
			return j + 1
		}
		j++
	}
	return j
}

// stripComments removes // line comments and /* */ block comments from
// src — see tokenize's doc for the known "quote-blind" limitation.
func stripComments(src string) string {
	runes := []rune(src)
	out := make([]rune, 0, len(runes))
	i := 0
	for i < len(runes) {
		if runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			continue
		}
		if runes[i] == '/' && i+1 < len(runes) && runes[i+1] == '*' {
			i += 2
			for i+1 < len(runes) && (runes[i] != '*' || runes[i+1] != '/') {
				i++
			}
			i += 2
			continue
		}
		out = append(out, runes[i])
		i++
	}
	return string(out)
}

// shingleHashes builds the set of hashed k-shingles (contiguous token
// windows of shingleSize) for tokens — the input to MinHash signature
// computation (minhash.go). A token stream shorter than shingleSize
// becomes exactly one shingle covering everything it has, so very short
// (but not filtered-out-as-trivial) entities still get a real fingerprint
// rather than an empty one.
func shingleHashes(tokens []string) map[uint64]struct{} {
	set := map[uint64]struct{}{}
	if len(tokens) == 0 {
		return set
	}
	if len(tokens) < shingleSize {
		set[hashTokens(tokens)] = struct{}{}
		return set
	}
	for i := 0; i+shingleSize <= len(tokens); i++ {
		set[hashTokens(tokens[i:i+shingleSize])] = struct{}{}
	}
	return set
}

func hashTokens(tokens []string) uint64 {
	h := fnv.New64a()
	for _, t := range tokens {
		_, _ = h.Write([]byte(t))
		_, _ = h.Write([]byte{0}) // separator, so ["ab","c"] and ["a","bc"] never collide
	}
	return h.Sum64()
}
