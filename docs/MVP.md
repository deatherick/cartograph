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
| 3d — Daemon watcher (V0: full reindex on change, not per-file incremental) | ✅ Done (ADR-0012) — `internal/watch` + real `cmd/ctxd`, verified end-to-end |
| 6 — Web UI: integrated Overview (table+detail+graph+impact tabs), navigable graph, git-diff impact | ✅ Done (ADR-0013/0015) — served by `ctxd --web`, React+Vite (reversed from V0's no-build choice) |
| 4 — Impact analysis (`ctx impact`, git-diff-driven blast radius) | ✅ Done (ADR-0014) — unblocks the Web UI's Impact view |
| 5 — Duplicate/similarity engine (Web UI's Duplicates view depends on this) | ⬜ Post-MVP |
| 7-9 — Cross-repo, learned relationships, AI, hardening | ⬜ Post-MVP |

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
- ~~Object/schema-style `const` declarations never extracted as entities~~ — **fixed**
  (edge-case-backlog.md I11): `const User = model('User', UserSchema)` now extracts as `KindClass`,
  module-scope only. Verified: real-repo resolved edges 9→14, `User`/`Article` now findable.
  Measured honestly: this did NOT move the real-repo benchmark's aggregate recall@gold (still
  0.47) — the two zero-recall tasks are route-file/auth concerns, a still-open, separate gap. See
  `docs/benchmarks/2026-08-29-schemaconst-realworld-ts.md`.
- **Real-repo Context Compiler recall gap (0.47 vs 0.85 target) remains open** — not caused by the
  schema-const gap above (that was measured, not assumed). The two zero-recall tasks (pagination-
  limit validation, auth-payload trust across routes) point at something in route-handler
  extraction or seeding, not yet investigated.
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
- Seeding is term-overlap (with camelCase splitting) **plus IDF term weighting** (a rare, specific
  term counts for more than a generic one — `termWeights`), not full BM25/FTS5 — explicitly
  deferred (ADR-0006's search-scope decision). Measured, not assumed: dampened to 40% strength
  after full-strength IDF regressed the synthetic fixture below its exit criterion; the synthetic
  fixture now passes at exactly 0.85 recall (the threshold itself), a thinner margin than before
  this change — worth re-checking the next time seeding is touched. See
  `docs/benchmarks/2026-08-29-idf-seeding.md`.
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
- **Staleness detection exists only while `ctxd` is running for that project** (ADR-0012) — plain
  `ctx index` (no daemon) still has no mtime/content-hash check; running the snapshot stale until
  the next manual `ctx index` in that mode is unchanged from Phase 1.
- Reader uses `os.ReadFile`, not a real `mmap` — a deliberate, documented scoping choice (ADR-0005)
  since no daemon exists yet to make mmap's advantage matter. Format is mmap-ready for later.

### Daemon (`internal/watch`, `cmd/ctxd`) — ADR-0012
- **Full reindex on every change, not true per-file incremental** — a deliberate V0 scoping
  choice (measured: well under a second on this project's own 58-file source), not a small
  optimization deferred casually. Real incremental indexing needs content-hash re-anchoring and
  dependency tracking across the resolver's same-file/same-package/import tiers — genuinely
  harder work, tracked via `docs/research/edge-case-backlog.md`'s `F1`-`F9` cases.
- fsnotify's kqueue/inotify backend costs one descriptor per watched *directory* — fine at any
  scale measured so far, but a real FSEvents binding (per
  `docs/research/05-watcher-and-invalidation.md`'s recommendation) is still the right answer
  before this runs against a repo with thousands of directories.
- No exclusion churn quarantine, no `.git/HEAD` branch-change poller, no crash-reconcile-on-
  restart — `ctxd` still runs in the foreground only. Multi-project watching itself is done
  (ADR-0019: `ctxd <path> [<path>...]`, one goroutine and `opstatus.Tracker` per project); the
  still-open gap is a real `ctxd project add/list` command that adds/removes a project from an
  *already-running* daemon without restarting it. All explicitly deferred, catalogued in
  `docs/research/edge-case-backlog.md`'s `F`/`G` sections.
- `internal/search`'s FTS5/fuzzy layer does not exist — exact and qualified-name lookup (a linear
  scan) cover today's real need; SQLite is deferred until a feature already needs it
  (ADR-0006).
- Session ledger writes are not atomic (unlike snapshot writes) — acceptable since a session
  ledger is advisory state, not correctness-critical (`internal/ledger`'s package doc).

### CLI / UX
- `--file <substring>` disambiguation exists but there is no equivalent for `ctx context` itself
  — a task capsule can't currently be scoped to "only consider files matching X."
  Repo directory naming collisions across two different paths sharing a repo name are handled by
  path hashing (`internal/store.RepoDir`).
- ~~No real multi-project management~~ — **fixed on both sides now**: `internal/project` (`ctx
  project add/list/remove`) is the CLI-only name→path registry (ADR-0016); `ctxd` (ADR-0019) takes
  multiple `<path>` arguments (each also resolved through that same registry) and watches all of
  them concurrently, with `internal/httpserver` and the Web UI scoping every request to a
  `?project=` and offering a live switcher. Still open: a `ctxd project add/list` that adds/removes
  a project from an *already-running* daemon (today the project list is fixed at `ctxd` startup);
  MCP's tools still don't resolve a registered name either (their `root` argument stays "absolute
  path" only).

## Explicitly deferred (post-MVP, tracked not forgotten)

- **C#/Python extraction** (Phase 3b/3c) — Go shipped first (ADR-0010); these two remain
  post-MVP.
- **Daemon + incremental indexing + file watcher** (Phase 3d) — FSEvents on macOS, inotify on
  Linux, content-hash re-anchoring; the watcher exclusion layers (static skip list, `.gitignore`,
  adaptive churn quarantine) are designed in `docs/research/05` but not implemented.
- **SQLite + FTS5 full-text search** (whenever SQLite is introduced for its already-scoped
  purposes — projects, decisions, ledger persistence, metrics).
- **Historical batch validation of impact analysis** (Phase 4 was built, ADR-0014, but its
  original exit criterion — across 20 real historical commits, the proposed test set actually
  contains the tests that commit touched, ≥80% of the time — was not run; needs a real repo with
  meaningful history/coverage to validate against).
- **Duplicate/Similarity Engine** (Phase 5) — the LSH funnel, the duplicate-decision UI concept.
- **Web UI beyond ADR-0013/0015/0019's scope** — entity classification/tagging, pattern
  identification as a first-class surface, filtering as a cross-cutting primitive, a Duplicates
  view (blocked on Phase 5), and a Projects/Settings management page (add/remove a project from the
  UI itself — today only the switcher exists; adding one still means `ctx project add` + restarting
  `ctxd`). Multi-project watching and live updates on reindex are now done (ADR-0019, polling-based,
  not push-based). Full remaining ask in `docs/requirements/phase6-web-ui.md`.
- **Grafel-parity UI surfaces evaluated and explicitly not pursued** — Topology/Links (need
  multi-repo, Phase 7/9), Security/Taint/Dependency-Injection/Error-flow/Infrastructure/GraphQL
  (entire analysis domains never in this project's own scope — Grafel's, not Cartograph's, per
  the master plan), a background-enrichment "Pending" queue (a different processing paradigm than
  `ctxd`'s simple watch-and-reindex). **Paths** (shortest path between two entities) has since been
  built — see item 12 below. **Docs** (rendering `Entity.DocSummary`, a field that exists but no
  extractor populates yet) remains identified as a real, low-cost addition worth revisiting.
- **Cross-repo linking, learned relationships, agent policy files** (Phase 7).
- **Optional AI provider integration, Ask AI** (Phase 8).
- **Hardening, installer, distribution** (Phase 9) — global install (`ctx`/`ctxd` on `PATH`, no
  clone/Go-toolchain required) and the daemon as a persistent system-level service (`launchd`/
  `systemd --user`), with only `.cartograph.json` ever living inside a project directory.
  Requirements captured in full at the user's explicit request:
  [`docs/requirements/phase9-global-install-and-daemon.md`](docs/requirements/phase9-global-install-and-daemon.md).
  Not started.

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
6. ~~Global-install requirements~~ — captured, not built, at the user's explicit request: see
   `docs/requirements/phase9-global-install-and-daemon.md` and ADR-0012.
7. ~~Daemon watcher (Phase 3d V0)~~ — done, ADR-0012: real `cmd/ctxd` indexes once then watches
   and re-indexes automatically on change (full reindex, not yet per-file incremental — a
   measured, documented scoping choice), verified end-to-end against a real fixture (entity count
   moving 4→5 after a live content change with zero manual `ctx index` re-run).
8. ~~Web UI V0~~ — done, ADR-0013: `internal/service.Graph` + `internal/httpserver` (a thin HTTP
   adapter, same "no duplicated logic" rule as MCP), served by `ctxd --web`. Shipped as an
   embedded vanilla-JS frontend (no Node/build step); later reversed once real usage found it
   insufficient — see item 10.
9. ~~Impact analysis (Phase 4)~~ — done, ADR-0014: `internal/store.Upstream` (a new directional,
   incoming-edges-only, unlimited-depth traversal — impact analysis's actual question, distinct
   from `Related`'s bidirectional interactive walk), `internal/gitdiff` (real `git diff` parsing,
   no external library), `ctx impact`/`context_impact` (MCP)/the Web UI's Impact panel, all
   sharing one core (`service.impactFor`). Verified against a real call-chain fixture and a real
   temporary git repo, plus manually against this project's own self-hosted source.
10. ~~Web UI rebuilt on React~~ — done, ADR-0015: moved off the V0 vanilla-JS build (ADR-0013)
    after direct feedback that it looked unpolished next to Grafel's own dashboard and that the
    graph view was static. Reuses real Grafel `webui-v2` UI code (design tokens, primitives,
    layout pattern — MIT, explicitly authorized for the UI layer only; see `NOTICE.md`), rejected
    `@cosmos.gl/graph` after real usage (renders no per-node text, wrong for this project's
    bounded-neighborhood scale) in favor of `@xyflow/react` + dagre, merged Overview/Explore/Graph/
    Impact into one integrated workspace (clickable Kind cards filter a real paginated table;
    selecting a row shows Detail/Graph/Impact as tabs, not separate page navigations), added a
    Tree view alongside Graph (same relationship data, read as text), simplified the standalone
    Impact route to git-diff-only, and fixed three real bugs surfaced by live testing: ambiguous
    names failing with a raw service error instead of a picker, a dropped `file` hint breaking
    disambiguation between linked views, and unbounded breadcrumb-history growth when
    self-navigating an isolated/self-referencing node.
11. Weighted the remaining backlog by effort/value together with the user and started working
    through the highest-value, lowest-effort items:
    - ~~Object/schema-style `const` extraction~~ — done (item above, edge-case-backlog.md I11).
    - ~~Multi-project registry (CLI)~~ — done, ADR-0016: `internal/project` +
      `ctx project add/list/remove`, every command's `<path>` resolves a registered name first.
    - ~~Context Compiler seeding improvement~~ — done: IDF term weighting (dampened to 40%
      strength after measuring full-strength regressed the synthetic fixture below its exit
      criterion). Real repo recall 0.47→0.50; synthetic fixture recall 0.87→0.85 (still passing,
      thinner margin — documented, not hidden). See `docs/benchmarks/2026-08-29-idf-seeding.md`.
12. ~~Paths (shortest path between two entities)~~ — done: `internal/store.ShortestPath`
    (bidirectional BFS over `FanOut`/`FanIn`, same "either direction" semantics as `Related`, with
    parent-pointer path reconstruction), wired through the full stack — `service.Path`, `ctx path
    <path> <fromName> <toName>` (CLI), `context_path` (MCP), `render.Path`. Verified with a real
    3-hop call chain fixture (service→service test) and end-to-end through MCP (mcpserver_test.go).
13. ~~Quality (persisted bug_rate/disposition breakdown)~~ — done, ADR-0017: snapshot format
    bumped to version 2 to add a disposition section; `Snapshot.Files()/Dispositions()/BugRate()`
    read it back with the exact same formula `index.Stats.BugRate()` uses; `ctx stats` and a new
    `context_stats` MCP tool (there was no MCP equivalent of `ctx stats` before this) both surface
    it without requiring a reindex.
14. ~~Operations (`ctxd`'s own watch/reindex status)~~ — done, ADR-0018: `internal/opstatus.Tracker`
    (daemon lifecycle state — started-at, watching, reindex count/reason/stats/error — deliberately
    separate from `internal/service`, which answers "what does the snapshot say" from any process,
    not "is this specific running daemon healthy"), surfaced at `/api/operations` through the
    existing HTTP adapter (`ctxd --web`), 404 when no daemon is behind it. Since surfaced in the
    Web UI too (item 15's TopBar operations badge), so it's no longer CLI/HTTP-only.

This closes every item in the weighted "easy win" batch (Paths, Quality, Operations — ADR-0016/
0017/0018) picked alongside the user in item 11 above.

15. ~~`ctxd` multi-project + a live Web UI project switcher~~ — done, ADR-0019, at the user's
    explicit follow-up request: `ctxd` accepts multiple `<path>` arguments and watches them all
    concurrently (one goroutine + `opstatus.Tracker` each); `internal/httpserver` gained
    `?project=` scoping and `/api/projects`; the Web UI gained a `ProjectProvider`/`useProject()`
    context, a `TopBar` project switcher, and 3-second polling (`usePoll`) for a "live, no manual
    reload" feel while a watched project reindexes. Verified live: `cartograph` (this repo) and
    `ts-basic` registered and watched together, switcher screenshot-confirmed scoping the entity
    table correctly per project, and a real source edit to the watched fixture visibly updating
    the already-open browser tab's entity count with no reload.
16. Next after that, per the deferred list above: C#/Python extractors (Phase 3b/3c), true
    per-file incremental indexing, the similarity/duplicate engine (Phase 5), or the
    global-install/system-service work (Phase 9) — prioritized by real usage feedback, not by
    continuing to iterate against one synthetic fixture.
