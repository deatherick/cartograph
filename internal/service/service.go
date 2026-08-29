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

// Find looks up every entity whose bare name exactly matches name. Exact
// match only for now — internal/search's FTS5/fuzzy layer is unbuilt.
func (s *Service) Find(root, repo, name string) ([]model.Entity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, err
	}
	var out []model.Entity
	for _, e := range snap.All() {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// Related returns every entity within maxDepth graph hops of the entity
// whose bare name matches name, erroring if the name is ambiguous (matches
// more than one entity) or matches none.
func (s *Service) Related(root, repo, name string, maxDepth int) ([]model.RelatedEntity, error) {
	snap, err := s.open(root, repo)
	if err != nil {
		return nil, err
	}
	var match *model.Entity
	for _, e := range snap.All() {
		if e.Name != name {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("service: %q is ambiguous — matches both %s and %s (qualify by file, not yet supported)", name, match.Qualified, e.Qualified)
		}
		ent := e
		match = &ent
	}
	if match == nil {
		return nil, fmt.Errorf("service: no entity named %q found", name)
	}
	return snap.Related(match.ID, maxDepth), nil
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
