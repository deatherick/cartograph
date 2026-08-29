# ADR-0011: Plug-and-play language architecture, opt-in/opt-out, and the init wizard

- **Status**: Accepted (done)
- **Date**: 2026-08-29
- **Related**: ADR-0010 (Go extractor), ADR-0004 (query-based extraction), docs/MVP.md

## Context

Immediately after ADR-0010 shipped the Go extractor, the user raised a direct architectural
concern rather than a feature request: *"me gusta que el mismo identificador pueda trabajar con
múltiples lenguajes... me gustaría que fueran plug and play, que cada lenguaje fuera fácilmente
agregado al core y no hubiera una dependencia bidireccional entre lenguajes."* Looking at
`internal/resolve/resolve.go` as it stood after ADR-0010 confirmed the concern was justified: the
core resolution pipeline had four separate `if fe.lang == model.LangGo { ... } else { ... }`
branches (in `resolveQualified`, `resolveByReceiverType`, twice in `resolveUnqualified`), and
`Index` carried language-specific config fields (`tsconfig`, `goModule`) directly. Adding a third
language (C#, Python) would have meant a fifth, sixth, seventh branch — a core file that grows
forever and, worse, that anyone fixing one language's resolution logic has to read past another
language's logic to find it. Immediately after that, the user extended the ask twice more: make
language activation **opt-in/opt-out per project** (a lighter run when a language isn't wanted),
and make the whole thing **easy for outside contributors to extend or fix one language** without
touching the others.

## Decision: LanguagePolicy, a real plugin interface

`internal/resolve/langpolicy.go` defines `LanguagePolicy` — six methods (`Lang`,
`SameScopeFiles`, `ResolveQualifiedImport`, `ResolveUnqualifiedImport`, `FollowImportToMethods`,
`IsBuiltin`, `FinalDisposition`) that together cover every point resolve.go's pipeline used to
branch on language. `internal/resolve/resolve.go` was rewritten to dispatch through
`idx.policies[fe.lang]` at each of those points and — this is the part actually enforced, not just
claimed — **never names a specific `model.Lang` value anywhere in the file**.
`TestArchitectureBoundary_CoreNeverBranchesOnLang` (`resolve_test.go`) greps the file's text for
`model.LangTS`/`model.LangGo` and fails the build if either appears, the same discipline
`internal/parser/architecture_test.go` already applies to keep tree-sitter types from leaking past
their wrapper.

TypeScript's and Go's policies are two ordinary files, `lang_ts.go` and `lang_go.go` — nothing
new was invented for the interface's sake; every method's body is code that already existed in
resolve.go before this ADR, moved and given a name. Neither file imports the other, and neither
imports `internal/parser/ts` or `internal/parser/golang` — a policy talks only to the shared
`Index`/`fileEntry` types resolve.go already exposed internally. This is the literal answer to
"no bidirectional dependency between languages": there is no dependency between them at all, only
each depending independently on the same interface.

**Adding a third language now means:** write one `internal/parser/<lang>` extractor (unchanged
from ADR-0010's pattern), write one `internal/resolve/lang_<lang>.go` implementing
`LanguagePolicy`, and add one `Language` value to `internal/index/languages.go`'s `registry()`.
Nothing in `resolve.go`, nothing in `lang_ts.go`, nothing in `lang_go.go` changes. This is also
what makes it realistic for an outside contributor to fix or optimize one language: the blast
radius of a change to Go's resolution policy is one file, verifiable by tests scoped to that file,
with no way to accidentally regress TypeScript's behavior by construction (not just by
discipline).

## Decision: languages are opt-in/opt-out per project, not compiled-in constants

`internal/index/languages.go`'s `Language` struct bundles an extractor, a policy, and a `Detect`
function (a cheap marker-file/extension check, never a full parse) — `registry(root)` is the
**one place** every known language is listed. `internal/index/config.go` adds `.cartograph.json`,
a project-level, git-committable settings file (like `tsconfig.json` or `.eslintrc` — not
`~/.cartograph/`, which holds the disposable derived snapshot): `{"languages": ["go"]}` narrows a
run to exactly those languages. Zero config (`.cartograph.json` absent, or present with no
`languages` key) means "every language `registry()` detects" — a brand-new user never has to run
anything before `ctx index` works.

Verified this actually changes what gets walked, not just what gets reported: indexing a
synthetic two-language fixture (`backend/service.go` + `frontend/app.ts`) with both languages
enabled reports `files: 2`; writing `{"languages": ["typescript"]}` and re-indexing reports
`files: 1` — Go's extractor is never invoked at all, not merely filtered out of the result. This
is the literal "lighter core" the user asked for: an unwanted language costs nothing at index
time, not just at read time.

## Decision: `ctx init`, an intuitive wizard, not a required step

`ctx init <path>` detects languages via `registry(root)`'s `Detect` functions, shows the user
what it found (`[x] typescript` / `[ ] go`), and asks whether to enable everything detected or
type a custom comma-separated list — then writes `.cartograph.json`. Re-running it (or hand-
editing the JSON) is how the choice is changed later, directly answering "que se pueda editar o
configurar después del init." Three things were built in specifically because this tool's primary
consumer is a coding agent, not only a human:

- **`--yes`** and **`--languages a,b`** flags skip every prompt for a scripted/agent invocation.
- **Non-interactive stdin detection** (`isInteractiveStdin`, checking `os.Stdin`'s
  `ModeCharDevice`): an agent or CI script invoking `ctx init` with no flags never blocks waiting
  for input it cannot supply — it gets the zero-config default (every detected language),
  **loudly logged to stdout** ("non-interactive terminal — enabling every detected language
  without prompting"), never a silent hang or a silent guess.
- **`ctx languages <path>`**, a read-only status command showing enabled/detected per language —
  what a user (or an agent orienting itself) checks before deciding whether to run `init` at all.

## What this is not

No SQLite, no plugin *loading* (dynamic `.so`/RPC plugins) — every language still ships compiled
into the `ctx` binary; "plug and play" here means architecturally decoupled and independently
addable at the source level, registered in one list, not a runtime-loadable extension mechanism.
That remains explicitly out of scope until a real need for it (e.g. a genuinely external,
separately-versioned language package) appears — see docs/MVP.md's deferred list.

## Verification

- Every pre-existing TypeScript resolver test (`resolve_test.go`) passes unchanged in substance —
  each now explicitly registers `NewTSPolicy`/`NewGoPolicy` via a small `tsIndex()`/`goIndex()`
  helper, since a language is no longer implicitly active; this made visible, in the tests
  themselves, exactly how much of the old behavior was "TypeScript is just always on" versus
  genuinely core.
- Two new tests: `TestArchitectureBoundary_CoreNeverBranchesOnLang` (the grep, described above)
  and `TestResolve_UnregisteredLanguage_IsUnclassifiedNotGuessed` (a ref whose language has no
  registered policy gets `DispositionUnclassified`, never a bug-rate hit and never a guess at
  another language's rules).
- Re-ran ADR-0010's self-hosting measurement after this refactor (a pure restructuring — no
  resolution behavior was meant to change): still 0.1% bug_rate. `go test ./... -race`,
  `go vet ./...`, and `golangci-lint run ./...` all clean.
