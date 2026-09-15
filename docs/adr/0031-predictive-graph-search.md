# ADR-0031: Predictive search for the Graph view's toolbar

- **Status**: Accepted
- **Date**: 2026-09-15
- **Related**: ADR-0006 (Phase 1's exact-match `Find`/`/api/find`, and the explicit deferral of
  fuzzy/full-text search this ADR partially revisits), ADR-0030 (the in-canvas node finder — a
  different, purely client-side search over nodes already loaded, not extended here)

## Context

Direct user feedback: "el search de graph no me gusta porque asume que uno se sabe los nombres
exactos de las funciones... me gustaria mejorar bastante ese buscador para que sea mas
predictivo" (the Graph search assumes you already know the exact function name; wants it far more
predictive).

The toolbar's "center the graph on" search (`EntityGraphPanel`'s `onSearchSubmit`) calls
`svc.Find`, which does an **exact** bare-name (or exact qualified-name) match — reasonable once you
already know the name, useless while still guessing at it. Nothing surfaced until Enter was
pressed, and a near-miss just produced "No entity named X found."

## Decision: a live substring-match autocomplete, not fuzzy/typo-tolerant ranking

Added `Service.Suggest` (backend) and `/api/suggest` (HTTP), and rebuilt the toolbar search as a
real autocomplete on top of it:

- **Matching**: case-insensitive substring on the bare name — the same rule `EntityTable`'s and
  ADR-0030's in-canvas node finder already use, extended here to the whole indexed snapshot
  instead of only whatever's already on screen. Deliberately **not** fuzzy/typo-tolerant scoring
  or an embeddings-based search — ADR-0006 explicitly deferred that, and a plain substring match is
  a small, honest step beyond `Find`'s exact match, not a reopening of that decision. It stays
  fully deterministic: the same query always returns the same entities in the same order, no
  model, no guessing.
- **Ranking**: exact (case-insensitive) match first, then prefix match, then any other substring
  match, each tier broken by name then file — deterministic, not alphabetical noise.
- **Kind filter**: an optional `kind` parameter narrows to one `model.Kind` (Function, Class,
  Method, …) — an exact filter, not itself fuzzy — surfaced in the UI as a row of toggle pills atop
  the dropdown, so a `run` vs `runTask` vs a `RunTask` class collision is easy to break by eye
  instead of by typing more of the name.
- **UI**: typing debounces (150ms) into a floating dropdown below the input, showing each match's
  kind dot + kind label + name + file. Arrow keys move a highlight, Enter picks the highlighted
  match, Escape closes it, a click picks by mouse. Picking navigates straight to that entity — no
  ambiguous-name detour, since a suggestion is already one specific, resolved entity.
- **The old exact-match submit path is kept**, not replaced: plain Enter with nothing highlighted
  still calls `api.find` and falls back to the existing ambiguous-candidate picker. Someone who
  already knows the exact (or qualified `file#Name`) name loses nothing.

## A real bug found live, and its fix

Picking a suggestion sets `searchInput` to the picked entity's name (so the input reflects what's
now centered) — which re-triggered the same debounced-suggest effect on that change, and the
resulting single exact match reopened the dropdown right on top of the graph it had just navigated
to. Fixed with a `skipNextSuggestRef` flag set immediately before any *programmatic*
`setSearchInput` call (only `pickSuggestion` does this), checked at the top of the effect to skip
exactly one re-run — a keystroke from the user still triggers normally, a picked result does not
re-trigger itself.

## What this is explicitly NOT

- **Not fuzzy or typo-tolerant.** No Levenshtein distance, no ranking model, no FTS5 — see ADR-0006
  for why that stays out of scope. This is a strict substring match, always explainable by reading
  the query back against the result.
- **Not a replacement for ADR-0030's in-canvas node finder.** That answers "where, among what's
  already loaded, is X" (client-side, zero network). This answers "what, across the whole indexed
  project, resembles what I'm typing" (server-side, a fresh `/api/suggest` call per query) — a
  different question, kept as a different, complementary UI element.

## Verification

Backend: new `TestService_Suggest_*` (ranking tiers, kind filter, limit, empty query) and
`TestHTTPServer_Suggest_*` (substring match across entities, kind filter, missing `?q=` is 400) —
all passing alongside the full existing suite (`go build/vet/test -race`, `golangci-lint run`, 0
issues).

Frontend live-tested with Playwright against a throwaway `ctxd` built from this branch (isolated
`$HOME`, separate port, indexing this same repo — the real running production `ctxd` predates
`/api/suggest` and would otherwise 200+return its SPA fallback for the unmatched route, the same
false-positive trap ADR-0030's sequence-diagram exploration first documented): typing a partial
name populates the dropdown with real ranked matches, the kind filter pills correctly narrow
results, ArrowDown/Enter and mouse-click selection both navigate and correctly close the dropdown
afterward (the bug above was caught and fixed during this pass), and the pre-existing exact-match
ambiguous-picker fallback (tested via Escape then Enter on an intentionally ambiguous name, "main")
still works unchanged. `npm run build`/`tsc -b`/`npm run lint` all clean — no new warnings beyond
the pre-existing `set-state-in-effect` ones already present elsewhere in this file before this
change.
