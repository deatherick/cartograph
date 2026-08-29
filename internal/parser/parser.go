// Package parser wraps tree-sitter with the three safeguards the discovery
// on Grafel's parser identified as load-bearing (see
// docs/research/01-parser-and-treesitter-binding.md):
//
//   - a 10% syntax-error-ratio gate: a file whose parse tree is more than
//     10% ERROR nodes is rejected rather than turned into garbage entities;
//   - a per-parse timeout, since tree-sitter can hang on pathological input
//     (minified/generated files, multi-megabyte single lines);
//   - the tree is closed on every path, including the error path — Grafel
//     leaked C heap for the life of the process by skipping this on error
//     (their issue #5963).
//
// ARCHITECTURE BOUNDARY: no type from go-tree-sitter or any
// tree-sitter-<lang> grammar module may be imported outside this directory
// tree (internal/parser/**). Grafel's binding migration touched 245 files
// and 1,758 call sites precisely because the binding type leaked past its
// wrapper (ADR-0023). See architecture_test.go, which enforces this with a
// grep over import declarations, not just documents it.
//
// Language-specific extraction (internal/parser/ts, csharp, python) lives
// inside this boundary and converts sitter.Node trees into model.FileFacts
// before returning to any caller outside the package tree.
package parser

import (
	"context"
	"fmt"
	"time"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// maxErrorRatio is the fault-tolerance threshold: files with a higher
// ERROR-node ratio are rejected. Mirrors the threshold Grafel validated in
// production (docs/research/01-parser-and-treesitter-binding.md) — this is
// not re-derived from scratch, it is adopted because it already survived
// real corpora.
const maxErrorRatio = 0.10

// defaultTimeout bounds a single parse call — tree-sitter can hang on
// pathological input (minified/generated files, multi-megabyte single
// lines). Enforced via ParseOptions.ProgressCallback (see Parse below),
// not the now-deprecated SetTimeoutMicros/ParseCtx, which go-tree-sitter
// v0.25 marks for removal in 0.26.
const defaultTimeout = 5 * time.Second

// ErrHighSyntaxErrorRate is returned when a file's parse tree exceeds
// maxErrorRatio.
var ErrHighSyntaxErrorRate = fmt.Errorf("parser: syntax error rate exceeds %.0f%%", maxErrorRatio*100)

// Language is an opaque handle to a compiled tree-sitter grammar. Language
// subpackages construct these from their grammar module's exported
// language pointer.
type Language struct {
	name string
	inner *sitter.Language
}

// NewLanguage wraps a raw tree-sitter language pointer, as returned by a
// grammar module's LanguageXxx() function (e.g.
// tree_sitter_typescript.LanguageTypescript()).
func NewLanguage(name string, ptr *sitter.Language) *Language {
	return &Language{name: name, inner: ptr}
}

func (l *Language) String() string { return l.name }

// Raw returns the underlying *sitter.Language. Exported so language
// subpackages (internal/parser/ts, csharp, python) can compile their own
// queries against it — those subpackages already live inside this
// package's architecture boundary (see the package doc), so exposing the
// sitter type to them is not a leak; it would only be a leak if a package
// outside internal/parser/** called this.
func (l *Language) Raw() *sitter.Language { return l.inner }

// Tree is the parsed result. Owns C memory; callers MUST call Close.
type Tree struct {
	inner *sitter.Tree
	// Source is retained because Node text extraction (Utf8Text) needs the
	// original bytes; tree-sitter nodes only carry byte offsets.
	Source []byte
}

// Root returns the tree's root node.
func (t *Tree) Root() *sitter.Node { return t.inner.RootNode() }

// Close releases the underlying C tree. Safe to call on a nil receiver's
// nil inner tree (defensive against the ErrHighSyntaxErrorRate path, where
// callers must not hold a tree with no owner).
func (t *Tree) Close() {
	if t != nil && t.inner != nil {
		t.inner.Close()
	}
}

// Parse parses src with lang, enforcing the error-ratio gate and a timeout.
// On ErrHighSyntaxErrorRate, the tree is closed internally before returning
// — matching Grafel's fix for their issue #5963 (docs/research/01) — since
// no caller can use a tree attached to a rejected parse.
//
// Timeout and ctx cancellation are both enforced through
// ParseOptions.ProgressCallback, which tree-sitter polls periodically
// during parsing — the mechanism go-tree-sitter v0.25 is migrating to in
// place of the deprecated SetTimeoutMicros/ParseCtx (removed in 0.26).
func Parse(ctx context.Context, src []byte, lang *Language) (*Tree, error) {
	p := sitter.NewParser()
	defer p.Close()

	if err := p.SetLanguage(lang.inner); err != nil {
		return nil, fmt.Errorf("parser: setting language %s: %w", lang.name, err)
	}

	deadline := time.Now().Add(defaultTimeout)
	length := len(src)
	raw := p.ParseWithOptions(
		func(i int, _ sitter.Point) []byte {
			if i < length {
				return src[i:]
			}
			return []byte{}
		},
		nil,
		&sitter.ParseOptions{
			ProgressCallback: func(sitter.ParseState) bool {
				select {
				case <-ctx.Done():
					return true // cancel
				default:
				}
				return time.Now().After(deadline)
			},
		},
	)
	if raw == nil {
		return nil, fmt.Errorf("parser: parse of %s returned nil (timeout or cancellation)", lang.name)
	}

	ratio := errorRatio(raw.RootNode())
	if ratio > maxErrorRatio {
		raw.Close()
		return nil, fmt.Errorf("%w: %.1f%% (lang=%s)", ErrHighSyntaxErrorRate, ratio*100, lang.name)
	}

	return &Tree{inner: raw, Source: src}, nil
}

// errorRatio walks the tree counting ERROR nodes against total nodes. A
// full walk is O(nodes) but runs once per parse, which is cheap relative to
// the parse itself.
func errorRatio(root *sitter.Node) float64 {
	total, errs := 0, 0
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		total++
		if n.IsError() {
			errs++
		}
		cursor := n.Walk()
		defer cursor.Close()
		for _, child := range n.Children(cursor) {
			c := child
			walk(&c)
		}
	}
	walk(root)
	if total == 0 {
		return 0
	}
	return float64(errs) / float64(total)
}
