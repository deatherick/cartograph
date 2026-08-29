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
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
	"github.com/deatherick/cartograph/internal/store"
)

// Service is the product logic surface.
type Service struct{}

// New constructs a Service.
func New() *Service { return &Service{} }

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
// deferred; see docs/adr/0006-search-scope.md for why.
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

// Inspection is everything ctx inspect shows for one entity: the entity
// itself plus its fan-in/fan-out edges — the graph data the master plan's
// Phase 1 scope names explicitly (fan_in/fan_out) and that was previously
// computed but never surfaced through any interface.
type Inspection struct {
	Entity  model.Entity
	FanIn   []model.Edge
	FanOut  []model.Edge
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
	src, err := readLines(filepath.Join(root, filepath.FromSlash(match.Anchor.File)), match.Anchor.StartLine, match.Anchor.EndLine)
	if err != nil {
		return "", model.Entity{}, fmt.Errorf("service: reading source for %s: %w", match.Qualified, err)
	}
	return src, match, nil
}

func readLines(path string, startLine, endLine int) (string, error) {
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
