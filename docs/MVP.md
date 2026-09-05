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
| 3b — C# extractor | ✅ Done (ADR-0023) — 0.0% bug_rate on a real repo (eShopOnWeb, 254 files) |
| 3c — Python extractor | ✅ Done (ADR-0024) — 0.0% bug_rate on a real repo (django-realworld-example-app, 44 files); real-repo ctxbench passes the exit criterion (88.9% reduction, 0.86 recall) |
| 3d — Daemon watcher (V0: full reindex on change, not per-file incremental) | ✅ Done (ADR-0012) — `internal/watch` + real `cmd/ctxd`, verified end-to-end |
| 6 — Web UI: integrated Overview (table+detail+graph+impact+duplicates tabs), navigable graph, git-diff impact | ✅ Done (ADR-0013/0015/0027) — served by `ctxd --web`, React+Vite (reversed from V0's no-build choice) |
| 4 — Impact analysis (`ctx impact`, git-diff-driven blast radius) | ✅ Done (ADR-0014) — unblocks the Web UI's Impact view |
| 5 — Duplicate/similarity engine V0 + identifier normalization + Web UI view | ✅ Done (ADR-0021/ADR-0025/ADR-0027) — 1.00 precision / 0.83 recall on the 24-pair labeled eval; AST tree-edit distance and a ≥120-pair eval set remain open |
| 7-8 — Cross-repo, learned relationships, AI | ⬜ Post-MVP |
| 9 — Hardening, installer, distribution | ✅ Done (ADR-0026), **fully live-verified**: real `v0.1.0` release, real `install.sh` download, real `ctx service install` running as a system service with real registered projects |

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
- ~~Route-handler extraction gap (the two real-repo zero-recall tasks)~~ — **investigated and
  partially fixed**, ADR-0022: those tasks' gold files (Express route files) were producing ZERO
  entities at all — every route handler is an anonymous callback with no declared name, a query
  pattern this extractor had no case for. Now extracted as real `KindFunction` entities, name
  synthesized from HTTP verb+path plus every `req.<x>.<field>` the handler reads. One of the two
  tasks (R10) now passes; real-repo average recall@gold moved 0.50 -> 0.62. The other (R07) is
  still open — its gold entity now exists and scores correctly, but doesn't reach the ranker's
  default top-5 seeds; two ranker-side fixes were built, measured, and explicitly REJECTED because
  each regressed the synthetic fixture's recall below its own exit criterion (see ADR-0022 and
  `docs/benchmarks/2026-08-29-route-handler-extraction.md` for the full account — a real ranking
  function, not another patch to substring matching, is what R07 actually needs).
- **Real-repo Context Compiler recall gap (0.62 vs 0.85 target) remains open** — see above; this
  is now understood as a seeding/ranking limitation (substring matching on common words), not an
  extraction gap.
- `Entity.Signature` and `Entity.DocSummary` are never populated — the source ladder's
  signature/skeleton rungs read the first source line as a stand-in (`internal/compile`'s
  package doc). A real reconstructed signature string is better long-term.
- ~~Destructured CJS require with renaming (`const { a: renamed } = require(...)`) — only the
  shorthand form is handled~~ **closed**: the renaming form (`pair_pattern`) is now a dedicated
  query pattern (`internal/parser/ts/queries/entities.scm`'s `import.cjs.stmt3`).
- ~~tsconfig `extends` (config inheritance) ... not handled~~ **closed**: `loadTSConfig` now
  follows a relative-path `extends` chain (cycle-guarded), merging per tsconfig's own
  compilerOptions-level override semantics — a child's own `baseUrl`/`paths` replace the parent's
  wholesale when present, otherwise the parent's are inherited (`internal/index/tsconfig.go`).
  JSONC (comments/trailing commas) and a package-specifier `extends` target (needing
  node_modules resolution) remain unsupported — a malformed/unsupported tsconfig is skipped, not
  guessed at.
- ~~Nested calls inside a test callback ... are not attributed to the test entity as `Src`~~
  **closed**: the test query now captures the trailing callback (`test.callback`), registered in
  `scopeByStartByte` the same way `methodassign`'s does; `enclosingScope` now also walks through
  `arrow_function` (`internal/parser/ts/extractor.go`).
- No export-awareness — every top-level entity is treated as visible/exported; a private helper
  with the same name as a real export in the same file is a false-resolve risk
  (`internal/resolve/resolve.go`). Deliberately deferred: closing it properly across all four
  languages (Go's own exported-vs-unexported casing aside) is a bigger, riskier change than fits
  this pass — flagged here rather than attempted piecemeal.

### Resolution (`internal/resolve`)
- tsconfig path aliases only support single-wildcard patterns (`"@/*": ["src/*"]`) — this matches
  tsconfig's own specification (at most one `*` per pattern is valid there too), so this is not
  actually a gap relative to real-world tsconfig files, just a note that a malformed
  multi-wildcard/regex-like pattern in `paths` is ignored rather than guessed at.

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
  **Deliberately left open**: closing it correctly needs the REAL declared return type of
  whatever function is being called; for a std-lib/external call (like `context.WithTimeout`)
  that means either full type-checking (out of scope for a tree-sitter-query extractor) or a
  hand-maintained table of known external functions' return types — the latter is itself a form
  of the guessing this project's own anti-inference discipline rejects (a hardcoded fact about
  ONE external function, not a general rule), so it's left as a documented gap rather than
  patched with a special case.
- ~~Struct fields typed from another package via `pkg.Type` ... produce no receiver-type
  signal~~ **closed**: a named field's qualified type (`repo *pkg.UserRepo`) is now captured
  (`field.decl.qualified`/`field.decl.qualified.ptr`) and stored as the compound string
  `"pkgAlias.TypeName"`; the resolver's `FollowImportToMethods` (`internal/resolve/lang_go.go`)
  splits it, resolves the alias through the file's own import table to a directory, and looks the
  method up there — exact matches only, same discipline as every other tier. An anonymous
  (embedded) field of a qualified type is deliberately NOT matched by the same query (kept
  `field.name` required, not optional) — that shape stays an unhandled gap rather than risk a
  wrongly-scoped `RefExtends` target.
- No export-awareness — same gap as TypeScript's resolver, not fixed twice.
- Go's implicit interface satisfaction (no `implements` keyword) is a **permanent** gap: this
  extractor never emits `RefImplements` for Go — detecting it needs real type-checking, not
  tree-sitter queries.
- An import's local identifier, when no alias is written, is approximated as the import path's
  last path segment — wrong only for the rare package whose declared name differs from its
  directory name. **Deliberately left open**: a correct fix needs the target package's own
  ACTUAL declared name, which is only knowable for an internal (same-module) import by reading
  one of that directory's already-indexed files — a cross-file, index-level correction this
  extractor's per-file `Extract` has no access to. Attempted only as a bounded, additive fix
  during this pass; deferred rather than risk a bigger resolver-pipeline change for a case this
  project's own self-hosting measurement has never actually hit.
- Dot imports (`. "pkg"`) are not resolved — a rare, discouraged Go idiom. Left as a **permanent**
  gap: `go vet`/linting conventions actively discourage this form in real code, so the ROI of
  supporting it is low relative to the resolver-pipeline surface it would touch.

### Extraction and resolution (`internal/parser/csharp`) — ADR-0023
Measured at 0.0% bug_rate on a real repo (eShopOnWeb, 254 files, 777 entities, 337 resolved
edges) — these are the documented gaps behind that number, not blockers:
- ~~No xUnit/NUnit/MSTest test detection~~ **closed**: a method carrying a recognized test
  attribute (`[Fact]`/`[Theory]`/`[Test]`/`[TestMethod]`, bare or namespace-qualified) is now
  reclassified as `KindTest` (`test.methodnode` query pattern + `isTestAttribute`'s exact
  allowlist match, `internal/parser/csharp/extractor.go`) — verified live against eShopOnWeb
  (`ReturnsHomePageWithProductListing` now resolves as `Test`, not `Method`). ASP.NET
  routing-attribute extraction (`[HttpGet]`/`[Route]`) reuses the same attribute-parsing
  machinery but is a distinct follow-up, not done here — it needs the route's own path/verb
  captured from the attribute's arguments, not just its name.
- ~~No extension-method resolution~~ **closed**: a method's `this` parameter modifier (`public
  static T Foo(this ExtendedType x, ...)`) is now captured as a `model.ExtensionMethod` fact
  (`ext.methodnode` query pattern + `isThisModifier`'s exact check) and indexed by the type it
  extends, NOT its own declaring class; `FollowImportToMethods` (`internal/resolve/lang_csharp.go`)
  resolves it only through the same in-scope file set `SameScopeFiles` already computes (same
  directory + `using`-resolved directories) — matching real C#'s own visibility rule, and only
  after an ordinary same-class method already failed to match (an instance method always wins
  over a same-named extension method, verified by a dedicated test). Scoped to a custom
  (locally-declared) `ExtendedType` only — a built-in type (`this string s`) parses as
  `predefined_type`, not `identifier`/`generic_name`, and is deliberately not matched, since this
  project has no locally-indexed entity for `string`/`int`/etc. to resolve to anyway. Not
  measurably exercised by eShopOnWeb (every extension method there targets an external framework
  type — `IServiceCollection`, `IUrlHelper`, `string`, ...), but unit- and resolver-tested against
  a synthetic same-repo case.
- No partial-class support — a class split across multiple files (rare, but real) has its
  fields/methods indexed per-file, not merged; `fieldTypesByOwner`/`methodsByOwner` only see
  whichever file's own declarations they were built from.
- Qualified names are DIRECTORY-scoped, never derived from a file's own `namespace`/file-scoped
  namespace declaration — same approximation Go's extractor already documents (ADR-0010); a
  namespace that diverges from its folder is a known gap, not a new risk.
- **Two deliberate anti-inference guards, chosen by the user over a higher-recall alternative**:
  a qualified call's receiver only resolves from a real type annotation, never a
  capitalization-based guess; a `using` directive only maps to a directory on an EXACT namespace
  match against a known `.csproj`'s root namespace, never a partial/suffix match. Both trade
  recall for zero false-resolution risk — see ADR-0023's "what was tried and rejected" section.
- ~~`.csproj` `<ProjectReference>` edges are not read~~ **closed**: `loadCSharpProjects` now reads
  every `<ProjectReference>` (`internal/index/csproj.go`), and a `using` directive only crosses
  into ANOTHER project's directory when the calling file's own project can reach it — directly OR
  transitively (real MSBuild semantics: A referencing B referencing C means A can use C's public
  types too) — via a real reference chain (`lang_csharp.go`'s `resolveImportPath`/
  `transitivelyReferences`), never merely because the namespace happens to exist somewhere in the
  repo. A direct-only check was tried first and measurably regressed eShopOnWeb (337 -> 329
  resolved edges — IntegrationTests reaches ApplicationCore only via UnitTests, never a direct
  reference of its own); the transitive version restores the baseline exactly (337 resolved
  edges, 0.0% bug_rate) while still correctly blocking a namespace with no real reference path at
  all (verified by a dedicated test). A file this index cannot attribute to any known project
  (an edge case) falls back to the old permissive behavior rather than risk denying a legitimate
  same-project resolution it simply couldn't verify.
- Real-repo Context Compiler recall (0.65 vs 0.85 target, `fixtures/tasks/eshoponweb.json`)
  remains open — a seeding/ranking limitation (spot-checked: both gold entities of one
  zero-recall task are correctly extracted and findable), the same category ADR-0022 already
  documented and left open for TypeScript, not chased again here.

### Extraction and resolution (`internal/parser/python`) — ADR-0024
Measured at 0.0% bug_rate on a real repo (django-realworld-example-app, 44 files, 112 entities,
21 resolved edges) — these are the documented gaps behind that number, not blockers:
- ~~No re-export awareness~~ **closed**: `findExportedEntity` now chases through an
  `__init__.py`'s own import table (depth-limited, cycle-safe, mirroring TypeScript's barrel
  re-export chasing) — Python has no explicit re-export syntax, but an ordinary
  `from .models import Name` inside `__init__.py` genuinely does put `Name` in that package's own
  namespace, so this isn't a convention being guessed at, just real Python semantics. Deliberately
  scoped to `__init__.py` files only, not chased through every ordinary module's own imports
  (which real Python namespace rules technically also allow) — narrower than the language permits,
  to avoid surfacing an unrelated same-named import as a false resolve. Verified unchanged (0.0%
  bug_rate, 21 resolved edges; 0.86 average recall@gold) against django-realworld-example-app —
  still not exercised by that repo, only unit/resolver-tested.
- No three-level unaliased namespace chain (`import x.y.z` then `x.y.z.member()`) — Python binds
  only the top segment; the chain past that isn't chased, the same bounded scope C#'s
  `Guard.Against.Null` gap already accepts.
- ~~`self.field = some_parameter` ... gives no receiver-type signal~~ **closed**: a new query
  pattern (`receiver.fieldfromparam`) captures the assignment; the Go side cross-references the
  assigned identifier against the SAME enclosing function's own typed parameters
  (`paramTypesByFunc`, keyed by function start byte so two different methods' same-named
  parameter never collide) — silently produces no signal when the parameter has no PEP 484 hint,
  never guessed from its bare name. Verified unchanged (0.0% bug_rate, 0.86 recall) against
  django-realworld-example-app.
- Decorators that rename or replace their target at runtime (`functools.wraps`-based wrappers,
  Django's `@receiver`) are undetectable via syntax alone — a permanent gap in the same category
  as Go's implicit interface satisfaction.
- A `src`-layout repo (on-disk root ≠ importable package root) won't resolve absolute imports —
  never guessed at with a "src/" prefix fallback, the same "exact match only" discipline ADR-0023
  established for C#.
- Real-repo Context Compiler recall (0.86, `fixtures/tasks/django-realworld.json`) **passes** the
  0.85 exit criterion on the first real-repo measurement — an outlier compared to every prior
  language's own first pass (TS/C# both needed extraction fixes or still fell short); attributed to
  this repo's own short, vocabulary-rich call chains, not a Context Compiler change (untouched).

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

### Daemon (`internal/watch`, `cmd/ctxd`) — ADR-0012, ADR-0020
- ~~Full reindex on every change, not true per-file incremental~~ — **fixed**, ADR-0020:
  `internal/index.Indexer` re-processes exactly the changed files plus everything
  `resolve.Index.Dependents` finds could be affected (importers, barrel re-exporters, same-package
  siblings), not the whole repo. Measured live against this project's own real, self-hosted source:
  a single-file rename with one cross-file dependent completed in ~4ms, against ~960ms for a full
  reindex of the same 105-file repo. The F1-F9 edge cases `docs/research/edge-case-backlog.md`
  catalogued are addressed one by one in ADR-0020's own verification section — F6 (a real
  `.git/HEAD` branch-change poller) remains explicitly out of scope, and F4 (file rename) is
  correct but not zero-cost (see below).
- `Dependents` is a linear scan over every registered file's own import table, not a maintained
  reverse index — fast at today's scale; a real reverse index is a legitimate future optimization
  once profiling (not intuition) says this scan is the bottleneck.
- A pure file rename (unchanged content, new path) is handled correctly — the same entity ID
  reappears under the new path, since `EntityID` excludes file by design — but costs one file's
  worth of re-extraction rather than a true zero-cost re-anchor. A real content-hash-keyed rename
  shortcut (matching an old path's removed content hash against a new path's added one, skipping
  re-extraction entirely) is a known, honestly-reported optimization, not built.
- fsnotify's kqueue/inotify backend costs one descriptor per watched *directory* — fine at any
  scale measured so far, but a real FSEvents binding (per
  `docs/research/05-watcher-and-invalidation.md`'s recommendation) is still the right answer
  before this runs against a repo with thousands of directories.
- No exclusion churn quarantine, no `.git/HEAD` branch-change poller, no crash-reconcile-on-
  restart needed (ADR-0020: `ctxd` always runs a fresh full index at startup, so a restart after
  downtime naturally reconciles). Multi-project watching itself is done (ADR-0019: `ctxd <path>
  [<path>...]`, one goroutine, `opstatus.Tracker`, and one live `Indexer` per project). ~~the
  still-open gap is a real `ctxd project add/list` command that adds/removes a project from an
  *already-running* daemon without restarting it~~ **stale — this was actually closed by
  ADR-0026 (Phase 9)**: `ctxd` with no arguments watches every project in
  `~/.cartograph/projects.json` and reconciles against it every `registryPollInterval`
  (`cmd/ctxd/main.go`'s `reconcile`), so `ctx project add`/`remove` takes effect on the running
  system-service daemon with no restart — this bullet just never got updated when that landed.
  All remaining items here are explicitly deferred, catalogued in
  `docs/research/edge-case-backlog.md`'s `F`/`G` sections.
- `internal/search`'s FTS5/fuzzy layer does not exist — exact and qualified-name lookup (a linear
  scan) cover today's real need; SQLite is deferred until a feature already needs it
  (ADR-0006).
- Session ledger writes are not atomic (unlike snapshot writes) — acceptable since a session
  ledger is advisory state, not correctness-critical (`internal/ledger`'s package doc).

### CLI / UX
- ~~`--file <substring>` disambiguation exists but there is no equivalent for `ctx context`
  itself~~ **stale — closed in the known-issues sweep**: `ctx context <path> "<task>" [--budget N]
  [--session ID] [--file <substring>]` and the `context_index`/`compile` MCP tool's `file` arg
  both scope Context Compiler SEEDING to matching files (graph expansion stays unrestricted,
  deliberately — see `Options.FileFilter`'s doc, `internal/compile/compile.go`). This bullet just
  never got updated when that landed.
  Repo directory naming collisions across two different paths sharing a repo name are handled by
  path hashing (`internal/store.RepoDir`).
- ~~No real multi-project management~~ — **fixed on both sides now**: `internal/project` (`ctx
  project add/list/remove`) is the CLI-only name→path registry (ADR-0016); `ctxd` (ADR-0019) takes
  multiple `<path>` arguments (each also resolved through that same registry) and watches all of
  them concurrently, with `internal/httpserver` and the Web UI scoping every request to a
  `?project=` and offering a live switcher. ~~Still open: a `ctxd project add/list` that
  adds/removes a project from an *already-running* daemon ... MCP's tools still don't resolve a
  registered name either~~ **both stale — both closed since**: `ctxd`'s zero-argument mode
  reconciles against `~/.cartograph/projects.json` with no restart (ADR-0026, Phase 9), and every
  MCP tool's `root` argument now resolves a registered project name via `project.Resolve`
  (`internal/mcpserver/mcpserver.go`, closed in the known-issues sweep). These bullets just never
  got updated when each landed.

## Explicitly deferred (post-MVP, tracked not forgotten)

- **Daemon + file watcher remaining hardening** (Phase 3d) — true per-file incremental indexing is
  now done (ADR-0020); FSEvents on macOS (still fsnotify's kqueue), the watcher exclusion layers
  beyond the static skip list/`.gitignore` (adaptive churn quarantine), and a `.git/HEAD` branch-
  change poller are designed in `docs/research/05` but not implemented.
- **SQLite + FTS5 full-text search** (whenever SQLite is introduced for its already-scoped
  purposes — projects, decisions, ledger persistence, metrics).
- **Historical batch validation of impact analysis** (Phase 4 was built, ADR-0014, but its
  original exit criterion — across 20 real historical commits, the proposed test set actually
  contains the tests that commit touched, ≥80% of the time — was not run; needs a real repo with
  meaningful history/coverage to validate against).
- ~~Duplicate/Similarity Engine~~ (Phase 5) — **V0 done**, ADR-0021, **identifier normalization
  added**, ADR-0025: `internal/similar`'s real funnel (exact fingerprint -> MinHash+LSH candidate
  generation -> structural+behavioral scoring), `ctx similar/duplicates/decide` (CLI),
  `context_similar/duplicates/decide` (MCP), decision persistence. Still honestly narrower than the
  full ask: no L2 bounded AST tree-edit distance (token-shingle Jaccard stands in), and the labeled
  eval set is 24 pairs, not the master plan's ≥120 — but renamed-identifier normalization (ADR-0021's
  originally-named gap) is now closed: measured precision 1.00 / recall **0.83** (up from 0.50) on
  that same 24-pair set, reported plainly. ~~The duplicate-decision UI panel~~ is **done, ADR-0027**
  — see the Web UI item below.
- **Web UI beyond ADR-0013/0015/0019's scope** — entity classification/tagging, pattern
  identification as a first-class surface, filtering as a cross-cutting primitive, and a
  Projects/Settings management page (add/remove a project from the UI itself — today only the
  switcher exists; adding a project no longer needs restarting `ctxd` since ADR-0026's zero-argument
  daemon mode, but there's still no UI button for it, only `ctx project add`). ~~A Duplicates
  view~~ is **done, ADR-0027**: `/duplicates`, every score fully decomposed
  (Exact/Structural/Behavioral/Overall, never one opaque number), a decision recorded from the
  browser via the same `internal/service.Decide` the CLI calls — a pair decided from either never
  resurfaces in the other. Found and fixed a real, pre-existing bug along the way: every
  client-side route (`/graph`, `/impact`, and now `/duplicates`) 404'd on a direct page load —
  only ever masked because every route was previously reached by clicking through from `/`, never
  a fresh load or a bookmark. Multi-project watching and live updates on reindex are now done
  (ADR-0019, polling-based, not push-based). Full remaining ask in
  `docs/requirements/phase6-web-ui.md`.
- **Grafel-parity UI surfaces evaluated and explicitly not pursued** — Topology/Links (need
  multi-repo, Phase 7/9), Security/Taint/Dependency-Injection/Error-flow/Infrastructure/GraphQL
  (entire analysis domains never in this project's own scope — Grafel's, not Cartograph's, per
  the master plan), a background-enrichment "Pending" queue (a different processing paradigm than
  `ctxd`'s simple watch-and-reindex). **Paths** (shortest path between two entities) has since been
  built — see item 12 below. **Docs** (rendering `Entity.DocSummary`, a field that exists but no
  extractor populates yet) remains identified as a real, low-cost addition worth revisiting.
- **Cross-repo linking, learned relationships, agent policy files** (Phase 7).
- **Optional AI provider integration, Ask AI** (Phase 8).
- ~~**Hardening, installer, distribution**~~ (Phase 9) — **done, ADR-0026**: `ctxd` with no
  arguments watches every project in `~/.cartograph/projects.json` and reconciles against it every
  5s (add/remove/restart a project with no daemon restart — verified live, not just read from the
  code); `ctx service install/uninstall/status` registers `ctxd` as a real `launchd` user agent
  (macOS) or `systemd --user` unit (Linux), one native mechanism per OS in its own build-tag file
  (`internal/sysservice`); `install.sh` + `.github/workflows/release.yml` are real and working.
  **`v0.1.0` was tagged and released at the user's explicit follow-up request** (2026-09-05) —
  `install.sh` was verified live end-to-end against the real published release (downloaded,
  extracted, ran). Full requirements:
  [`docs/requirements/phase9-global-install-and-daemon.md`](docs/requirements/phase9-global-install-and-daemon.md).
  **One thing still needs the user's own go-ahead, not done without it**: actually running `ctx
  service install` for real on a live machine (registers a real, persistent background service) —
  see ADR-0026's "what still needs a human decision."

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
16. ~~True per-file incremental indexing~~ — done, ADR-0020, picked by the user after a weighted
    review of the remaining large backlog items: `internal/index.Indexer` (a live graph + resolver
    held across updates), `graph.RemoveEntity/RemoveFile`, `resolve.Index.Dependents` (the reverse
    "who imports this file" lookup, built without the core resolver branching on language), and
    `watch.Events()` now reporting actual changed paths. Every F1-F9 edge case
    `docs/research/edge-case-backlog.md` catalogued is addressed (verification detail in
    ADR-0020). Verified live against this project's own real, self-hosted, running `ctxd`: a
    cross-file rename correctly propagated to the importing file (resolved -> bug-resolver and
    back) in ~4ms, against ~960ms for a full reindex of the same 105-file repo.
17. ~~Similarity/Duplicate Engine (Phase 5) — V0~~ — done, ADR-0021, picked by the user after a
    weighted review of the remaining large backlog items: `internal/similar`'s real funnel (exact
    fingerprint -> MinHash+LSH candidate generation -> structural+behavioral scoring, never one
    opaque number), `ctx similar/duplicates/decide` (CLI), `context_similar/duplicates/decide`
    (MCP), per-repo decision persistence. A real bug was found and fixed via the eval fixture
    itself (behavioral score wrongly penalized entities with zero resolved calls) — see the ADR.
    Honestly narrower than the full ask (no AST tree-edit distance, no renamed-identifier
    normalization, a 24-pair eval vs. the master plan's ≥120): measured precision 1.00 / recall
    0.50 on that smaller set, reported as measured.
18. ~~Route/event handler extraction (the real-repo Context Compiler recall gap)~~ — done, partial,
    ADR-0022, at the user's explicit request to close this before moving to language additions:
    Express-style `obj.method('string', ...middlewares, handler)` registrations (previously
    invisible — zero entities from an entire route file) now extract as real `KindFunction`
    entities. Real-repo recall@gold 0.50 -> 0.62 (R10 fixed, R07 still open — needs a real ranking
    function, not another patch to substring matching; two such patches were tried, measured, and
    explicitly rejected for regressing the synthetic fixture below 0.85). Synthetic fixture
    unchanged (still exactly 0.85, its own exit criterion, untouched by this work).
19. ~~C# extractor (Phase 3b)~~ — done, ADR-0023: `internal/parser/csharp` + `lang_csharp.go`,
    added via the plug-and-play architecture (ADR-0011) with zero changes to `resolve.go`'s core
    pipeline beyond one generic, language-agnostic addition (`reclassifyHeritageEdge`, needed
    because C#'s `base_list` syntax cannot distinguish extends from implements the way TypeScript's
    separate grammar clauses can — corrected deterministically from the RESOLVED target's real
    Kind, never a naming guess). Validated against a real repo the user picked
    (`dotnet-architecture/eShopOnWeb`, 254 files): 0.0% bug_rate, 337 resolved edges. Mid-design,
    the user raised a direct concern about inference/guessing risk; two heuristics under active
    design (capitalization-based receiver-type guessing, partial-namespace-to-directory matching)
    were paused and put back to the user rather than assumed — both rejected in favor of the
    strictly conservative option (see ADR-0023's guardrails). `fixtures/csharp-basic` (synthetic,
    ctxbench 78.3% reduction / 0.85 recall — passes) plus `fixtures/tasks/eshoponweb.json` (real
    repo, ctxbench 46.4% reduction / 0.65 recall — below target, reported honestly, same open
    seeding/ranking gap category as ADR-0022's TypeScript story, not a new one).
20. ~~Python extractor (Phase 3c)~~ — done, ADR-0024: `internal/parser/python` + `lang_python.go`,
    the third and final language in the original Go/C#/Python plan, added the same way with zero
    core changes beyond the architecture test's grep list. Validated against a real repo the user
    picked (`gothinkster/django-realworld-example-app`, chosen specifically to compare methodology
    against the TypeScript RealWorld clone already used for Phase 1/2): 0.0% bug_rate, 21 resolved
    edges. File-scoped (not directory-scoped like Go/C#) because Python genuinely has no implicit
    same-package visibility, unlike either — a real language difference, not a narrower
    approximation. `self`'s type resolves deterministically (it names its own enclosing class, a
    structural fact) without repeating C#'s rejected capitalization heuristic — a real, one-line
    text check, not a guess, applying the user's ADR-0023 guardrail as this project's standing
    default rather than re-litigating it. `fixtures/python-basic` (synthetic, ctxbench 77.2%
    reduction / 1.00 recall) and `fixtures/tasks/django-realworld.json` (real repo, ctxbench 88.9%
    reduction / 0.86 recall — the first language whose real-repo measurement clears the exit
    criterion on its own first pass, reported as a property of this specific repo's short,
    vocabulary-rich call chains, not a Context Compiler change).
21. Next after that, per the deferred list above: the global-install/system-service work (Phase 9),
    or deepening the Similarity Engine (AST tree-edit distance, identifier normalization, a larger
    labeled eval set, Web UI/Context Compiler integration) — prioritized by real usage feedback.
    This closes the three-language plan (Go, C#, Python) set at the start of Phase 3.
22. ~~Similarity Engine: identifier normalization~~ — done, ADR-0025, picked by the user over
    closing C#'s open Context Compiler recall gap: `tokenize.go`'s `normalizeIdentifiers` (the
    standard "blind renaming" clone-detection technique — every identifier-looking token not in a
    shared `structuralKeywords` list, spanning all four supported languages, becomes a placeholder
    numbered by first appearance within one entity's own token stream). Measured, not assumed:
    recall on the 24-pair labeled eval set moved 0.50 -> 0.83, precision held exactly 1.00 (zero new
    false positives). A real regression the measurement itself caught before shipping: two
    trivially-short eval-fixture functions (`getX`/`getY`) collapsed to an identical normalized
    token stream, becoming a false positive — fixed by raising `minBodyTokens` 12 -> 15 (re-measured
    at several values to confirm 15 is the smallest sufficient one). Spot-checked live against this
    project's own self-hosted source: correctly surfaced a real, previously-invisible duplication
    (`anchorFrom`/`contentHash` helpers copy-pasted identically across all four language
    extractors). AST tree-edit distance and the ≥120-pair eval set remain open, named again in
    ADR-0025.
23. ~~Phase 9 (global install, system-level daemon)~~ — done, ADR-0026, at the user's explicit
    request ("hagamos la fase 9 de una vez y cerremos el tema y las preguntas abiertas"), after
    first confirming a review-only pass of the requirements doc, then a separate, later message
    confirming the actual build. Closes the daemon-side gap named across `docs/MVP.md` and the
    requirements doc since ADR-0019: `ctxd` with no arguments watches every project in
    `~/.cartograph/projects.json` and reconciles against it every 5s — add/remove/restart a project
    with no daemon restart, verified live under a temp `$HOME` (never the real user's
    `~/.cartograph`). `internal/httpserver.ProjectRegistry` replaces the old static project slice
    so this is visible through the same HTTP API immediately. `ctx service install/uninstall/
    status` (`internal/sysservice`) registers `ctxd` as a real `launchd` agent (macOS) / `systemd
    --user` unit (Linux), one file per OS behind a build tag, not a `runtime.GOOS` switch — the
    exact bug category `edge-case-backlog.md` G3 already catalogued from Grafel's own #6218,
    followed rather than rediscovered. `install.sh` + `.github/workflows/release.yml` are real,
    tested live against this repo. **Same day, at a separate explicit follow-up request, `v0.1.0`
    was tagged and released** — `install.sh` verified live end-to-end against the real published
    release (downloaded `cartograph_darwin_arm64.tar.gz`, extracted, ran `ctx` for real). **Then, at
    a further explicit go-ahead, `ctx service install` was run for real** on the maintainer's own
    machine: `ctxd` is a real, running `launchd` agent (verified via `ctx service status`), with
    five real projects registered (this repo itself, plus four fixtures) — one of them
    (`similarity-eval`) was registered WHILE the service was already running and picked up live,
    via ADR-0026's own reconciliation, with no restart. Every "what still needs a human decision"
    item from ADR-0026 is now closed.
24. ~~Web UI: Duplicates view~~ — done, ADR-0027, picked by the user after weighing it against
    closing C#'s Context Compiler recall gap and hardening the watcher. `/api/duplicates`,
    `/api/similar`, `/api/decide` (`internal/httpserver`) are thin adapters over the exact same
    `internal/service.Duplicates/Similar/Decide` the CLI already used — no logic duplicated;
    `/duplicates` (`web/src/pages/DuplicatesPage.tsx`) renders every pair with its full score
    breakdown and a decision control, polling every 5s. A real, PRE-EXISTING bug was found and
    fixed along the way, unrelated to the new page itself: every client-side route (`/graph`,
    `/impact`, and now `/duplicates`) 404'd on a direct page load or refresh — `http.FileServer`
    had no concept of a react-router client route; fixed with a standard SPA-fallback wrapper
    (`spaFallback`), regression-tested against all three routes. Verified live against the real
    running system-service `ctxd` from item 23 (not just a test fixture): a real screenshot of
    `/duplicates?project=cartograph` showed genuine, previously-invisible self-hosting
    duplication (`text()`/`anchorFrom()` helpers copy-pasted near-identically across all four
    language extractors), and clicking "Record" in the browser persisted a real decision to
    `~/.cartograph/cartograph-<hash>/duplicate-decisions.json` — confirmed by reading that file
    afterward, not assumed from the UI alone.
