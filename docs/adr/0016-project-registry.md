# ADR-0016: `ctx project` — a CLI-only, global project registry

- **Status**: Accepted
- **Date**: 2026-08-29
- **Related**: `docs/MVP.md`'s CLI/UX known issues (this closes one), ADR-0011 (`.cartograph.json`,
  which this is deliberately not the same thing as), ADR-0012 (`ctxd`'s own still-open
  multi-project gap, which this is also deliberately not the same thing as)

## Context

Following a weighted backlog review with the user (effort × value, prioritizing small-but-
significant items over the two large remaining phases — the similarity engine and true
incremental indexing), `ctx project add/list/remove` — named explicitly in the master plan's CLI
scope and tracked as a known gap in `docs/MVP.md` since Phase 1 — was picked as the second item in
this batch: every command has always taken a raw filesystem path, with no way to refer to a
project by a short name.

## Decision: a small global name→path registry, CLI-only, resolved once per command

`internal/project` is deliberately minimal: a JSON file at `~/.cartograph/projects.json` (never
inside a project directory — ADR-0011's invariant that `.cartograph.json` is the only
project-local artifact stays true; this is the one thing that's genuinely global) mapping a name
to an absolute path, with `Add`/`Remove`/`List`/`Resolve`. `Resolve` is the integration point every
existing CLI command's `root := args[0]` now runs through — if the argument matches a registered
name, its path is substituted; otherwise the argument passes through **completely unchanged**,
so every user who never registers a project sees no behavior change at all.

## What this is explicitly NOT

- **Not `ctxd`'s daemon-side multi-project registry.** A future `ctxd` watching and serving
  several projects from one running process (ADR-0012's own documented gap: "no multi-project
  registry yet") is a different, harder problem — one process, several watchers, several HTTP
  mounts. This ADR is purely a CLI naming convenience; `ctxd` still takes exactly one path.
- **Not wired into MCP.** `context_index` and friends still document their `root` argument as
  "absolute path to the repository" — an agent calling MCP tools doesn't get name resolution yet.
  A natural follow-up, not attempted here to keep this change small and reviewable.
- **Not validated beyond "is this a directory that exists".** `Add` checks the path resolves and
  is a directory; it does not check that the directory has ever been indexed, has a
  `.cartograph.json`, or is even a real code repository. Registering nonsense just means later
  commands fail with their own normal "no index found" error, not a special one from this layer.

## Verification

10 new tests in `internal/project` (round-trip add/list, re-adding overwrites in place, removing
an unregistered name is a no-op not an error, resolving an unregistered input passes through
unchanged, sorted listing, a nonexistent path is rejected at `Add` time). Manually verified
end-to-end: `ctx project add cartograph ~/code/cartograph` then `ctx index cartograph` indexed
this project's own real source exactly as `ctx index ~/code/cartograph` would have. `go build/vet/
test/lint` clean.
