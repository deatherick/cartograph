# ADR-0009: Live agent demo — the last item on docs/MVP.md's checklist

- **Status**: Accepted (done; a real bug found and fixed along the way)
- **Date**: 2026-08-29
- **Related**: docs/MVP.md, ADR-0008 (MCP server)

## Context

`docs/MVP.md` named this the final, purely functional gap before MVP: "connect a real coding
agent to the MCP server and resolve a real task against the real-repo validation clone,
measuring actual file reads before/after — the proof the whole project has been building
toward." Everything up to this point had been verified by this project's own tests
(`internal/mcpserver/mcpserver_test.go`) — a real, but self-administered, check. This ADR is
what happened when an actual coding agent used the tools on its own, with no coaching beyond
"use this MCP server as your primary investigation tool."

## Method

A real task from this project's own benchmark (`fixtures/tasks/realworld-ts.json`, task R09):
*"Is there an existing helper for building a JSON:API-style profile response I should reuse
before writing a new one for comments?"* — investigate-only, safe to run against the real-repo
validation clone (`~/code/_ref/realworld-ts`) without mutating it, with a known-correct answer
already encoded as that task's `gold_files`.

Two headless Claude Code invocations (`claude -p`), same task, same repo, same model:

1. **Baseline** — no MCP configured, ordinary file tools only.
2. **With MCP** — `bin/ctxmcp` configured via `--mcp-config`, `--strict-mcp-config` (no other MCP
   servers reachable), told to prefer the `context_*` tools over grep/read.

The repo was pre-indexed once (`ctx index`) before either run, matching how a real user would
operate — indexing is one-time setup, not part of resolving a task.

## What actually happened — three runs, not one

**Run 1 (with MCP, default permissions): every `context_find` call failed** with *"Claude
requested permissions to use mcp__cartograph__context_find, but you haven't granted it yet."*
Not a Cartograph defect — headless mode's permission gate has no interactive prompt to answer.
The agent degraded gracefully (fell back to grep/read, reached a correct answer), but this
wasn't the test intended. Fixed by passing `--allowedTools` scoped to exactly the `context_*`
tools plus the baseline file tools, for both runs.

**Run 2 (with MCP, permissions granted): a real bug, not a graceful degradation.**
`context_find` and `context_related` both failed with *"MCP server 'cartograph' returned a
malformed result that failed schema validation: expected 'record'"* — a genuine defect this
project's own tests had never caught. Root cause: both handlers declared `Out=any` in
`mcp.AddTool[In, Out]` and returned a bare Go slice. The SDK's generic schema derivation
synthesizes an object-shaped (`"record"`) output schema when it cannot derive one from `any`,
and a slice value fails validation against that schema on every single call. `context_compile`,
`context_inspect`, and `context_source` were unaffected because their handlers return
struct-shaped values, not slices — the bug was specific to the two slice-returning tools.

The agent adapted (context_compile succeeded, cache the real source via `context_source`, still
reached the correct answer with zero raw grep/read calls beyond what `context_find`'s failure
forced), but this is exactly the kind of defect a live demo exists to surface: **none of
`internal/mcpserver/mcpserver_test.go`'s original 7 tests asserted anything about
`StructuredContent`'s shape** — they only checked that `Content` (the text block) was present.

### The fix

Every handler now declares a **concrete** `Out` type — never `any` — wrapping any slice-shaped
result in a named struct (`findResult{Entities []model.Entity}`,
`relatedResult{Related []model.RelatedEntity}`), matching the pattern `context_source` already
used. A new regression test, `TestMCPServer_StructuredContentIsAlwaysAnObject`, marshals every
tool's `StructuredContent` and requires it to unmarshal as a JSON object — verified to actually
catch the original bug by temporarily reverting the fix and confirming the test fails exactly
the way the live run did, then re-confirming it passes with the fix restored.

**Run 3 (with MCP, fix applied, permissions granted): clean.**

## Result — Run 3 vs the baseline

| | Baseline (no MCP) | With MCP (fixed) |
|---|---:|---:|
| Tool calls | 7 (1 subagent delegation + 2 Bash + 4 Read) | 7 (2 ToolSearch + 1 find + 1 compile + 2 source + 1 related) |
| Raw grep/bash/read calls | 6 | **0** |
| Subagents spawned | 1 | 0 |
| Output tokens | 3,152 | 1,375 (**-56%**) |
| Real cost (`total_cost_usd`) | $0.2084 | $0.0928 (**-55.5%**) |
| Answer correctness | Correct, cites `toProfileJSONFor` + comment/article reuse | Correct, cites the same plus `article.model.ts`'s parallel usage |

Both runs reached the right answer (matching the task's known-correct `gold_files`). The
MCP-equipped run needed **zero** raw file-system tool calls — every finding came through
`context_compile`/`context_source`/`context_find`/`context_related` — at roughly half the real
dollar cost and with no need to delegate to a subagent.

## Consequences

- `docs/MVP.md`'s last functional checklist item is done. Only the README quickstart remains
  before MVP is complete.
- **A real, previously-undetected bug shipped in ADR-0008's commit and was caught by this demo,
  not by any test written before real usage happened.** This is direct evidence for why
  `docs/MVP.md` treats the live demo as load-bearing, not ceremonial — self-administered tests
  verify what the author thought to check; a real agent finds what nobody thought to check.
- The regression test added here (`TestMCPServer_StructuredContentIsAlwaysAnObject`) generalizes
  past this one bug: any future tool that returns `Out=any` with a non-object runtime value will
  now fail CI, not just a live demo.
- The efficiency numbers (0 raw file reads, -55.5% cost) are a single real-world data point, not
  a benchmark — `ctxbench` remains the reproducible measurement (ADR-0007); this demo is the
  qualitative proof that a real agent, unscripted, chose to use the tools productively and got a
  better outcome doing so.

## Alternatives considered

- **Treating the Run 1 permission-gate failure as sufficient proof of "graceful degradation" and
  stopping there** — rejected: it never actually exercised the MCP tools, so it proved nothing
  about Cartograph itself, only that Claude Code degrades sensibly when a tool is unavailable.
- **Fixing the Run 2 bug and re-running only the fixed configuration, discarding the earlier
  transcripts** — rejected: the failed runs are part of the honest record of how this was found,
  consistent with this project's practice throughout (e.g. ADR-0007's tuning table keeps the
  rejected parameter settings, not just the final one).
