# MVP definition and roadmap

Single source of truth for "what does shipping v0.1 actually require," consolidating findings
scattered across 7 ADRs, `docs/research/edge-case-backlog.md` (85 entries), 4 benchmark reports,
and inline code comments. Written to stop open-ended iteration: every item below is either **in
MVP scope** (blocks shipping), **explicitly deferred** (documented, not silently dropped), or
**already done**. When in doubt about whether to build something next, this file is the answer.

## Status as of 2026-08-29

| Phase | Status |
|---|---|
| 0a — Discovery on Grafel | ✅ Done |
| 0b — Foundations, `ctxbench` | ✅ Done |
| 1 — TypeScript static map | ✅ Done (ADR-0004/0005/0006) |
| 2 — Context Compiler + MCP | ✅ Done — Context Compiler (ADR-0007), MCP server (ADR-0008), live agent demo (ADR-0009), README quickstart. **MVP shipped.** |
| 3a — Go extractor + self-hosting | ✅ Done (ADR-0010) — 0.1% bug_rate indexing this project's own real Go source |
| 3a′ — Plug-and-play language architecture, init wizard | ✅ Done (ADR-0011) — `LanguagePolicy` interface, `.cartograph.json`, `ctx init`/`ctx languages` |
| 3b/3c — C#, Python extractors | ⬜ Post-MVP (now a drop-in addition per ADR-0011, not a core rewrite) |
| 3d — Daemon, incremental indexing, file watcher | ⬜ Post-MVP |
| 4-9 — Impact analysis, duplicates, Web UI, cross-repo, AI, hardening | ⬜ Post-MVP |

## What "MVP" means for this project

Decided this session, to stop scope creep: **the MVP is a local TypeScript-only Context Engine,
usable by both a human (CLI) and a coding agent (MCP), that measurably beats grep+read on token
cost without losing recall.** Everything else the master plan eventually wants — other
languages, a daemon, a web UI, duplicate detection, impact analysis, cross-repo linking, learned
relationships, optional AI features — is real, wanted, and **not required to ship v0.1**.

### Why MCP is in MVP scope, not deferred

The project's own framing (master plan) is "Local Agent Context Manager" — a tool whose primary
consumer is a coding agent, not a human typing CLI commands. Today there is **no agent-facing
interface at all**: `ctx context` only exists as a CLI subcommand. Shipping an MVP that cannot
actually be used by the agent it was built for would not deliver on the project's own stated
purpose. MCP wiring (`context_compile`, `context_find`, `context_related`, `context_inspect`,
`context_source` — the minimal tool set the master plan names) is therefore **the one piece of
Phase 2 still required for MVP**, not a nice-to-have.

## MVP Definition of Done

- [x] Deterministic TypeScript extraction via tree-sitter queries (classes, interfaces,
      functions, methods, enums, type aliases, prototype-assignment methods, heritage, ESM+CJS
      imports, re-exports, test detection) — ADR-0004, ADR-0006
- [x] Resolver pipeline (same-file → import-table → receiver-type → bare-name allowlist →
      disposition), tsconfig paths/baseUrl, barrel-following — ADR-0006
- [x] `bug_rate` metric, measured and CI-gated (0.0%–1.1% on real validation) — ADR-0006
- [x] Formal precision measurement against an annotated fixture (100% on a 13-entry checklist,
      exceeding the 95% target) — ADR-0006, `internal/index/precision_test.go`
- [x] Binary snapshot persistence, sub-100ms reads — ADR-0005
- [x] CLI: `index`, `find`, `inspect`, `related`, `source`, `stats`, `context`
- [x] Context Compiler: ranker + real knapsack budgeter + Context Ledger, meeting its exit
      criterion on the project's own benchmark (70.7% reduction, 0.87 recall@gold) — ADR-0007
- [x] **MCP server** (`internal/mcpserver`, `cmd/ctxmcp`) exposing `context_index`,
      `context_compile`, `context_find`, `context_related`, `context_inspect`, `context_source`
      over stdio via the official `modelcontextprotocol/go-sdk`, so an agent can use this
      directly instead of only a human via CLI — ADR-0008. Verified two ways: in-memory
      transport tests (7 tests, including the Context Ledger's dedup working through MCP) and a
      real subprocess test spawning the actual `bin/ctxmcp` binary via `CommandTransport`.
- [x] **One end-to-end live demo**: a real coding agent (headless Claude Code) connected via MCP
      resolved a real task from `fixtures/tasks/realworld-ts.json` against the real-repo
      validation clone — ADR-0009. Found and fixed a real bug along the way
      (`context_find`/`context_related` failed MCP schema validation with `Out=any` returning a
      bare slice — neither was caught by this project's own tests before real usage). After the
      fix: zero raw grep/bash/read calls (vs 6 in the no-MCP baseline), no subagent delegation
      needed, -55.5% real dollar cost, same correct answer.
- [x] A short `README.md` quickstart a new user/contributor can follow without reading every ADR
      — install/prerequisites, a zero-setup Quickstart against the vendored `fixtures/ts-basic`
      with real verified command output (not fabricated), CLI usage, MCP usage with a working
      `.mcp.json` example, a Known limitations section, and a documentation map. Every command
      shown was actually run against a clean-room build (`rm -rf bin ~/.cartograph && make
      build`) to confirm it works exactly as written.

**All Definition of Done items are complete. The MVP has shipped.**

## Consolidated known issues (not blocking MVP, but should not be forgotten)

Organized by area, pulled from every ADR and code comment written so far — this is the "don't
rediscover this later" list.

### Extraction (`internal/parser/ts`)
- **Object/schema-style `const` declarations are never extracted as entities**
  (`const User = model('User', UserSchema)`) — edge-case-backlog.md I11. Measured root cause of
  the real-repo Context Compiler recall gap (0.47 vs 0.85 target). Highest-value single fix for
  real-code recall; extends the existing `methodassign` pattern.
- `Entity.Signature` and `Entity.DocSummary` are never populated — the source ladder's
  signature/skeleton rungs read the first source line as a stand-in (`internal/compile`'s
  package doc). A real reconstructed signature string is better long-term.
- Destructured CJS require with renaming (`const { a: renamed } = require(...)`) — only the
  shorthand form is handled (`internal/parser/ts/extractor.go`).
- tsconfig `extends` (config inheritance) and JSONC (comments/trailing commas) are not handled —
  a malformed/unsupported tsconfig is skipped, not guessed at (`internal/index/tsconfig.go`).
- Nested calls inside a test callback (`it('...', () => { ... })`) are not attributed to the test
  entity as `Src` — would need arrow-function callbacks registered as scopes, matching
  `methodassign`'s existing pattern (`internal/parser/ts/extractor.go`).
- No export-awareness — every top-level entity is treated as visible/exported; a private helper
  with the same name as a real export in the same file is a false-resolve risk
  (`internal/resolve/resolve.go`).

### Resolution (`internal/resolve`)
- tsconfig path aliases only support single-wildcard patterns (`"@/*": ["src/*"]`) — multi-segment
  or regex-like patterns are unsupported.

### Language plugin architecture (`internal/resolve`, `internal/index`) — ADR-0011
- "Plug and play" means architecturally decoupled and independently addable at the source level
  (one `LanguagePolicy` file per language, registered in one list) — NOT a runtime-loadable
  plugin mechanism (dynamic `.so`/RPC plugins). Every language still ships compiled into the
  `ctx` binary. Deliberately out of scope until a real need appears.
- `.cartograph.json` has no schema versioning or unknown-field preservation — a hand-edited typo
  in `languages` is silently ignored (falls out of `enabledLanguages`'s name-matching), not
  rejected with an error. Acceptable today (one field, low stakes); revisit once the file grows.
- `ctx init`'s non-interactive-stdin detection (`isInteractiveStdin`) is a heuristic
  (`os.Stdin`'s `ModeCharDevice` bit) — could misfire in an unusual terminal emulator; the
  fallback behavior (enable every detected language, logged loudly) is always safe even if the
  heuristic is wrong, so this has not needed a fix.

### Extraction and resolution (`internal/parser/golang`) — ADR-0010
Measured at 0.1% bug_rate self-hosting on this project's own real Go source (54 files, 393
entities, 1,916 dispositions) — these are the documented gaps behind that number, not blockers:
- A local variable's function type inferred from a multi-return call's second value (`ctx, cancel
  := context.WithTimeout(...)`) is not detected — only a syntactic func literal or a func-typed
  annotation is. The one remaining bug-extractor case in the self-hosting measurement.
- Struct fields typed from another package via `pkg.Type` (`repo *pkg.UserRepo`) produce no
  receiver-type signal — only bare `type_identifier` fields do. A call through such a field
  resolves to `DispositionUnimplemented`, not a wrong answer.
- No export-awareness — same gap as TypeScript's resolver, not fixed twice.
- Go's implicit interface satisfaction (no `implements` keyword) is a **permanent** gap: this
  extractor never emits `RefImplements` for Go — detecting it needs real type-checking, not
  tree-sitter queries.
- An import's local identifier, when no alias is written, is approximated as the import path's
  last path segment — wrong only for the rare package whose declared name differs from its
  directory name.
- Dot imports (`. "pkg"`) are not resolved — a rare, discouraged Go idiom.

### Context Compiler (`internal/compile`)
- Seeding is crude term-overlap (with camelCase splitting), not BM25/FTS5 — explicitly deferred
  (ADR-0006's search-scope decision), a real ranking function is the natural next refinement.
- No centrality/PageRank term in scoring — `internal/graph`'s package doc already defers this;
  it would need pre-baked attributes at index time (a real, contained addition once useful).
- No git-recency term — no git-metadata extraction exists yet (Phase 4 scope).
- The budgeter assigns each entity ONE natural rung up front (primary→skeleton,
  related→signature) rather than optimizing rung-per-item within the knapsack — a real
  multi-rung optimization is a documented next refinement, not attempted in the V0 slice.
- The Context Ledger's own multi-call token savings are unit-tested but not measured by
  `ctxbench` — each benchmark task compiles with no session, by design; a multi-call session
  benchmark is a separate, not-yet-built measurement.
- `relevanceFloorRatio`/`defaultMaxSeeds`/`defaultMaxDepth` were tuned against ONE synthetic
  fixture and validated to generalize poorly to real, sparse graphs (ADR-0007) — not a bug, but a
  known limitation: these constants may need per-repo-density awareness eventually, not global
  constants.

### Persistence (`internal/store`, `internal/ledger`)
- No staleness detection — if source changes after `ctx index` ran, every read command silently
  serves the stale snapshot. No mtime/content-hash check exists yet (explicitly Phase 3: the
  watcher, incremental indexing).
- Reader uses `os.ReadFile`, not a real `mmap` — a deliberate, documented scoping choice (ADR-0005)
  since no daemon exists yet to make mmap's advantage matter. Format is mmap-ready for later.
- `internal/search`'s FTS5/fuzzy layer does not exist — exact and qualified-name lookup (a linear
  scan) cover today's real need; SQLite is deferred until a feature already needs it
  (ADR-0006).
- Session ledger writes are not atomic (unlike snapshot writes) — acceptable since a session
  ledger is advisory state, not correctness-critical (`internal/ledger`'s package doc).

### CLI / UX
- `--file <substring>` disambiguation exists but there is no equivalent for `ctx context` itself
  — a task capsule can't currently be scoped to "only consider files matching X."
  Repo directory naming collisions across two different paths sharing a repo name are handled by
  path hashing (`internal/store.RepoDir`), but there is still no real multi-project management
  (`ctx project add/list/remove`, named in the master plan) — every command takes a raw path.

## Explicitly deferred (post-MVP, tracked not forgotten)

- **C#/Python extraction** (Phase 3b/3c) — Go shipped first (ADR-0010); these two remain
  post-MVP.
- **Daemon + incremental indexing + file watcher** (Phase 3d) — FSEvents on macOS, inotify on
  Linux, content-hash re-anchoring; the watcher exclusion layers (static skip list, `.gitignore`,
  adaptive churn quarantine) are designed in `docs/research/05` but not implemented.
- **SQLite + FTS5 full-text search** (whenever SQLite is introduced for its already-scoped
  purposes — projects, decisions, ledger persistence, metrics).
- **Impact analysis + git awareness** (Phase 4) — `ctx impact`, git-diff-driven blast radius.
- **Duplicate/Similarity Engine** (Phase 5) — the LSH funnel, the duplicate-decision UI concept.
- **Web UI** (Phase 6) — requirements already captured in `docs/requirements/phase6-web-ui.md`
  (visual graph, entity classification/tagging, pattern identification, quantification, filtering
  as a cross-cutting primitive).
- **Cross-repo linking, learned relationships, agent policy files** (Phase 7).
- **Optional AI provider integration, Ask AI** (Phase 8).
- **Hardening, installer, distribution** (Phase 9).

## Immediate next steps, in order

1. ~~MCP server~~ — done, ADR-0008.
2. ~~Live demo~~ — done, ADR-0009. Found and fixed a real schema-validation bug in
   `context_find`/`context_related` along the way.
3. ~~README quickstart~~ — done. A new user/contributor can clone, build, index a repo, and run
   one query without reading 8 ADRs first.
4. ~~MVP shipped~~. ~~Go extractor + self-hosting~~ — done, ADR-0010: 0.1% bug_rate indexing this
   project's own real source, `ctx find`/`inspect`/`related`/`context` all verified working
   against it, including a cross-language `context` capsule (Go + TypeScript results together for
   one task).
5. ~~Plug-and-play language architecture~~ — done, ADR-0011: `LanguagePolicy` interface (no
   per-language branching in `internal/resolve`'s core pipeline, verified by a dedicated
   architecture test), `.cartograph.json` for opt-in/opt-out language selection, and `ctx
   init`/`ctx languages` as the wizard/status commands. Verified a disabled language is never
   even walked, not merely filtered from output.
6. Next up, per the deferred list above: C#/Python extractors (Phase 3b/3c — now a drop-in
   addition, not a core rewrite), then the daemon + incremental indexing + file watcher
   (Phase 3d) — prioritized by real usage feedback, not by continuing to iterate on ranker
   constants against one synthetic fixture.
