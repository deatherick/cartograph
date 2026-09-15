# ADR-0030: Sequence-diagram view — a real-data exploration, not a decision to ship

- **Status**: Exploratory (built on `explore/sequence-diagram-real-path`, off the now-merged
  `main` from ADR-0029, at the user's explicit request: "parte de main con esta nueva propuesta
  para explorarla" — start from main, explore this)
- **Date**: 2026-09-15
- **Related**: ADR-0029 (the Web UI redesign this branches from), the user's own separate question
  about `tt-a1i/archify` (a GitHub repo shared as a second design reference) and the "should we
  integrate its charts" discussion this ADR is the answer to

## Context

After merging the Observatory-inspired redesign, the user asked whether `tt-a1i/archify`'s own
diagrams (architecture/workflow/sequence/data-flow/lifecycle) would be worth integrating. Reading
that project's own README closely surfaced a disqualifying fact: **its diagrams are not derived
from real code relationships** — its own documentation states the diagram's layout is
hand-curated by an agent ("the agent chooses hierarchy, spacing, routes, and emphasis... layout
judgment over generic auto-layout"), and it accepts either a hand-authored description or a code
repository as input, but the actual diagram content is agent-curated either way. That is precisely
the class of "guess/infer with weights" this project's own resolver has been built from day one to
avoid (the user's own standing rule, restated across every ADR since C#'s design: never let
inference take over, never guess).

**Decision: reuse the diagram TYPE (sequence), not the tool.** `internal/service.Path` (already
shipped, used by `ctx path`/`context_path`) already computes a real, deterministic shortest-path
chain of resolved edges between two named entities — exactly the raw material a sequence diagram
needs (participants = entities on the path, messages = real edges), with zero authoring or
curation involved. This ADR wires that EXISTING real data into a new Web UI view, drawn as a
sequence diagram, rather than adopting anything from Archify's own codebase, rendering pipeline,
or agent-curated-layout philosophy.

## What was built

- **`GET /api/path`** (`internal/httpserver/httpserver.go`) — a thin adapter over `svc.Path`,
  mirroring every other read endpoint's exact shape (`resolveProject`, `writeJSON`/`writeError`).
  Two new tests (`TestHTTPServer_Path`, `TestHTTPServer_Path_MissingParams`) against the same real
  TS fixture (`helper` calling `greet`) every other httpserver test already uses.
- **`api.path()`** (`web/src/lib/api.ts`) — the typed client call, plus a `PathResult` type
  mirroring `service.PathResult` exactly (same casing-quirk discipline this file's own header
  documents).
- **`SequencePage.tsx`** (new route, `/sequence`) — two entity-name inputs + a "Find path" button;
  on success, an inline SVG sequence diagram: one column per participant (entity on the path, in
  order), a dashed lifeline per column, and one arrow per hop, labeled with the real
  `EdgeKind` (`CALLS`, `EXTENDS`, ...) and the resolver's own confidence score — never invented,
  always the exact value `Related`/`ShortestPath` already computed. Styled consistently with
  ADR-0029's own token/typography system (display face for participant names, mono for kind/edge
  labels and confidence).
- A `NavRail` entry, clearly marked "Exploratory" both in the nav's own code comment and in the
  page's own `.eyebrow` label — this is not presented as a finished, decided feature.

## What was found and fixed only by actually testing it live, not assumed from the diff

- **The obvious mistake to check for and avoid**: testing this branch's new `/api/path` route
  against the REAL running production `ctxd` service (still on the pre-this-branch build) would
  have silently hit its SPA fallback handler (an unmatched route returns `index.html`, HTTP 200,
  not a 404) instead of a real 404 or the new JSON — a false "it works" easy to miss. Caught before
  it became a false positive: a throwaway `ctxd` instance was built from THIS branch and run
  against a small isolated synthetic fixture (a 3-function call chain, in a scratch directory, its
  own isolated `$HOME` so it never touched the real `~/.cartograph` registry) on a different port,
  with the dev server's proxy target pointed at it — confirming the real JSON response — rather
  than trusting an early curl check that only looked at the HTTP status code and got fooled by
  this exact trap.
- **Label/arrow overlap**: an early layout put a hop's "conf 0.95" caption close enough to its own
  arrow line that it visually read as struck through — fixed by placing the arrow at a fixed
  distance below both label lines rather than deriving the gap from font metrics.
- The dev-server proxy edit made to point at the throwaway test instance was reverted before
  committing anything — `web/vite.config.ts` is unchanged from `main`.

## What this is explicitly NOT

- **Not a port of Archify.** No code, dependency, rendering engine, or diagram schema from that
  project appears here — only the observation that "sequence diagram" is a useful SHAPE for
  already-real path data this project already computes.
- **Not a decision to ship.** Left on its own branch, not merged, at the user's own framing
  ("explorarla" — explore it) — same discipline as ADR-0029's own initial branch, before the user
  reviewed it live and asked for the merge.
- **Not a general-purpose diagramming feature.** Only the one shape `service.Path` already
  supports (a linear shortest-path chain) is rendered; branching/parallel call structure (which a
  REAL sequence diagram can express, e.g. two independent branches from one caller) is not
  attempted — `Path` only ever returns one linear chain by construction.
