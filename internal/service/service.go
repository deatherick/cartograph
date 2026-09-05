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
	"github.com/deatherick/cartograph/internal/similar"
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
	meta := store.Meta{Files: result.Stats.Files, Dispositions: result.Stats.Dispositions}
	if err := store.Write(path, repo, result.Graph, meta); err != nil {
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
	return s.ContextScoped(root, repo, task, budget, sessionID, "")
}

// ContextScoped is Context plus fileFilter — scopes seeding to entities
// whose file contains fileFilter (the same `--file` disambiguation
// convention every other lookup already uses, findUnique above), closing
// docs/MVP.md's own "a task capsule can't currently be scoped to 'only
// consider files matching X'" gap. fileFilter="" behaves exactly like
// Context (unchanged).
func (s *Service) ContextScoped(root, repo, task string, budget int, sessionID, fileFilter string) (*compile.Capsule, error) {
	return compile.Compile(root, repo, task, compile.Options{Budget: budget, SessionID: sessionID, FileFilter: fileFilter})
}

// Stats loads the persisted snapshot and returns summary counts, including
// the disposition breakdown and BugRate — since format version 2 (see
// docs/adr/0017-persisted-quality-stats.md) these survive past the one
// process that ran `ctx index`, so `ctx stats` can show them without
// requiring a fresh reindex.
type Stats struct {
	Entities     int
	Repo         string
	Files        int
	Dispositions map[model.Disposition]int
	BugRate      float64
}

// Stats returns summary counts from the persisted snapshot.
func (s *Service) Stats(root, repo string) (Stats, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return Stats{}, err
	}
	return Stats{
		Entities:     len(snap.All()),
		Repo:         snap.Repo,
		Files:        snap.Files(),
		Dispositions: snap.Dispositions(),
		BugRate:      snap.BugRate(),
	}, nil
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

// decisionsPath resolves the per-repo path internal/similar's Decisions
// persist to — namespaced under the same directory internal/store
// (snapshots) and internal/ledger (sessions) already use.
func (s *Service) decisionsPath(root, repo string) (string, error) {
	dir, err := store.RepoDir(root, repo)
	if err != nil {
		return "", fmt.Errorf("service: resolving repo directory: %w", err)
	}
	return similar.DecisionsPath(dir), nil
}

func (s *Service) loadDecisions(root, repo string) (*similar.Decisions, error) {
	path, err := s.decisionsPath(root, repo)
	if err != nil {
		return nil, err
	}
	d, err := similar.LoadDecisions(path)
	if err != nil {
		return nil, fmt.Errorf("service: loading duplicate decisions: %w", err)
	}
	return d, nil
}

// PairWithEntities pairs one similar.Pair with both entities it names —
// similar.Pair itself only carries EntityIDs (internal/similar has no
// business knowing about model.Entity display fields), so this is what
// every caller (render, MCP) actually wants: the score AND enough to show
// a human which two entities it's about, with no separate lookup.
type PairWithEntities struct {
	Pair similar.Pair
	A, B model.Entity
}

func resolvePairs(snap *store.Snapshot, pairs []similar.Pair) []PairWithEntities {
	out := make([]PairWithEntities, 0, len(pairs))
	for _, p := range pairs {
		a, aok := snap.Lookup(p.A)
		b, bok := snap.Lookup(p.B)
		if !aok || !bok {
			continue // an entity removed since Find last ran (stale snapshot mid-read) — skip rather than show a blank half-pair
		}
		out = append(out, PairWithEntities{Pair: p, A: a, B: b})
	}
	return out
}

// Similar finds every undecided similarity/duplicate candidate involving
// the entity named name — internal/similar's Function/Method-only V0
// scope, see its package doc. Returns the matched entity alongside the
// pairs so a caller can render both without a second lookup.
func (s *Service) Similar(root, repo, name, fileHint string) ([]PairWithEntities, model.Entity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, model.Entity{}, err
	}
	match, err := findUnique(snap, name, fileHint)
	if err != nil {
		return nil, model.Entity{}, err
	}
	all, err := similar.Find(snap, root, 0)
	if err != nil {
		return nil, model.Entity{}, fmt.Errorf("service: finding similarity candidates: %w", err)
	}
	decisions, err := s.loadDecisions(root, repo)
	if err != nil {
		return nil, model.Entity{}, err
	}
	undecided := decisions.Filter(all)

	var out []similar.Pair
	for _, p := range undecided {
		if p.A == match.ID || p.B == match.ID {
			out = append(out, p)
		}
	}
	return resolvePairs(snap, out), match, nil
}

// Duplicates returns every undecided similarity/duplicate pair across the
// whole repo whose Overall score is >= threshold (threshold<=0 uses
// similar.DefaultThreshold), sorted by Overall descending.
func (s *Service) Duplicates(root, repo string, threshold float64) ([]PairWithEntities, error) {
	if threshold <= 0 {
		threshold = similar.DefaultThreshold
	}
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, err
	}
	pairs, err := similar.Find(snap, root, threshold)
	if err != nil {
		return nil, fmt.Errorf("service: finding duplicate candidates: %w", err)
	}
	decisions, err := s.loadDecisions(root, repo)
	if err != nil {
		return nil, err
	}
	return resolvePairs(snap, decisions.Filter(pairs)), nil
}

// Decide records a human decision on the pair (nameA, nameB) — persisted
// so it never resurfaces via Similar/Duplicates again. Both names are
// resolved the same ambiguity-checked way every other by-name lookup in
// this file is.
func (s *Service) Decide(root, repo, nameA, fileHintA, nameB, fileHintB string, decision similar.Decision) error {
	snap, err := s.open(root, repo)
	if err != nil {
		return err
	}
	a, err := findUnique(snap, nameA, fileHintA)
	if err != nil {
		return err
	}
	b, err := findUnique(snap, nameB, fileHintB)
	if err != nil {
		return err
	}
	decisions, err := s.loadDecisions(root, repo)
	if err != nil {
		return err
	}
	decisions.Decide(a.ID, b.ID, decision)
	path, err := s.decisionsPath(root, repo)
	if err != nil {
		return err
	}
	if err := decisions.Save(path); err != nil {
		return fmt.Errorf("service: saving duplicate decision: %w", err)
	}
	return nil
}
