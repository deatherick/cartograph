// Package index orchestrates indexing a repo: walk files, extract each
// with the matching language extractor, resolve every ref, and build the
// in-memory graph. Run is the one-shot full index every non-daemon caller
// uses (`ctx index`, ctxbench, tests); Indexer (indexer.go) is the
// stateful, incremental counterpart cmd/ctxd uses — one FullIndex at
// startup, then repeated UpdateFiles calls as its watcher reports changed
// paths, re-processing only the affected files (and whatever else their
// change could affect — see resolve.Index.Dependents) instead of
// re-walking the whole tree every time. See ADR-0020 for the design.
package index

import (
	"context"
	"time"

	"github.com/deatherick/cartograph/internal/graph"
	"github.com/deatherick/cartograph/internal/model"
)

// Stats summarizes one index run — the numbers Phase 1's exit criteria
// check (see the project plan): file/entity/edge counts, wall time, and
// the disposition breakdown that bug_rate is computed from.
type Stats struct {
	Files         int
	Entities      int
	ResolvedEdges int
	Dispositions  map[model.Disposition]int
	Duration      time.Duration
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

// Run walks root, extracts every recognized file using whichever
// languages are enabled (see enabledLanguages — every detected language by
// default, or exactly what .cartograph.json names, editable via `ctx
// init` or by hand), resolves every ref repo-wide, and returns the built
// graph plus run statistics. repo is the identity namespace entities are
// scoped to (see docs/adr/0003-data-model.md) — typically the repo's
// directory name, but callers may pass anything stable.
//
// A thin one-shot wrapper over Indexer.FullIndex (ADR-0020) — every
// caller that just wants "index once and get a Result" (the CLI's `ctx
// index`, ctxbench, tests) keeps this exact signature; cmd/ctxd is the
// one caller that needs the live Indexer itself, to follow FullIndex with
// incremental UpdateFiles calls as its watcher reports changes.
func Run(ctx context.Context, root, repo string) (*Result, error) {
	ix := NewIndexer(root, repo)
	stats, err := ix.FullIndex(ctx)
	if err != nil {
		return nil, err
	}
	return &Result{Graph: ix.Graph(), Stats: stats}, nil
}
