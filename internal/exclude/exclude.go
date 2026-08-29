// Package exclude implements the exclusion rules a file walk over a real
// repository needs before treating its contents as source: skip dependency/
// build directories, skip machine-generated lockfiles, and skip binary
// content. Grafel's own design documents this need explicitly (§40 of the
// project plan; see docs/research/05-watcher-and-invalidation.md for the
// watcher-side exclusion layers), and cloning a real repo for ctxbench
// (T-2026-08-29, comparing against typescript-node-express-realworld-example-app)
// surfaced a concrete case: that repo's working tree includes a MongoDB
// data directory of binary .wt files that a naive walker reads as text.
//
// This package is intentionally small and dependency-free so both ctxbench
// (today) and the daemon's watcher (Phase 3) can share it — the project's
// standing rule is one implementation behind every consumer, never
// duplicated logic between tools (see docs/adr and the "Restricciones
// permanentes" section of the project plan).
//
// V0 scope: a static directory/file blacklist plus a binary-content sniff.
// Respecting .gitignore is Phase 3 work (it requires real gitignore
// semantics — negations, nested files, glob precedence — which this
// package does not attempt).
package exclude

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// dirNames is the set of directory basenames that are never walked, because
// their content is generated, vendored, or otherwise not something a human
// or agent would explore during a task. Mirrors the plan's §40 list plus
// mongod's on-disk data directory name, encountered in practice.
var dirNames = map[string]bool{
	".git":         true,
	"node_modules": true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"bin":          true,
	"coverage":     true,
	"vendor":       true,
	"generated":    true,
	".next":        true,
	"target":       true,
}

// fileNames is the set of file basenames that are always skipped: they are
// machine-generated, huge by convention, and never part of a legitimate
// exploration or gold set — nobody greps a lockfile while understanding a
// task.
var fileNames = map[string]bool{
	"package-lock.json": true,
	"yarn.lock":         true,
	"pnpm-lock.yaml":     true,
	"Gemfile.lock":       true,
	"poetry.lock":        true,
	"Cargo.lock":         true,
	"go.sum":             true,
}

// binarySniffLen is how many leading bytes of a file are inspected for a NUL
// byte to decide whether it is binary. 8KB is generous enough to catch
// binary formats that pad a text-looking header (a common case for
// database/media formats) without reading the whole file.
const binarySniffLen = 8192

// SkipDir reports whether a directory should never be descended into.
// Also skips any dot-directory (.git, .github, .vscode, …) as a category,
// since none of them hold source a task exploration should read.
func SkipDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	return dirNames[name]
}

// SkipFile reports whether a file should never be read as source, based on
// its name alone (cheap, no I/O).
func SkipFile(name string) bool {
	return fileNames[name]
}

// IsBinary reports whether content looks binary: a NUL byte within the
// first binarySniffLen bytes. This is the same heuristic `file`/git use and
// is enough to catch images, compiled artifacts, and database pages without
// a full content-type registry.
func IsBinary(content []byte) bool {
	n := len(content)
	if n > binarySniffLen {
		n = binarySniffLen
	}
	return bytes.IndexByte(content[:n], 0) != -1
}

// WalkSource walks root, invoking fn for every regular file that is not
// excluded by directory, filename, or binary-content rules. fn receives the
// absolute path and the file's content already read — callers that only
// need a subset of files should filter before doing anything expensive with
// the content, but reading is unavoidable here since binary detection
// requires it.
func WalkSource(root string, fn func(path string, content []byte) error) error {
	return filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != root && SkipDir(info.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if SkipFile(info.Name()) {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if IsBinary(content) {
			return nil
		}
		return fn(path, content)
	})
}
