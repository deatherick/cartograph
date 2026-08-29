# ADR-0001: Go core

- **Status**: Accepted
- **Date**: 2026-08-29

## Context

Go, Rust, a Go+Rust hybrid, and TypeScript/Node were evaluated for the core (parser, graph,
resolver, context compiler, daemon).

## Decision

**Go.** Single binary, trivial cross-compile, mature and maintained tree-sitter bindings
(`tree-sitter/go-tree-sitter`), a daemon that's cheap on memory/CPU, and it's the same language
in which Grafel already resolved a huge volume of edge cases in resolution — reimplementing that
knowledge in the same language it was studied in reduces translation risk.

## Consequences

- Concurrency with goroutines/channels fits well with parallel per-file indexing and with the
  watcher.
- No VM runtime, no JVM, no Docker dependency for distribution.
- We lose Rust's static-analysis tooling ecosystem (borrow checker) for the
  Similarity Engine; the risk is accepted — see docs/research for the state of the art that
  already exists in Go via `gonum/graph` and similar.

## Alternatives considered

- **Pure Rust**: maximum control and performance, but rebuilding extractors, resolvers, and
  incremental indexing from scratch significantly lengthens V0.
- **Go+Rust hybrid** (similarity engine via FFI): build/CI complexity from day 1, with no
  clear benefit until the Similarity Engine (Phase 5) exists and is measured.
- **TypeScript/Node**: a single language with the UI, but worse performance on large repos and
  a heavier daemon in memory.
