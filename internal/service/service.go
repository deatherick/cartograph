// Package service is the single layer that owns product logic — CLI, and
// later MCP/HTTP/UI, are thin adapters over it, never duplicating logic
// between interfaces (see the project plan's "Restricciones permanentes").
//
// Index runs the full pipeline and persists a snapshot (internal/store).
// Every other method reads that snapshot instead of re-indexing — the
// whole point of internal/store existing. If no snapshot is present, they
// return a clear error telling the caller to run Index first; there is no
// silent auto-reindex fallback, since that would quietly reintroduce a
// full reparse+reresolve on every query and defeat the persistence layer
// without anyone noticing.
package service

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/deatherick/cartograph/internal/compile"
	"github.com/deatherick/cartograph/internal/gitdiff"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/srcread"
	"github.com/deatherick/cartograph/internal/store"
)

// Service is the product logic surface.
type Service struct{}

// New constructs a Service.
func New() *Service { return &Service{} }

// RepoName derives a stable repo identity from a filesystem path when the
// caller has no better name — the last path component of the absolute
// path. Every interface (cmd/ctx, internal/mcpserver) uses this same
// derivation, since every Service method takes an explicit repo string
// rather than deriving it internally — kept here, not duplicated per
// interface, per the package doc's "no logic duplicated between
// interfaces" rule.
func RepoName(root string) string {
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Base(root)
	}
	return filepath.Base(strings.TrimRight(abs, string(filepath.Separator)))
}

// Index runs a full index of root, persists a snapshot for repo, and
// returns the run statistics (file/entity/edge counts, duration, and the
// disposition breakdown bug_rate is computed from).
func (s *Service) Index(ctx context.Context, root, repo string) (index.Stats, error) {
	result, err := index.Run(ctx, root, repo)
	if err != nil {
		return index.Stats{}, err
	}
	path, err := store.SnapshotPath(root, repo)
	if err != nil {
		return index.Stats{}, fmt.Errorf("service: resolving snapshot path: %w", err)
	}
	if err := store.Write(path, repo, result.Graph); err != nil {
		return index.Stats{}, fmt.Errorf("service: persisting snapshot: %w", err)
	}
	return result.Stats, nil
}

// open loads the persisted snapshot for root/repo, with the standard
// "run ctx index first" error when none exists yet.
func (s *Service) open(root, repo string) (*store.Snapshot, error) {
	path, err := store.SnapshotPath(root, repo)
	if err != nil {
		return nil, err
	}
	snap, err := store.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no index found for %s — run `ctx index %s` first (%w)", root, root, err)
	}
	return snap, nil
}

// Find looks up every entity matching name — exact bare-name match, or
// exact qualified-name match when name contains "#" (the "<file>#<Name>"
// / "<file>#<Owner>.<Name>" convention internal/parser/ts's entityFromMatch
// produces). This covers the "exact, qualified name" search Phase 1 scoped
// (docs/research/09) without a dedicated internal/search package — a
// linear scan over snap.All() is adequate at today's scale (tens to low
// thousands of entities). Fuzzy/full-text search (FTS5) is explicitly
// deferred; see docs/adr/0006-phase1-completion-and-search-scope.md for why.
func (s *Service) Find(root, repo, name string) ([]model.Entity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, err
	}
	byQualified := strings.Contains(name, "#")
	var out []model.Entity
	for _, e := range snap.All() {
		if (byQualified && e.Qualified == name) || (!byQualified && e.Name == name) {
			out = append(out, e)
		}
	}
	return out, nil
}

// findUnique locates the single entity in snap named name, erroring if
// none or more than one match. fileHint, if non-empty, first narrows
// candidates to those whose Anchor.File contains it — a substring
// qualifier for the common real-world case of two entities sharing a bare
// name in different files (found live: a Jest `describe("UserService",
// ...)` block collides with the actual `UserService` class by name).
// Shared by Related/Inspect/Source.
func findUnique(snap *store.Snapshot, name, fileHint string) (model.Entity, error) {
	var candidates []model.Entity
	for _, e := range snap.All() {
		if e.Name == name {
			candidates = append(candidates, e)
		}
	}
	if fileHint != "" {
		var narrowed []model.Entity
		for _, e := range candidates {
			if strings.Contains(e.Anchor.File, fileHint) {
				narrowed = append(narrowed, e)
			}
		}
		candidates = narrowed
	}
	if len(candidates) == 0 {
		if fileHint != "" {
			return model.Entity{}, fmt.Errorf("service: no entity named %q found in a file matching %q", name, fileHint)
		}
		return model.Entity{}, fmt.Errorf("service: no entity named %q found", name)
	}
	if len(candidates) > 1 {
		files := make([]string, len(candidates))
		for i, c := range candidates {
			files[i] = c.Anchor.File
		}
		return model.Entity{}, fmt.Errorf("service: %q is ambiguous across %v — disambiguate with --file <substring>", name, files)
	}
	return candidates[0], nil
}

// Related returns every entity within maxDepth graph hops of the entity
// whose bare name matches name, erroring if the name is ambiguous (matches
// more than one entity) or matches none.
func (s *Service) Related(root, repo, name, fileHint string, maxDepth int) ([]model.RelatedEntity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, err
	}
	match, err := findUnique(snap, name, fileHint)
	if err != nil {
		return nil, err
	}
	return snap.Related(match.ID, maxDepth), nil
}

// PathResult is the shortest chain of edges connecting two entities — the
// answer to "how does A reach B" for two names an agent already knows,
// rather than Related's "what's near A" or Impact's "what depends on A".
type PathResult struct {
	From  model.Entity
	To    model.Entity
	Path  []model.RelatedEntity // From -> ... -> To, in order; empty if Found is false
	Found bool
}

// Path finds the shortest path (fewest hops, either edge direction, same
// semantics as Related/Upstream) from the entity named fromName to the
// entity named toName, erroring if either name is ambiguous or unmatched.
func (s *Service) Path(root, repo, fromName, fromFileHint, toName, toFileHint string) (PathResult, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return PathResult{}, err
	}
	from, err := findUnique(snap, fromName, fromFileHint)
	if err != nil {
		return PathResult{}, err
	}
	to, err := findUnique(snap, toName, toFileHint)
	if err != nil {
		return PathResult{}, err
	}
	path, ok := snap.ShortestPath(from.ID, to.ID)
	return PathResult{From: from, To: to, Path: path, Found: ok}, nil
}

// Inspection is everything ctx inspect shows for one entity: the entity
// itself plus its fan-in/fan-out edges — the graph data the master plan's
// Phase 1 scope names explicitly (fan_in/fan_out) and that was previously
// computed but never surfaced through any interface.
type Inspection struct {
	Entity model.Entity
	FanIn  []model.Edge
	FanOut []model.Edge
}

// Inspect returns full detail on the entity named name: its declaration
// plus who calls/extends/implements/uses it (FanIn) and what it calls/
// extends/implements/uses (FanOut).
func (s *Service) Inspect(root, repo, name, fileHint string) (Inspection, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return Inspection{}, err
	}
	match, err := findUnique(snap, name, fileHint)
	if err != nil {
		return Inspection{}, err
	}
	return Inspection{Entity: match, FanIn: snap.FanIn(match.ID), FanOut: snap.FanOut(match.ID)}, nil
}

// Source returns the source lines the entity named name spans, read
// directly from the file on disk at root/Anchor.File — the snapshot
// stores locations, not file contents (see docs/adr/0005-snapshot-persistence.md),
// so this always re-reads from the working tree, which may have moved on
// since the last `ctx index` (a documented Phase 1 staleness gap, same
// one ADR-0005 already names for find/related/stats).
func (s *Service) Source(root, repo, name, fileHint string) (string, model.Entity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return "", model.Entity{}, err
	}
	match, err := findUnique(snap, name, fileHint)
	if err != nil {
		return "", model.Entity{}, err
	}
	src, err := srcread.Lines(filepath.Join(root, filepath.FromSlash(match.Anchor.File)), match.Anchor.StartLine, match.Anchor.EndLine)
	if err != nil {
		return "", model.Entity{}, fmt.Errorf("service: reading source for %s: %w", match.Qualified, err)
	}
	return src, match, nil
}

// Context compiles a token-budgeted capsule for task — the Context
// Compiler's entry point (internal/compile), the product's central
// feature (see the master plan's Phase 2 framing). sessionID enables the
// Context Ledger: repeat calls with the same non-empty sessionID cost
// fewer tokens for anything already delivered. Requires a snapshot to
// already exist (same "run ctx index first" contract as every other read
// path).
func (s *Service) Context(root, repo, task string, budget int, sessionID string) (*compile.Capsule, error) {
	return compile.Compile(root, repo, task, compile.Options{Budget: budget, SessionID: sessionID})
}

// Stats loads the persisted snapshot and returns summary counts — a
// lighter-weight relative of Index's Stats (no disposition breakdown,
// since that's a run-time resolver artifact not persisted in the
// snapshot; see store/format.go for what is and isn't stored).
type Stats struct {
	Entities int
	Repo     string
}

// Stats returns summary counts from the persisted snapshot.
func (s *Service) Stats(root, repo string) (Stats, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return Stats{}, err
	}
	return Stats{Entities: len(snap.All()), Repo: snap.Repo}, nil
}

// Graph is the whole persisted graph — every entity and every edge — the
// data internal/httpserver's web UI (Phase 6) renders. Not something the
// CLI needed before now: `ctx related`/`ctx inspect` only ever needed one
// entity's neighborhood, never the entire graph at once.
type Graph struct {
	Entities []model.Entity
	Edges    []model.Edge
}

// Graph returns every entity and edge in the persisted snapshot for root/
// repo. Edges are derived by taking each entity's FanOut once — since
// every model.Edge has exactly one Src, concatenating FanOut across every
// entity yields the complete edge set with no duplication, without
// internal/store needing a dedicated "all edges" accessor of its own.
func (s *Service) Graph(root, repo string) (Graph, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return Graph{}, err
	}
	entities := snap.All()
	var edges []model.Edge
	for _, e := range entities {
		edges = append(edges, snap.FanOut(e.ID)...)
	}
	return Graph{Entities: entities, Edges: edges}, nil
}

// ImpactResult is one entity's blast radius: everything that transitively
// depends on it (Transitive, via internal/store.Upstream — callers, and
// their callers, and so on, with no depth limit by default), the subset
// one hop away (DirectCallers), and which of the transitively-dependent
// entities are tests (CoveringTests) — the tests worth running after
// changing Target, found without any dedicated TESTS edge (docs/model.go
// defines model.EdgeTests but no extractor currently emits it): a Test
// entity that calls Target, directly or transitively, IS a test covering
// it, and that is exactly what Upstream's closure already contains.
type ImpactResult struct {
	Target        model.Entity
	DirectCallers []model.Entity
	Transitive    []model.RelatedEntity
	CoveringTests []model.Entity
}

// impactFor computes target's blast radius against an already-open
// snapshot — the shared core both Impact (by entity name) and
// ImpactFromGitDiff (by changed file/line ranges) build on, so the two
// entry points never compute this differently.
func impactFor(snap *store.Snapshot, target model.Entity, maxDepth int) ImpactResult {
	upstream := snap.Upstream(target.ID, maxDepth)
	result := ImpactResult{Target: target, Transitive: upstream}
	for _, r := range upstream {
		if r.Depth == 1 {
			result.DirectCallers = append(result.DirectCallers, r.Entity)
		}
		if r.Entity.Kind == model.KindTest {
			result.CoveringTests = append(result.CoveringTests, r.Entity)
		}
	}
	return result
}

// Impact computes the entity named name's blast radius: every entity that
// transitively depends on it, and which of those are tests. maxDepth<=0
// means the full transitive closure (see store.Upstream's doc for why
// that's impact analysis's natural default, unlike Related's interactive
// default of 2 hops).
func (s *Service) Impact(root, repo, name, fileHint string, maxDepth int) (ImpactResult, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return ImpactResult{}, err
	}
	match, err := findUnique(snap, name, fileHint)
	if err != nil {
		return ImpactResult{}, err
	}
	return impactFor(snap, match, maxDepth), nil
}

// GitDiffImpact is what changed (per internal/gitdiff's line-range
// mapping) and everything that change transitively affects, aggregated
// across every directly-changed entity.
type GitDiffImpact struct {
	ChangedEntities  []model.Entity
	ImpactedEntities []model.Entity // union of every changed entity's Transitive closure, deduplicated
	RecommendedTests []model.Entity // union of every changed entity's CoveringTests, deduplicated
}

// ImpactFromGitDiff runs `git diff --unified=0 <gitRef>` against root
// (gitRef defaults to "HEAD" — working tree vs the last commit — when
// empty), maps the changed line ranges to entities whose Anchor overlaps
// them, and unions each changed entity's blast radius. This is Phase 4's
// git-diff-driven mode (docs/MVP.md: "ctx impact --git-diff [ref]") — the
// same impactFor core Impact uses, just seeded from a diff instead of one
// named entity.
func (s *Service) ImpactFromGitDiff(root, repo, gitRef string, maxDepth int) (GitDiffImpact, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return GitDiffImpact{}, err
	}
	if gitRef == "" {
		gitRef = "HEAD"
	}
	diffOutput, err := gitdiff.Diff(root, gitRef)
	if err != nil {
		return GitDiffImpact{}, err
	}
	changedRanges := gitdiff.ParseChangedRanges(diffOutput)

	seenChanged := map[model.EntityID]bool{}
	seenImpacted := map[model.EntityID]bool{}
	seenTests := map[model.EntityID]bool{}
	var out GitDiffImpact

	for _, e := range snap.All() {
		ranges, ok := changedRanges[e.Anchor.File]
		if !ok {
			continue
		}
		for _, r := range ranges {
			if !r.Overlaps(e.Anchor.StartLine, e.Anchor.EndLine) {
				continue
			}
			if !seenChanged[e.ID] {
				seenChanged[e.ID] = true
				out.ChangedEntities = append(out.ChangedEntities, e)
			}
			res := impactFor(snap, e, maxDepth)
			for _, rel := range res.Transitive {
				if !seenImpacted[rel.Entity.ID] {
					seenImpacted[rel.Entity.ID] = true
					out.ImpactedEntities = append(out.ImpactedEntities, rel.Entity)
				}
			}
			for _, test := range res.CoveringTests {
				if !seenTests[test.ID] {
					seenTests[test.ID] = true
					out.RecommendedTests = append(out.RecommendedTests, test)
				}
			}
			break // one overlapping range is enough to flag this entity as changed
		}
	}
	return out, nil
}
