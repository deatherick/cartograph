# ADR-0008: MCP server — the last piece of Phase 2's MVP scope

- **Status**: Accepted (implemented)
- **Date**: 2026-08-29
- **Related**: docs/MVP.md, ADR-0007 (Context Compiler)

## Context

`docs/MVP.md` named this the one remaining blocker to shipping v0.1: without an agent-facing
interface, a tool whose own stated purpose is "Local Agent Context Manager" could only actually
be used by a human typing CLI commands — not by the coding agent it was built for.

## Decision

Built on the **official** `github.com/modelcontextprotocol/go-sdk` (not a hand-rolled JSON-RPC/
stdio implementation, and not the popular third-party `mark3labs/mcp-go` — the protocol itself
is not something this project has reason to reimplement or depend on a non-canonical
implementation for).

**`internal/mcpserver`** registers six tools, every one a thin adapter with no product logic of
its own — each handler does nothing but call the exact same `internal/service` method `cmd/ctx`
calls, and render the result through `internal/render`:

- `context_index` — run the pipeline and persist a snapshot (not in `docs/MVP.md`'s original
  five-tool list, added because requiring a human to shell out to `ctx index` before an agent
  could use anything else would make the MCP server nearly unusable standalone)
- `context_compile`, `context_find`, `context_inspect`, `context_related`, `context_source` —
  exactly the five named in `docs/MVP.md`

**A real duplication was caught and fixed while wiring this**: the CLI's `repoName` helper
(derive a stable repo identity from a path) needed to exist in the MCP server too. The first
draft reimplemented it by hand in `internal/mcpserver` — worse, as a manual byte-scanning
path-splitter instead of `filepath.Base`. Moved to `service.RepoName`, an exported helper both
`cmd/ctx` and `internal/mcpserver` now call, consistent with the project's standing rule that no
logic is duplicated between interfaces.

**Tool-level errors go in `Content` with `IsError: true`**, per the SDK's own documented
guidance, not as MCP protocol-level errors — so an agent sees "no index found, run context_index
first" as readable text it can act on, not an opaque RPC failure.

**Every tool also returns structured content** (the SDK's generic `AddTool[In, Out]` auto-
populates `StructuredContent` from the handler's typed return value) alongside the human-readable
`TextContent` — an agent that wants to parse fields programmatically can, without the server
maintaining two separate code paths.

### Verification

Two layers, deliberately not just one:

1. **In-memory transport tests** (`internal/mcpserver/mcpserver_test.go`, 7 tests) — connect a
   real `mcp.Client` to the server via `mcp.NewInMemoryTransports()` and exercise the actual
   JSON-RPC request/response path (not direct Go function calls into the handlers), including
   tool-level error handling, structured content, and — notably — the Context Ledger's
   session-based deduplication working correctly *through* the MCP layer, not just in
   `internal/compile`'s own unit tests.
2. **Real subprocess test**: a throwaway client program spawned `bin/ctxmcp` as an actual child
   process via `mcp.CommandTransport` (exactly how Claude Code or any other MCP client connects
   to a stdio server) and called `context_index`/`context_find` against the real binary — not
   just the package under test. This is the closest thing to the actual live-agent demo
   `docs/MVP.md` still lists as outstanding, run manually to confirm the binary itself works
   before that demo happens for real.

An earlier attempt to verify the binary by hand-crafting raw newline-delimited JSON-RPC and
piping it into `bin/ctxmcp` produced no output — investigated and found to be a mistake in the
hand-written request framing, not a server defect, once the SDK's own client (subprocess test
above) round-tripped correctly against the identical binary.

## Consequences

- `docs/MVP.md`'s Definition of Done: MCP server wiring is now checked off. The two remaining
  items are the live agent demo and a README quickstart — both explicitly *not* attempted in
  this change, kept in sequence.
- `go.mod` gains the MCP SDK and its transitive dependencies (`jsonschema-go`, `oauth2`,
  `uritemplate`, `errgroup`, `x/time`, `segmentio/encoding`) — none of which this project has any
  reason to vet further, since they arrive as the official SDK's own dependency graph, not
  hand-picked.
- `internal/render` now exists as a genuine shared layer between two interfaces (CLI, MCP),
  proving out the "one service layer, formatting shared" architecture the project has stated as
  a rule since Phase 1 but had — until two real interfaces existed — never actually been forced
  to keep.

## Alternatives considered

- **`mark3labs/mcp-go`** (a popular third-party Go MCP implementation) — rejected in favor of the
  project maintained by the MCP organization itself, for the same reason the project avoided a
  hand-rolled protocol implementation: prefer the canonical implementation over a community one
  when both exist and the official one is viable.
- **Skipping `context_index` to stay exactly within `docs/MVP.md`'s five-tool list** — rejected:
  an MCP server that cannot bootstrap its own snapshot would force every session to start with a
  human running the CLI first, undermining the entire point of an agent-facing interface.
