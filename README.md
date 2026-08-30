# Cartograph

Local Agent Context Manager + Code Intelligence Engine.

A tool that keeps a deterministic structural map of a codebase and compiles the **minimum
useful context** for a task, under an explicit token budget — for AI coding agents and humans
alike. The idea: `grep` → read file → `grep` → read 8 more files → guess the relationship →
discover the duplicate later becomes graph → ranked context capsule → 2-3 targeted reads →
grep to verify.

**Status**: MVP shipped (Phase 2), plus Go support (Phase 3a), a plug-and-play language
architecture (Phase 3a′), a daemon watcher (Phase 3d V0), impact analysis (Phase 4), and a
React-based web UI (Phase 6) — TypeScript and Go extraction/resolution, the Context Compiler, an
MCP server, `ctxd` (index once, then watch and re-index on change), `ctx impact`/git-diff blast
radius, and a browser UI (an integrated Overview with a searchable/filterable entity table, a
navigable graph + tree view, and impact analysis, all in one workspace — see
[ADR-0015](docs/adr/0015-react-web-ui.md)) are all built, tested, and measured against both a
real coding agent (ADR-0009) and this project's own real source (ADR-0010: 0.1% bug_rate
self-hosting). See [`docs/MVP.md`](docs/MVP.md) for the full picture and
[`docs/adr/`](docs/adr/) for how every decision was made. Functional via CLI, MCP, and a browser
today. C#/Python extraction, true per-file incremental indexing, the similarity/duplicate engine,
and global system-level install are still ahead — see the
[known limitations](#known-limitations) below.

## Prerequisites

- **Go 1.27** or newer (`go.mod` pins this)
- **A C compiler** (`cc`/`clang`/`gcc`) — the TypeScript extractor uses tree-sitter via cgo
- **Node.js** (any current LTS) — only needed to build the web UI (`make web`/`make build`); the
  Go binaries themselves have no Node runtime dependency. If you only want the CLI/MCP server and
  don't have Node installed, build those directly (`go build ./cmd/ctx`, `./cmd/ctxmcp`) instead
  of `make build`, or `go build ./cmd/ctxd` after leaving the previously-built
  `internal/httpserver/web/` in place from an earlier `make web`.
- macOS or Linux (CI runs both; Windows is untested)

## Install

```bash
git clone https://github.com/deatherick/cartograph.git
cd cartograph
make build
```

This produces four binaries in `bin/`: `ctx` (CLI), `ctxmcp` (MCP server), `ctxd` (daemon — indexes
once then watches and re-indexes on change; see [Keeping the index fresh](#keeping-the-index-fresh-ctxd)
below), `ctxbench` (the benchmark harness the project's own token-
savings claims are measured against).

## Quickstart (2 minutes, no setup)

The repo ships a small synthetic TypeScript project at `fixtures/ts-basic/` specifically so you
can try every command with zero setup — no need to find or clone a TypeScript repo first.

```bash
./bin/ctx index fixtures/ts-basic
```
```
files:          14
entities:       66
resolved edges: 54
bug_rate:       0.0%
duration:       69ms
dispositions:
  resolved           54
  external-known     20
  unimplemented      34
```

`index` runs the full pipeline (parse → resolve → build the graph) once and persists a
snapshot — every other command below reads that snapshot instead of re-indexing, which is why
they're fast (single-digit milliseconds). Re-run `index` whenever the source changes; there's no
staleness detection yet (see [known limitations](#known-limitations)).

```bash
./bin/ctx find fixtures/ts-basic UserService
```
```
Class      src/services/userService.ts#UserService  src/services/userService.ts:11-46
Test       tests/userService.test.ts#UserService    tests/userService.test.ts:5-29
```

Now point it at a real repo instead — TypeScript, Go, or a mix of both in the same repo, same
commands either way (a Java Spring service and its TypeScript frontend, a Go backend with a React
UI — one index, one graph, one `context` capsule spanning both):

```bash
./bin/ctx index ~/path/to/some/project
./bin/ctx inspect ~/path/to/some/project SomeClassOrStructName
./bin/ctx related ~/path/to/some/project SomeClassOrStructName --depth 2
./bin/ctx source ~/path/to/some/project someFunctionName
./bin/ctx context ~/path/to/some/project "add validation to the order flow" --budget 2500
./bin/ctx impact ~/path/to/some/project SomeFunctionName   # blast radius: what depends on this
./bin/ctx impact ~/path/to/some/project --git-diff         # blast radius of your uncommitted changes
./bin/ctx path ~/path/to/some/project FunctionA FunctionB  # shortest chain from A to B
./bin/ctx duplicates ~/path/to/some/project                 # undecided duplicate/similarity pairs
./bin/ctx similar ~/path/to/some/project someFunctionName   # candidates involving one entity
./bin/ctx decide ~/path/to/some/project fnA fnB same-pattern  # record a human decision on a pair
```

`duplicates`/`similar` ([ADR-0021](docs/adr/0021-similarity-duplicate-engine.md)) score every
candidate on multiple dimensions (never one opaque number) and never tell you what to do about
it — `decide` records your own call (`ignore`, `intentional`, `same-pattern`,
`should-share-abstraction`, `false-positive`) so a reviewed pair stops resurfacing.

Cartograph indexes its own source this way too — `./bin/ctx index ~/code/cartograph` runs clean
at 0.1% bug_rate ([ADR-0010](docs/adr/0010-go-extractor-and-self-hosting.md)).

Typing the same path repeatedly gets old — register a short name once and every command above
accepts it instead ([ADR-0016](docs/adr/0016-project-registry.md)):

```bash
./bin/ctx project add myapp ~/path/to/some/project
./bin/ctx index myapp
./bin/ctx find myapp SomeClassOrStructName
./bin/ctx project list
./bin/ctx project remove myapp
```

### Choosing which languages run: `ctx init`

Every language is opt-in/opt-out per project, not a fixed set — a language you don't want costs
nothing at index time (it's never even parsed), and languages are architecturally decoupled from
each other ([ADR-0011](docs/adr/0011-plugin-language-architecture.md)): adding one never touches
another's code.

```bash
./bin/ctx init ~/path/to/some/project        # wizard: detects languages, asks to confirm, writes .cartograph.json
./bin/ctx init ~/path/to/some/project --yes   # skip prompts, enable everything detected
./bin/ctx init ~/path/to/some/project --languages go,typescript   # skip detection entirely
./bin/ctx languages ~/path/to/some/project    # show what's enabled/detected without changing anything
```

`init` is optional — with no `.cartograph.json` present, `ctx index` already enables every
language it detects. Running `init` (or hand-editing `.cartograph.json`, a plain, git-committable
JSON file — `{"languages": ["go"]}`) is how you narrow that, and how you change it again later.

`context` is the Context Compiler — the actual point of this project: instead of a bare entity
lookup, it ranks everything relevant to the task description and returns a token-budgeted
capsule (see [`docs/adr/0007`](docs/adr/0007-context-compiler-vertical-slice.md) for how the
ranking/budgeting works and what it's measured against). Add `--session <id>` to reuse the same
session across multiple calls — repeat calls cost fewer tokens for anything already delivered
(the Context Ledger).

If two entities share a bare name (common — a class and a same-named test block, or two classes
in different files), every lookup command accepts `--file <substring>` to disambiguate:
`./bin/ctx inspect <path> UserService --file services`.

### Keeping the index fresh: `ctxd`

`ctx index` is a one-shot snapshot — edit the source afterward and every read command silently
serves the old one until you re-index by hand. `ctxd` does that automatically:

```bash
./bin/ctxd ~/path/to/some/project   # indexes once, then watches and re-indexes on every change
```

Runs in the foreground until `Ctrl+C`. It re-indexes only the files a change actually affects — the
changed file(s) plus anything that imports them ([ADR-0020](docs/adr/0020-true-incremental-indexing.md):
a real cross-file rename in this project's own 105-file source completed in ~4ms, against ~960ms
for a full reindex) — not the whole project every time. It doesn't yet run as a background system
service — see
[`docs/requirements/phase9-global-install-and-daemon.md`](docs/requirements/phase9-global-install-and-daemon.md)
for that design, captured but not built yet.

`ctxd` also watches more than one project at once — pass every `<path>` (each also accepts a name
registered via `ctx project add`):

```bash
./bin/ctxd cartograph ts-basic   # both watched concurrently from one process
```

See [ADR-0019](docs/adr/0019-daemon-multi-project-web-ui.md) for the design; the Web UI's project
switcher (below) is how you pick which one to look at.

### Web UI

`ctxd` also serves a browser UI by default, at `http://127.0.0.1:7420` (change with `--web
addr`, or disable with `--web ""`):

```bash
./bin/ctxd ~/path/to/some/project
# then open http://127.0.0.1:7420 in a browser
```

Watching more than one project shows a project switcher in the header, scoping every view to
whichever one is selected — data updates live (polled every few seconds) as a watched project's
source changes, with no manual reload.

One integrated workspace, not a set of separate pages: Overview's entity/edge counts and per-Kind
breakdown are clickable filters over a real, searchable, paginated entity table — selecting a row
shows **Detail** (fan-in/fan-out, source view), **Graph** (a navigable graph of that entity's
neighborhood — click any node to make it the new center, or switch to a **Tree** view of the same
relationships as an indented text list), and **Impact** (blast radius, no search step needed) as
tabs in the same panel. A separate `/graph` and `/impact` route cover free-form exploration and
git-diff-driven impact analysis. Built with React + Vite + Tailwind (`web/`, compiled by
`make web` into `internal/httpserver/web/` for `go:embed` — see
[ADR-0015](docs/adr/0015-react-web-ui.md) for why this reversed ADR-0013's original no-Node
choice, and for the real UI code reused from Grafel's own dashboard, MIT-licensed and explicitly
authorized for this layer — see [`NOTICE.md`](NOTICE.md)). A Duplicates view isn't built yet — it
needs Phase 5 (the similarity engine), which doesn't exist.

## Using it from a coding agent (MCP)

```bash
./bin/ctxmcp
```

This runs the MCP server over stdio. Point an MCP client (Claude Code, or any other) at the
`ctxmcp` binary — for Claude Code, a project-local `.mcp.json` like this works:

```json
{
  "mcpServers": {
    "cartograph": {
      "command": "/absolute/path/to/cartograph/bin/ctxmcp"
    }
  }
}
```

It exposes six tools — `context_index`, `context_compile`, `context_find`, `context_inspect`,
`context_related`, `context_source` — the exact same operations as the CLI above, over the same
service layer, so behavior never diverges between the two interfaces. See
[`docs/adr/0008`](docs/adr/0008-mcp-server.md) for how it's built and
[`docs/adr/0009`](docs/adr/0009-live-agent-demo.md) for what happened the first time a real
agent actually used it (a real bug was found and fixed).

## Known limitations

The honest list, kept current in [`docs/MVP.md`](docs/MVP.md#consolidated-known-issues-not-blocking-mvp-but-should-not-be-forgotten):

- **TypeScript/JavaScript and Go.** C# and Python are designed for but not built yet (Phase 3b/3c).
- **Go's implicit interface satisfaction (no `implements` keyword) cannot be detected** — needs
  real type-checking, not tree-sitter queries; a permanent gap, not a missing feature
  ([ADR-0010](docs/adr/0010-go-extractor-and-self-hosting.md)).
- **No staleness detection with plain `ctx index`.** Editing source silently serves the old
  snapshot until you re-index by hand, unless `ctxd` is running for that project (see
  [Keeping the index fresh](#keeping-the-index-fresh-ctxd) above), which now re-indexes
  incrementally, not the whole project on every change.
- **No fuzzy/full-text search.** `find`/`inspect`/`related`/`source` need an exact bare name or
  qualified name (`file#Name`). No SQLite/FTS5 yet — deliberately deferred, see
  [ADR-0006](docs/adr/0006-phase1-completion-and-search-scope.md).
- **Receiver-type inference is best-effort.** `obj.method()` resolves when `obj`'s type is
  statically declared (constructor properties, typed fields/variables); a local variable with no
  type annotation does not resolve, and is reported as such, never guessed.
- Full catalog, organized by subsystem: [`docs/MVP.md`](docs/MVP.md).

## Documentation map

- [`docs/MVP.md`](docs/MVP.md) — **consolidated status, MVP definition, known issues, roadmap**
- `docs/adr/` — architecture decision records, one per real design decision made, in the order
  they were made — the fastest way to understand *why* something works the way it does
- `docs/research/` — discovery notes from studying Grafel (MIT-licensed, studied as a reference,
  no code copied — see [ADR-0002](docs/adr/0002-grafel-reuse-protocol.md)) and a 90+-entry
  edge-case backlog derived from it (plus this project's own, e.g. Go's edge cases, J1-J7)
- `docs/benchmarks/` — frozen `ctxbench` results per phase, so token-savings claims are always
  checked against a prior recorded run, not just today's
- `docs/requirements/` — user-facing requirements captured ahead of the phase that implements them

## Running the benchmark

```bash
make bench        # synthetic fixture, vendored in this repo
make bench-real   # real external repo, auto-cloned to ~/code/_ref on first run, never vendored

./bin/ctxbench --baseline --capsule --budget 2500   # both the grep+read baseline AND the Context Compiler, side by side
```

`ctxbench` is what backs every token-savings number this project claims — see
[`docs/benchmarks/README.md`](docs/benchmarks/README.md) for how to read and reproduce them, and
[`docs/research/06-token-measurement.md`](docs/research/06-token-measurement.md) for why a
token-savings figure is never reported without a recall figure next to it.

## Contributing

Read [`docs/MVP.md`](docs/MVP.md) first — it says exactly what's done, what's deliberately
deferred, and what's next, so effort doesn't duplicate what's already decided. Every real design
decision has an ADR in `docs/adr/`; add one for anything that isn't an obvious bug fix.

## License

[MIT](LICENSE).
