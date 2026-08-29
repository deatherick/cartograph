# NOTICE

Cartograph's core (parsing, resolution, the graph engine, the Context Compiler, the CLI, the MCP
server, and the daemon) is original work. It does not vendor, import, or depend on
`github.com/cajasmota/grafel`.

Grafel (MIT, https://github.com/cajasmota/grafel) was studied as a reference during design — see
`docs/research/` for the discovery notes on its architecture: what was adopted conceptually, what
was adapted, and what was deliberately discarded. Through Phase 5 (see `docs/adr/0002-grafel-reuse-protocol.md`),
no source code was copied.

## Web UI (`web/`): adapted from Grafel's webui-v2, with the user's explicit authorization

Starting with the web UI (Phase 6, `docs/adr/0015-react-web-ui.md`), this changed for the
**frontend visual/UI layer specifically** — never the core listed above — at the explicit,
direct instruction of the project's owner, who confirmed Grafel is MIT-licensed (permitting
this) and that the earlier "study and reimplement, never copy" decision was their own choice to
make and to revise for this one layer, since it does not involve Cartograph's actual
intellectual contribution.

The following files in `web/src/` are adapted from Grafel's `webui-v2/src/` (MIT License,
Copyright (c) 2026 Jorge Cajas — https://github.com/cajasmota/grafel):

- `styles/tokens.css`, `styles/app.css` — design tokens (light/dark palette, the accessible
  pastel categorical scale, spacing/radius/shadow scale) and the Tailwind token bridge. Trimmed of
  the "warm" palette variant and animation keyframes tied to Grafel-only features (index wizard
  progress, insight-glow, flow-replay bounce) this project has no equivalent of.
- `lib/utils.ts` (the `cn` class-merge helper).
- `lib/graph-colors.ts` — `parseColor`/`writeNormalizedRGBA`/`readPastelScale`/`pastelAt` (a
  hard-won lesson about cosmos.gl's color-buffer format, kept even after Cartograph's own graph
  view moved to React Flow instead — see below — since the pastel-scale resolution helpers are
  still used for the Kind-based color legend). Cartograph's own `kindSlot`/`KIND_LEGEND` (Kind →
  color, replacing Grafel's repo/community-based coloring) are new, not adapted.
- `components/ui/button.tsx`, `card.tsx`, `badge.tsx`, `pill.tsx`, `tooltip.tsx`, `dialog.tsx`,
  `popover.tsx`, `tabs.tsx`, `tab-count.tsx`, `input.tsx`, `kbd.tsx`, `skeleton.tsx` — generic
  design-system primitives, each carrying a one-line attribution comment at the top of the file.
- `components/chrome/NavRail.tsx`, `TopBar.tsx` — the layout PATTERN (a hover-expand icon rail,
  breadcrumb-header structure) was adapted; the actual screen list and all content are
  Cartograph's own (Overview/Graph/Impact — Grafel's Topology/Paths/Links/GraphQL/Infrastructure/
  Security/Taint/Dependency-Injection/Error-flow/Quality/Operations screens have no Cartograph
  equivalent and were not carried over).

**Not adapted, despite being considered**: Grafel's `components/graph/graph-canvas.tsx` (a
2,700-line `@cosmos.gl/graph` implementation) was evaluated but not ported — real usage found
`@cosmos.gl/graph` itself unsuitable for Cartograph's scale (see ADR-0015: it renders no per-node
text, fine for Grafel's thousands-of-nodes graphs, useless for Cartograph's bounded ~10-40-node
neighborhoods). Cartograph's graph view (`components/EntityGraphPanel.tsx`) is built fresh on
`@xyflow/react` (React Flow) with `dagre` for layout — the same class of open-source library
Grafel's own `package.json` also lists (`@xyflow/react` is one of its direct dependencies,
alongside cosmos.gl, for its DAG-shaped views), but an independent implementation, not adapted
from Grafel's code.

## Third-party dependencies

See `go.mod`/`go.sum` (Go) and `web/package.json` (the web UI's Node dependencies) for the full,
current list.
