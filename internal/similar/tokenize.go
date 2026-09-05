package similar

import (
	"fmt"
	"hash/fnv"
	"unicode"
	"unicode/utf8"
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

// structuralKeywords is the shared set of keyword-like tokens kept
// LITERAL by normalizeIdentifiers — a union across every language this
// project extracts (TS/JS, Go, C#, Python), not per-language, since
// tokenize.go has always been a generic, not-per-language lexer (see its
// own doc). A keyword's presence is real structural signal ("this is a
// loop", "this is a conditional") that identifier normalization must NOT
// erase; only a name a human CHOSE (a variable, a called function) is
// fair game for normalization. Deliberately a starter list, not
// exhaustive — grows if a real fixture's measured recall/precision shows
// a gap, the same discipline every other tuned list in this project
// follows (docs/adr/0021's own "first value that clears the bar" note).
var structuralKeywords = map[string]bool{
	// Control flow (shared across all four languages, sometimes spelled
	// differently — both spellings included where they differ).
	"if": true, "else": true, "elif": true, "for": true, "while": true,
	"do": true, "switch": true, "case": true, "default": true,
	"break": true, "continue": true, "return": true, "yield": true,
	"try": true, "catch": true, "except": true, "finally": true,
	"throw": true, "raise": true, "goto": true, "pass": true,
	// Declarations.
	"function": true, "def": true, "class": true, "struct": true,
	"interface": true, "enum": true, "record": true, "delegate": true,
	"const": true, "let": true, "var": true, "func": true,
	"type": true, "namespace": true, "package": true, "module": true,
	// Modifiers/visibility.
	"public": true, "private": true, "protected": true, "internal": true,
	"static": true, "readonly": true, "final": true, "abstract": true,
	"virtual": true, "override": true, "sealed": true, "async": true,
	"await": true, "lambda": true,
	// Imports.
	"import": true, "from": true, "using": true, "require": true,
	"export": true, "as": true,
	// Values/operators-as-words.
	"true": true, "false": true, "null": true, "nil": true, "none": true,
	"new": true, "this": true, "self": true, "super": true, "base": true,
	"in": true, "is": true, "not": true, "and": true, "or": true,
	"void": true, "extends": true, "implements": true,
}

// normalizeIdentifiers rewrites tokens for structural (shingle-based)
// comparison ONLY — never for the raw token stream tokenize() itself
// returns, which stays a faithful lexing (see tokenize's own tests). Every
// identifier-looking token that ISN'T a shared structural keyword
// (structuralKeywords) is replaced with a placeholder ("ID1", "ID2", ...)
// numbered by first appearance WITHIN this one token stream (one function
// body, scoped per call — Find() tokenizes and normalizes one entity at a
// time) — the same identifier reused later in the same function keeps the
// same placeholder, so a real reuse PATTERN (e.g. "the loop variable
// feeds the accumulator") still shows up as a shingle match, while the
// actual chosen name ("total" vs "weight") no longer prevents one. This is
// the "blind renaming" technique real clone-detection tools (NiCad,
// SourcererCC) use — deliberately not scoped to only DECLARED names (vs.
// names merely referenced/called), since a heuristic tokenizer has no real
// declaration/reference distinction to draw on; ADR-0021 names this
// exact scope reduction (#2) as the one this closes.
func normalizeIdentifiers(tokens []string) []string {
	out := make([]string, len(tokens))
	ids := map[string]string{}
	for i, tok := range tokens {
		if !looksLikeIdentifier(tok) || structuralKeywords[tok] {
			out[i] = tok
			continue
		}
		placeholder, ok := ids[tok]
		if !ok {
			placeholder = fmt.Sprintf("ID%d", len(ids)+1)
			ids[tok] = placeholder
		}
		out[i] = placeholder
	}
	return out
}

// looksLikeIdentifier reports whether tok is an identifier-shaped token
// tokenize() could have produced — i.e. not NUM/STR (tokenize's own
// literal placeholders) and not a single-character punctuation/operator
// token (tokenize emits those one rune at a time). A real identifier
// literally named "NUM" or "STR" is an acknowledged, negligible edge case
// (would be kept literal instead of normalized) — not worth a more
// elaborate tagging scheme in tokenize() itself for this heuristic.
func looksLikeIdentifier(tok string) bool {
	if tok == "NUM" || tok == "STR" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(tok)
	return unicode.IsLetter(r) || r == '_'
}

// shingleHashes builds the set of hashed k-shingles (contiguous token
// windows of shingleSize) for tokens — the input to MinHash signature
// computation (minhash.go). A token stream shorter than shingleSize
// becomes exactly one shingle covering everything it has, so very short
// (but not filtered-out-as-trivial) entities still get a real fingerprint
// rather than an empty one. Callers pass normalizeIdentifiers(tokenize(src))
// for structural comparison, not tokenize's raw output — see that
// function's doc.
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
