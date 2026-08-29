// True per-file incremental indexing (ADR-0020) — the daemon-side
// counterpart to Run's one-shot full index. Indexer holds a graph and a
// resolver alive across many updates instead of rebuilding either from
// scratch on every change; cmd/ctxd builds one Indexer at startup
// (FullIndex), then calls UpdateFiles as its watcher reports changed
// paths.
//
// The hard problem this file exists to solve, stated plainly (see
// ADR-0020 for the full design): re-extracting just the changed file is
// not enough. internal/resolve's import-table/same-scope tiers mean
// another file's resolution outcome can depend on this one's exports —
// resolve.Index.Dependents finds every such file, and they are
// re-resolved (not re-extracted; their own content didn't change, so
// their cached model.FileFacts are reused) alongside the file that
// actually changed.
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
	"github.com/deatherick/cartograph/internal/resolve"
)

// Indexer holds one project's live index state. The zero value is not
// usable; construct with NewIndexer.
type Indexer struct {
	root, repo string
	langs      []Language
	exts       map[string]parser.Extractor

	resolver *resolve.Index
	graph    *graph.Graph

	// facts caches every currently-indexed file's last-extracted
	// model.FileFacts — needed to re-resolve a DEPENDENT file (one that
	// didn't change itself but whose resolution outcome might, because a
	// file it imports did) without re-extracting it: resolve.Index.Resolve
	// needs a file's Refs, which fileEntry does not store internally
	// (see resolve.go), so the Indexer keeps its own copy.
	facts map[string]*model.FileFacts

	// perFileDispositions + dispositions together let UpdateFiles report
	// a repo-wide bug_rate/disposition breakdown (matching what FullIndex
	// and a full `ctx index` both already show) without re-resolving the
	// whole repo on every small change: each file's own disposition
	// counts are cached, and the running total is adjusted incrementally
	// (subtract the file's old counts, add its new ones) whenever a file
	// is (re-)resolved or removed.
	perFileDispositions map[string]map[model.Disposition]int
	dispositions        map[model.Disposition]int
}

// NewIndexer builds an Indexer for root/repo — enabledLanguages is
// resolved once here (matching Run's own behavior: language selection is
// fixed for the run, not re-evaluated per file).
func NewIndexer(root, repo string) *Indexer {
	langs := enabledLanguages(root)
	exts := map[string]parser.Extractor{}
	for _, l := range langs {
		for _, ext := range l.Extractor.Extensions() {
			exts[ext] = l.Extractor
		}
	}
	return &Indexer{
		root: root, repo: repo,
		langs: langs, exts: exts,
		facts:               map[string]*model.FileFacts{},
		perFileDispositions: map[string]map[model.Disposition]int{},
		dispositions:        map[model.Disposition]int{},
	}
}

// Graph returns the Indexer's live graph — the same instance FullIndex/
// UpdateFiles mutate in place, so a caller (e.g. cmd/ctxd, persisting via
// store.Write after each update) always sees the current state.
func (ix *Indexer) Graph() *graph.Graph { return ix.graph }

// FullIndex walks the whole tree and (re)builds the graph and resolver
// from scratch — identical behavior to the package-level Run, except the
// result stays live in ix for later incremental UpdateFiles calls. Safe
// to call more than once (e.g. a crash-recovery reconcile pass): each
// call discards whatever state ix held before it, so a file deleted since
// the last FullIndex actually disappears rather than lingering because
// nothing told a fresh walk to remove it.
func (ix *Indexer) FullIndex(ctx context.Context) (Stats, error) {
	start := time.Now()

	ix.graph = graph.New()
	ix.resolver = resolve.NewIndex(ix.repo)
	for _, l := range ix.langs {
		ix.resolver.RegisterPolicy(l.Policy)
	}
	ix.facts = map[string]*model.FileFacts{}
	ix.perFileDispositions = map[string]map[model.Disposition]int{}
	ix.dispositions = map[model.Disposition]int{}

	var allFacts []*model.FileFacts
	err := exclude.WalkSource(ix.root, func(path string, content []byte) error {
		ext := filepath.Ext(path)
		extractor, ok := ix.exts[ext]
		if !ok {
			return nil
		}
		rel, relErr := filepath.Rel(ix.root, path)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		facts, extractErr := extractor.Extract(ctx, ix.repo, rel, content)
		if extractErr != nil {
			fmt.Fprintf(os.Stderr, "index: skipping %s: %v\n", rel, extractErr)
			return nil
		}
		allFacts = append(allFacts, facts)
		return nil
	})
	if err != nil {
		return Stats{}, fmt.Errorf("index: walking %s: %w", ix.root, err)
	}

	for _, f := range allFacts {
		ix.facts[f.File] = f
		ix.resolver.AddFile(f)
	}
	for _, f := range allFacts {
		for _, e := range f.Entities {
			ix.graph.AddEntity(e)
		}
	}
	for _, f := range allFacts {
		ix.resolveFile(f)
	}

	return ix.recomputeStats(start), nil
}

// UpdateFiles re-processes exactly the given changed (absolute) file
// paths PLUS every file whose resolution outcome could change as a
// result (resolve.Index.Dependents — importers, barrel re-exporters,
// same-scope package siblings), updating ix's graph/resolver in place.
// Returns fresh repo-wide Stats (the same shape a full reindex reports),
// and a non-nil error only to report that at least one file failed to
// re-extract (docs/research/edge-case-backlog.md's F1: that file's PRIOR
// state is left untouched, not wiped — the returned Stats still reflect
// everything else in the batch that succeeded).
func (ix *Indexer) UpdateFiles(ctx context.Context, changedAbs []string) (Stats, error) {
	start := time.Now()

	changed := map[string]bool{}
	for _, abs := range changedAbs {
		rel, err := filepath.Rel(ix.root, abs)
		if err != nil {
			continue // outside root somehow — ignore rather than fail the whole batch
		}
		changed[filepath.ToSlash(rel)] = true
	}
	if len(changed) == 0 {
		return ix.recomputeStats(start), nil
	}

	// Dependents computed BEFORE any file is re-extracted: this is what
	// correctly captures a DELETED file's importers/siblings, since that
	// information (its directory, its import table) would be gone from
	// the resolver once it's removed.
	impacted := map[string]bool{}
	for f := range changed {
		impacted[f] = true
		for _, dep := range ix.resolver.Dependents(f) {
			impacted[dep] = true
		}
	}

	touched := map[string]*model.FileFacts{}
	var reindexErr error
	for f := range changed {
		facts, ok, err := ix.reindexOneFile(ctx, f)
		if err != nil {
			// F1: extraction failed — prior state for f is left exactly as
			// it was (reindexOneFile does not remove anything before a
			// successful extraction), so nothing is silently lost.
			fmt.Fprintf(os.Stderr, "index: skipping %s (extraction error, keeping prior state): %v\n", f, err)
			reindexErr = err
			continue
		}
		if ok {
			touched[f] = facts
		}
	}

	// Dependents computed AGAIN, now that changed files are registered:
	// this is what correctly captures a NEWLY CREATED file's importers,
	// who only resolve to it now that it exists in the resolver — a
	// lookup made before this point would have found nothing.
	for f := range changed {
		for _, dep := range ix.resolver.Dependents(f) {
			impacted[dep] = true
		}
	}

	for f := range impacted {
		facts, ok := touched[f]
		if !ok {
			facts, ok = ix.facts[f]
		}
		if !ok {
			continue // deleted (or never indexed) — nothing left to resolve
		}
		ix.resolveFile(facts)
	}

	return ix.recomputeStats(start), reindexErr
}

// reindexOneFile re-extracts file (repo-relative, slash-normalized) from
// its current on-disk content and updates ix.graph/ix.resolver/ix.facts
// for it. changed=false with err=nil covers two cases that both mean "no
// further work needed for this file": it no longer exists on disk (already
// removed via removeFileState), or its freshly-extracted entities are
// byte-for-byte identical to what's already indexed (docs/research/
// edge-case-backlog.md's F8: a revert to already-indexed content, or F7:
// a delete-then-recreate-identical within one debounce window — both
// collapse to a correct no-op here, since this always reads CURRENT
// on-disk state rather than reacting to each individual watch event).
func (ix *Indexer) reindexOneFile(ctx context.Context, file string) (facts *model.FileFacts, changed bool, err error) {
	if exclude.SkipFile(filepath.Base(file)) {
		return nil, false, nil
	}
	abs := filepath.Join(ix.root, filepath.FromSlash(file))
	info, statErr := os.Lstat(abs)
	if statErr != nil || info.IsDir() {
		ix.removeFileState(file)
		return nil, false, nil
	}
	extractor, ok := ix.exts[filepath.Ext(file)]
	if !ok {
		return nil, false, nil // not a recognized/enabled extension — nothing to do
	}
	content, readErr := os.ReadFile(abs)
	if readErr != nil {
		return nil, false, readErr
	}
	if exclude.IsBinary(content) {
		return nil, false, nil
	}
	fresh, extractErr := extractor.Extract(ctx, ix.repo, file, content)
	if extractErr != nil {
		return nil, false, extractErr
	}

	if ix.resolver.HasFile(file) && ix.unchanged(file, fresh.Entities) {
		return nil, false, nil
	}

	ix.removeFileState(file)
	for _, e := range fresh.Entities {
		ix.graph.AddEntity(e)
	}
	ix.resolver.AddFile(fresh)
	ix.facts[file] = fresh
	return fresh, true, nil
}

// unchanged reports whether file's already-indexed entities are exactly
// the freshly-extracted set — same IDs, same per-entity ContentHash. Only
// meaningful for a file already known to the resolver (checked by the
// caller via HasFile first): a brand-new file with zero entities must
// never be treated as "unchanged" just because len(old)==len(new)==0.
func (ix *Indexer) unchanged(file string, fresh []model.Entity) bool {
	old := ix.graph.EntitiesInFile(file)
	if len(old) != len(fresh) {
		return false
	}
	oldHash := make(map[model.EntityID]string, len(old))
	for _, e := range old {
		oldHash[e.ID] = e.Anchor.ContentHash
	}
	for _, e := range fresh {
		h, ok := oldHash[e.ID]
		if !ok || h != e.Anchor.ContentHash {
			return false
		}
	}
	return true
}

// removeFileState purges file entirely from the graph, the resolver, the
// cached facts, and the running disposition total — the shared cleanup
// both a genuine deletion and a re-extraction (remove-then-re-add) use.
func (ix *Indexer) removeFileState(file string) {
	ix.graph.RemoveFile(file)
	ix.resolver.RemoveFile(file)
	delete(ix.facts, file)
	ix.subtractFile(file)
}

// resolveFile (re-)resolves f's refs against the current resolver state
// and adds any newly-resolved edges to the graph. Always clears f's PRIOR
// disposition contribution first — safe whether or not one existed yet
// (subtractFile is a no-op the first time) — so the running total is
// never double-counted across repeated calls for the same file (an
// UpdateFiles batch that includes both a changed file and its own
// dependents can, in principle, resolve the same file more than once
// across a session; each call fully replaces that file's contribution,
// never adds to it).
func (ix *Indexer) resolveFile(f *model.FileFacts) {
	ix.subtractFile(f.File)
	counts := map[model.Disposition]int{}
	for _, resolved := range ix.resolver.Resolve([]*model.FileFacts{f}) {
		counts[resolved.Disposition]++
		if resolved.Disposition == model.DispositionResolved && resolved.Edge != nil {
			ix.graph.AddEdge(*resolved.Edge)
		}
	}
	ix.perFileDispositions[f.File] = counts
	for d, n := range counts {
		ix.dispositions[d] += n
	}
}

func (ix *Indexer) subtractFile(file string) {
	counts, ok := ix.perFileDispositions[file]
	if !ok {
		return
	}
	for d, n := range counts {
		ix.dispositions[d] -= n
		if ix.dispositions[d] <= 0 {
			delete(ix.dispositions, d)
		}
	}
	delete(ix.perFileDispositions, file)
}

// recomputeStats builds a Stats snapshot from ix's current live state —
// entity count directly from the graph, dispositions/resolved-edges from
// the running total this file maintains incrementally (never a full
// repo-wide re-resolution), file count from the resolver's registered
// file set.
func (ix *Indexer) recomputeStats(start time.Time) Stats {
	dispositions := make(map[model.Disposition]int, len(ix.dispositions))
	for d, n := range ix.dispositions {
		dispositions[d] = n
	}
	return Stats{
		Files:         ix.resolver.FileCount(),
		Entities:      len(ix.graph.Entities),
		ResolvedEdges: dispositions[model.DispositionResolved],
		Dispositions:  dispositions,
		Duration:      time.Since(start),
	}
}
