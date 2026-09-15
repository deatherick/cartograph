# ADR-0032: Extending predictive search to the project switcher and Duplicates view

- **Status**: Accepted
- **Date**: 2026-09-15
- **Related**: ADR-0031 (the Graph view's predictive search this generalizes the PATTERN from, not
  the server-backed mechanism), ADR-0027 (Web UI Duplicates view)

## Context

Direct follow-up after ADR-0031: "revisa si es posible agregar este search mejorado en otras
partes que se necesiten, por ejemplo el dashboard principal, el dropdown de proyectos en caso
hubieran muchos, el diff, el duplicate y lugares para agilizar las busquedas, todo en su propio
contexto" — asked to extend the improved search wherever it would genuinely help, each in its own
context, not as one shared, one-size-fits-all component.

A review of every named surface follows, each judged on its own — extending the PATTERN
(predictive, not "type the exact name and hit Enter") does not mean extending the same mechanism
everywhere; ADR-0031 itself already drew that line for the Graph view.

## What changed

**TopBar's project switcher** (`ProjectSwitcher.tsx`, new): was a plain native `<select>` —
unusable to scan past a handful of entries. Now a searchable combobox: click to open, type to
filter (name or repo, case-insensitive substring), arrow keys + Enter or a click to pick, outside-
click to close — the same interaction language ADR-0031's dropdown established, so it feels like
the same product. **Client-side, not `api.suggest`**: `ProjectProvider` already loads the full
project list once up front (`project-context.tsx`), and it's a short, bounded list (registered
daemon projects), not thousands of indexed entities — filtering an array already in memory is
strictly faster and simpler than a debounced network round-trip for this case.

**Duplicates view**: had no search or filter of any kind — every undecided pair rendered at once,
so finding a specific entity's duplicates in a real repo (dozens to low hundreds of pairs) meant
scanning by eye. Added a client-side filter (entity name or qualified name, case-insensitive
substring) over the pairs `usePoll` already holds — the exact same "filter what's already loaded,
no network" reasoning as the project switcher above, and the same rule `EntityTable`'s own search
already uses.

## What was deliberately left unchanged, and why

- **Overview's entity table** (the "dashboard principal"): already has its own instant, client-side
  substring search + kind filter (`EntityTable.tsx`, predates this ADR). It filters the whole
  already-loaded entity list with zero network latency — switching it to `api.suggest` would be a
  strict regression (debounce lag, a round-trip) for no benefit, since the full list is already in
  memory. Nothing to change here; noted so this wasn't silently skipped.
- **The git-diff Impact page** ("el diff"): explicitly and deliberately had its own by-entity search
  input REMOVED in an earlier round, per direct feedback recorded in that file's own header comment
  — "un buscador que no se sabe lo que busca" (a search box with no visible context on what you're
  searching for) was confusing. That workflow now lives in Overview's Impact tab (select a row,
  see its blast radius — full context, no search step). Re-adding a bare entity-name search to this
  page would reverse a considered decision for the same reason it was removed the first time; the
  page's own git-ref input is a different kind of field (a ref, not an entity) and isn't a search
  box at all. Left unchanged.
- **`EntityDetail`/`EntityImpactPanel`**: take an already-resolved entity (name + file) as props by
  design — no search surface exists in either, nothing to extend.

## Verification

Both additions are pure frontend, reusing existing API endpoints (`/api/projects`,
`/api/duplicates`) — no backend change, no new SPA-fallback risk. Live-tested with Playwright
against the real running production `ctxd` (throwaway dev server, read-only): the project switcher
correctly filters a real 6-project registry and switches cleanly; the Duplicates filter correctly
narrows a real 121-pair list to 11 on a substring query. `npm run build`/`tsc -b`/`npm run lint`
all clean — no new warnings beyond the pre-existing ones already present before this change.
