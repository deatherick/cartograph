// Package service is the single layer that owns product logic — CLI, and
// later MCP/HTTP/UI, are thin adapters over it, never duplicating logic
// between interfaces (see the project plan's "Restricciones permanentes").
//
// PHASE 1 SCOPE NOTE: there is no persistence yet (internal/store — SQLite
// + a mmap-able snapshot, per docs/adr/0003-data-model.md — is still
// unbuilt). Every call here re-runs a full in-memory index from scratch;
// nothing survives between CLI invocations. This is an honest, working
// vertical slice, not the finished Phase 1: incremental indexing and
// cross-invocation persistence are the next increment, not silently
// skipped.
package service

import (
	"context"
	"fmt"

	"github.com/deatherick/cartograph/internal/graph"
	"github.com/deatherick/cartograph/internal/index"
	"github.com/deatherick/cartograph/internal/model"
)

// Service is the product logic surface. Stateless today (see the package
// doc) — every method takes the repo root it needs to index.
type Service struct{}

// New constructs a Service.
func New() *Service { return &Service{} }

// Index runs a full index of root and returns the resulting graph plus
// run statistics (see internal/index.Stats — file/entity/edge counts,
// duration, and the disposition breakdown bug_rate is computed from).
func (s *Service) Index(ctx context.Context, root, repo string) (*index.Result, error) {
	return index.Run(ctx, root, repo)
}

// Find looks up every entity whose bare name exactly matches name. Exact
// match only for now — internal/search's FTS5/fuzzy layer is unbuilt; see
// the package doc.
func (s *Service) Find(ctx context.Context, root, repo, name string) ([]model.Entity, error) {
	result, err := s.Index(ctx, root, repo)
	if err != nil {
		return nil, err
	}
	var out []model.Entity
	for _, e := range result.Graph.Entities {
		if e.Name == name {
			out = append(out, e)
		}
	}
	return out, nil
}

// Related returns every entity within maxDepth graph hops of the entity
// whose bare name matches name, erroring if the name is ambiguous (matches
// more than one entity) or matches none — this is a CLI convenience over
// graph.Related, which operates on a concrete model.EntityID.
func (s *Service) Related(ctx context.Context, root, repo, name string, maxDepth int) ([]graph.RelatedEntity, error) {
	result, err := s.Index(ctx, root, repo)
	if err != nil {
		return nil, err
	}
	var match *model.Entity
	for _, e := range result.Graph.Entities {
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
	return result.Graph.Related(match.ID, maxDepth), nil
}

// Stats runs a full index and returns just the run statistics — the
// numbers Phase 1's exit criteria check.
func (s *Service) Stats(ctx context.Context, root, repo string) (index.Stats, error) {
	result, err := s.Index(ctx, root, repo)
	if err != nil {
		return index.Stats{}, err
	}
	return result.Stats, nil
}
