package parser

import (
	"context"

	"github.com/deatherick/cartograph/internal/model"
)

// Extractor is the contract every language extractor implements. It is
// defined here — inside the architecture boundary — but its signature
// exposes only model types, never a sitter type, since callers outside
// internal/parser/** (internal/index, in particular) program against this
// interface without ever importing tree-sitter themselves.
type Extractor interface {
	// Extensions lists the file extensions this extractor handles (e.g.
	// ".ts", ".tsx").
	Extensions() []string

	// Extract parses src (the file at repoRelativePath within repo) and
	// returns every entity and unresolved reference it contains. Refs are
	// intentionally unresolved here — resolution is internal/resolve's job,
	// kept separate so extraction stays parallelizable and cacheable per
	// file (docs/research/02-refs-and-dispositions.md).
	Extract(ctx context.Context, repo, repoRelativePath string, src []byte) (*model.FileFacts, error)
}
