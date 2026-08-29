# Phase 6 Web UI — user requirements

Captured verbatim from a product conversation on 2026-08-29, before Phase 6 exists. Recorded
here so the requirement survives to whenever Phase 6 actually starts, rather than living only
in a chat transcript. See the project plan's Phase 6 section for the phase's existing outline
(Overview, Search palette, Entity inspector, Duplicates, Impact, Graph, Projects/Settings) — the
items below are the concrete asks that section should be checked against when Phase 6 begins.

## Status at time of capture

Not started. Phase 1 (parser/resolver/in-memory graph/CLI) is a working vertical slice with no
persistence yet. There is no daemon, no HTTP layer, no MCP server, and nothing to render — Phase
6 depends on Phase 1's remaining scope (persistence), Phase 3 (daemon, incremental indexing),
and ideally Phase 4/5 (impact analysis, duplicate detection) to have real data worth visualizing.

## The ask

A web interface to visually explore the graphs of analyzed projects, comparable in spirit to
Grafel's own dashboard (confirmed: Grafel does **not** use Grafana — see
`docs/adr/0001-go-core.md` and the note below — it ships a custom embedded React dashboard using
cosmos.gl for GPU-accelerated graph rendering, React Flow, and elkjs for automatic graph layout).
Specifically, the developer should be able to:

1. **See the graph visually** — an actual navigable graph canvas, not just tabular output.
2. **See classification of entities** — classes, components, entities/models, types, and however
   many other kinds the taxonomy grows to (see `internal/model`'s `Kind` enum, currently Class/
   Interface/Function/Method/Property/Enum/TypeAlias/Test — this UI requirement implies the
   taxonomy needs to keep growing toward framework-level kinds like Component/Service/Endpoint,
   which the master plan already scopes for later phases once real extractors populate them).
3. **See relationships** between all of the above (the edges: CALLS, IMPORTS, EXTENDS, etc.).
4. **Classify objects manually** — the developer should be able to look at code objects the
   system extracted and apply their own classification/tags to them, not just consume what the
   extractor produced automatically. This is broader than the master plan's existing "mark a
   duplicate pair as intentional/false-positive" UI (already planned) — this ask is about
   classifying individual entities themselves (e.g., tagging a class as "domain model" vs
   "infrastructure", or marking an ad-hoc pattern), which is closer to Phase 7's "Learned
   Relationships" concept but applied to nodes, not just edges. Needs its own design pass when
   Phase 6/7 planning starts.
5. **Identify patterns** — surface recurring structural/behavioral shapes across the codebase
   (relates to the master plan's Phase 5 Similarity Engine and Phase 5's "architecture smell
   detection" backlog item — high fan-in/fan-out, cycles, duplicated validators, etc. — but the
   ask here is explicitly about the UI making patterns visible/explorable, not just the backend
   computing them).
6. **Quantify** — counts, metrics, stats views (the master plan's Overview screen already covers
   the basics: files/entities/edges/languages/duplicate candidates/high-impact nodes; this ask
   confirms that screen is a hard requirement, not optional).
7. **Explore** — free navigation through the graph and the entity inspector (already planned:
   Search palette + Entity inspector in the master plan's Phase 6 section).
8. **Filter** — filter the graph/lists by kind, language, repo, tag, pattern, confidence,
   provenance, etc. Not explicitly broken out as its own UI surface in the master plan yet —
   should be a first-class, persistent capability across every view (graph, duplicates, impact),
   not just a one-off search box.
9. **Visualize** all of the above from one interface — a single web dashboard, not a collection
   of disconnected outputs.

## Why this doesn't change the existing plan, but sharpens it

The master plan's Phase 6 section (Overview, Search palette, Entity inspector, Duplicates,
Impact, Graph, Projects/Settings) already covers most of this at a high level. What this
conversation adds that wasn't explicit before:

- **Manual entity classification/tagging** is a distinct, first-class UI capability, not just
  a byproduct of duplicate-pair decisions. Needs a data model addition (a way to attach
  human-authored tags/classifications to an `EntityID`, analogous to how `docs/research/
  08-process-architecture-and-residuals.md` describes Grafel's `source: agent-repair`
  attribution pattern for learned edges — the same "always attributed, never silently merged
  with deterministic truth" principle should apply to human-applied entity tags).
- **Filtering** should be treated as a cross-cutting UI primitive from the start of Phase 6
  design, not bolted on per-screen later.
- **Pattern identification** is explicitly a UI-visible feature, which argues for prioritizing
  it earlier in Phase 6's screen-build order than "Graph" (the master plan already deprioritizes
  the raw graph visualization to last, on purpose — see Phase 6's note that the graph view is
  "the least valuable and most time-consuming to build well").

## Open questions for whenever Phase 6 planning actually starts

- What's the tagging/classification data model — free-form tags, a fixed taxonomy, or both?
- Does manual classification feed back into the resolver/ranker (e.g., a tag influencing the
  Context Compiler's relevance scoring in Phase 2), or is it purely descriptive/organizational
  for humans browsing the UI?
- Which "patterns" are in scope for V1 of this UI — architecture smells only (Phase 5's
  existing backlog), or a broader pattern-detection surface the user has in mind but hasn't
  detailed yet?
