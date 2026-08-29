// Package index orchestrates a full (non-incremental) index of a repo:
// walk files, extract each with the matching language extractor, resolve
// every ref across the whole repo, and build the in-memory graph.
// Incremental re-indexing (content-hash-based re-anchoring, the watcher)
// is Phase 3 — this is deliberately the simplest thing that can work
// end-to-end, per the project's own "vertical slice, not everything at
// once" principle.
package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/deatherick/cartograph/internal/exclude"
	"github.com/deatherick/cartograph/internal/graph"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/parser"
	"github.com/deatherick/cartograph/internal/parser/ts"
	"github.com/deatherick/cartograph/internal/resolve"
)

// Stats summarizes one index run — the numbers Phase 1's exit criteria
// check (see the project plan): file/entity/edge counts, wall time, and
// the disposition breakdown that bug_rate is computed from.
type Stats struct {
	Files          int
	Entities       int
	ResolvedEdges  int
	Dispositions   map[model.Disposition]int
	Duration       time.Duration
}

// BugRate is (bug-extractor + bug-resolver) / total dispositions —
// docs/research/02-refs-and-dispositions.md. Grafel's measured range on
// real corpora is 7.8%-12%; Phase 1's exit criterion is <=15% for
// TypeScript alone (see the project plan). Returns 0 if there were no
// refs to resolve at all (an empty repo, not a claim of perfection).
func (s Stats) BugRate() float64 {
	total := 0
	bugs := 0
	for d, n := range s.Dispositions {
		total += n
		if d.IsBug() {
			bugs += n
		}
	}
	if total == 0 {
		return 0
	}
	return float64(bugs) / float64(total)
}

// Result is everything an index run produces.
type Result struct {
	Graph *graph.Graph
	Stats Stats
}

// extractors maps a file extension to the extractor that handles it.
// Adding csharp/python (Phase 3) is one entry each, not a rewrite.
func extractors() map[string]parser.Extractor {
	m := map[string]parser.Extractor{}
	tsExt := ts.New()
	for _, ext := range tsExt.Extensions() {
		m[ext] = tsExt
	}
	return m
}

// Run walks root, extracts every recognized file, resolves every ref
// repo-wide, and returns the built graph plus run statistics. repo is the
// identity namespace entities are scoped to (see docs/adr/0003-data-model.md)
// — typically the repo's directory name, but callers may pass anything
// stable.
func Run(ctx context.Context, root, repo string) (*Result, error) {
	start := time.Now()
	exts := extractors()

	var allFacts []*model.FileFacts
	err := exclude.WalkSource(root, func(path string, content []byte) error {
		ext := filepath.Ext(path)
		extractor, ok := exts[ext]
		if !ok {
			return nil // not a recognized source file; skip silently
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		facts, extractErr := extractor.Extract(ctx, repo, rel, content)
		if extractErr != nil {
			// A single file's parse failure (high error ratio, timeout)
			// does not abort the whole index — it is reported and
			// skipped, matching the fail-soft principle documented in
			// docs/research/08-process-architecture-and-residuals.md
			// (Grafel: an over-cap marshal aborts cleanly rather than
			// crashing the whole daemon). Surfacing per-file failures
			// as first-class Stats output is a documented follow-up;
			// today they are silently skipped, which is the wrong
			// default long-term but an honest, visible gap for Phase 1.
			fmt.Fprintf(os.Stderr, "index: skipping %s: %v\n", rel, extractErr)
			return nil
		}
		allFacts = append(allFacts, facts)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("index: walking %s: %w", root, err)
	}

	resolverIdx := resolve.NewIndex(repo)
	for _, f := range allFacts {
		resolverIdx.AddFile(f)
	}

	g := graph.New()
	for _, f := range allFacts {
		for _, e := range f.Entities {
			g.AddEntity(e)
		}
	}

	stats := Stats{
		Files:        len(allFacts),
		Dispositions: map[model.Disposition]int{},
	}
	for _, f := range allFacts {
		stats.Entities += len(f.Entities)
	}

	for _, f := range allFacts {
		for _, resolved := range resolverIdx.Resolve([]*model.FileFacts{f}) {
			stats.Dispositions[resolved.Disposition]++
			if resolved.Disposition == model.DispositionResolved && resolved.Edge != nil {
				g.AddEdge(*resolved.Edge)
				stats.ResolvedEdges++
			}
		}
	}

	stats.Duration = time.Since(start)
	return &Result{Graph: g, Stats: stats}, nil
}
