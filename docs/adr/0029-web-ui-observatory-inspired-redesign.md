# ADR-0029: Web UI visual redesign, inspired by Netchex Observatory's own field guide

- **Status**: Proposed (built on a separate branch, `redesign/web-ui-observatory-inspired`,
  explicitly NOT merged yet — the user's own instruction: "hazlo en una rama aparte por si no
  funciona al 100%")
- **Date**: 2026-09-14
- **Related**: ADR-0002/0015 (Grafel reuse protocol — this ADR's own token/component work still
  builds on that MIT-licensed base, per NOTICE.md; nothing here changes that authorization's
  scope), ADR-0013 (original Web UI architecture)

## Context

The user shared an internal engineering "field guide" document (Netchex Observatory, an unrelated
internal tool at the user's employer) and asked for Cartograph's Web UI to be redesigned using it
as a base, after judging the current UI "no muy bien diseñada" (not very well designed) — a
subjective but specific complaint: functional, clean, correctly using its own token system, but
visually generic (one font family for everything, thin borders, no typographic hierarchy, no
distinctive treatment for data vs. prose).

The shared document is explicitly a **field guide document**, not the Observatory application
itself — a written architecture/reuse reference with its own "What to reuse" section that grades
each pattern Take/Adapt/Skip for porting into an unrelated new project. Its own explicit guidance:
the MECHANISM (a three-layer token contract, a display+mono type pairing, specific component
conventions) is the portable asset; its literal brand colors (violet/teal) are Netchex's own and
should be swapped for a new project's own identity. This ADR follows that guidance precisely: no
Netchex brand color, name, or content appears anywhere in Cartograph's own code — only the
*mechanism* was ported, with Cartograph's own from-scratch color choices.

## Decision: port the mechanism, not the brand — three-layer tokens, a display/mono type pairing, restyled shell + components

**Tokens (`web/src/styles/tokens.css`), restructured into three layers**, the reusable idea the
field guide names as its single most portable asset:

1. **Primitives** (`--c-*`) — raw hex ramps (slate, ink, sky, violet, status colors, the existing
   pastel categorical ramp), no meaning attached.
2. **Semantic contract** (`--bg`, `--text`, `--accent`, ...) — deliberately kept as the SAME names
   the prior (Grafel-derived) tokens.css already used, since every component already consumes them
   through `app.css`'s Tailwind `@theme` bridge (`bg-surface`, `text-text-2`, ...). Renaming a
   contract dozens of files already depend on, for no functional gain, would have been pure,
   avoidable risk — the real upgrade is what FEEDS these names now (real primitives, not a hex
   literal baked into each theme block) and the values themselves (richer shadow/radius scale, a
   genuine second accent hue, real motion tokens).
3. **Theme** (`[data-theme]` blocks) — re-points layer-2 names at different layer-1 primitives;
   adding a theme later means one more block here, zero component changes.

A new **`--accent2`** (violet) is reserved specifically for the Duplicates/similarity engine's own
"this looks like that" signal — kept visually distinct from the primary sky accent used for
navigation/actions, so a pattern-match score never reads as a status or a call-to-action.

**Typography**: a genuine display/body/mono trio replaces the prior single Geist/Geist Mono pair —
Space Grotesk (display: h1–h4, nav labels, the new `.eyebrow` utility), Inter (body copy), JetBrains
Mono (data: counts, paths, scores). This is the single most visible change in every screenshot
comparison — headings and data now read with real typographic hierarchy instead of one weight
doing everything.

**Shell**: `NavRail` changed from a hover-to-expand icon rail (labels hidden until a pointer
happens to sit over it) to a persistent, always-labeled sidebar — more scannable for a small, fixed
screen count like Cartograph's own four, and closer to the field guide's own persistent
`nav.toc`. `TopBar`'s current-screen label now renders in the display face; its stat/status pills
render their values in mono (`Badge`'s new `mono` prop) so a count reads as data, not prose.

**Components**: `Badge` gained an `accent2` tone and a `mono` prop; `Card` gained a `CardTag`
sub-component (a small uppercase mono chip above a title — the field guide's own most-repeated
component pattern); a new `.eyebrow` utility class (uppercase, letter-spaced, display-face caption)
replaced several pages' own hand-repeated `text-xs font-semibold text-text-3 uppercase
tracking-wide` className strings with one shared primitive. `EntityTable`'s header row now uses
`.eyebrow`; the Duplicates page's score chips and "Exact match" badge moved from the generic
success/neutral/danger ramp to `accent2`, since a similarity score is a pattern signal, not a
health status.

**No-flash theme script**: `index.html` gained a small synchronous script that stamps
`data-theme` before first paint, reading `localStorage`/`prefers-color-scheme` the same way
`useTheme()`'s React effect already does — without it, a user whose stored preference differs from
`prefers-color-scheme` sees one frame of the wrong theme before React's effect (which only runs
AFTER first paint) catches up. The exact fix the field guide documents for the identical problem.

## What was found and fixed only by actually screenshotting the redesign, not assumed from the diff

Live-rendered against the real running project's own data (via `npm run dev`'s existing API proxy
to a real `ctxd` instance, never the production system service itself — see "How this was
verified" below) at every step, not just built and assumed correct:

- **A wrapped button label.** Switching the body font from Geist to Inter changed the "Analyze
  diff" button's natural text width just enough that, inside its flex row next to a `w-full`
  input, it wrapped onto two lines ("Analyze" / "diff") — a regression invisible in the source diff,
  only visible in a real screenshot. Fixed at the component level (`Button` gained `shrink-0
  whitespace-nowrap` as a permanent, default behavior — not a one-off fix at the single call site
  that happened to surface it), so the same class of bug can't recur at any other Input+Button row
  in the app.
- **Mid-word-wrapping file paths.** `EntityTable`'s Location column, previously an
  unconstrained-width table column, now wrapped a long path awkwardly at an internal hyphen
  (`fixtures/csharp-` / `basic/...`) once the Kind column's eyebrow-style header text shifted
  column proportions slightly. Fixed with `table-fixed` + explicit column widths + `truncate` (with
  a `title` attribute for the full path on hover) on both the Name and Location cells.

Both were caught by a live before/after screenshot comparison specifically because of the "measure,
don't assume" discipline this project has followed for every non-UI change too (ADR-0022,
ADR-0028) — applied here to visual work for the first time.

## How this was verified

Screenshotted the CURRENT (pre-redesign) UI first, live, against the real running system-service
`ctxd` instance (read-only — no data or config was touched), across all four screens plus a dark
mode capture, to have a concrete baseline instead of relying on memory of "looks generic." All
redesign iteration afterward ran against `npm run dev`'s own dev server (a separate, temporary
process on a different port, proxying `/api` to the SAME real running `ctxd` for real data) — the
production system service was never restarted, reconfigured, or otherwise touched by this work.
`go build/vet/test -race`, `golangci-lint run`, and the web project's own `npm run build`/`oxlint`
all pass on this branch.

## What this is explicitly NOT

- **Not a fork of Observatory's own application code.** Nothing was copied from a live Observatory
  instance — the source was a written field guide document, and only the architectural *mechanism*
  it explicitly recommends porting was used, with Cartograph's own from-scratch primitive palette.
- **Not a rename of the semantic token contract.** Every component's existing Tailwind
  classNames (`bg-surface`, `text-text-2`, ...) still resolve correctly — only what feeds those
  names changed.
- **Not multi-theme yet.** The three-layer structure makes adding a third/fourth theme cheap later
  (one more `[data-theme]` block, zero component changes) — but only light/dark exist today, same
  as before this ADR.
- **Not merged to `main`.** Left on its own branch at the user's explicit request, specifically so
  a redesign this size can be reviewed and discarded cleanly if it doesn't hold up, without
  touching the working `main` branch or the real running system-service instance in the meantime.
