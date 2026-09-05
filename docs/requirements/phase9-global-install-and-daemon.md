# Requirements: global installation and system-level daemon (Phase 9)

Captured ahead of the phase that implements it — the same pattern
`docs/requirements/phase6-web-ui.md` already uses: write down what the user actually asked for in
full, so it isn't reconstructed from memory later, without starting the implementation before its
phase arrives. **Nothing in this document is built yet — reviewed and refined on 2026-09-05 at
the user's explicit request, still deliberately not started.** Phase 3d (immediately following
this capture) builds the daemon's watching/incremental-indexing *logic*; this document is about
how that daemon gets **installed and run persistently at the system level**, which is Phase 9
scope (hardening, installer, distribution) per `docs/MVP.md`.

## What has shipped since this was first captured (2026-08-29) — context for the review below

Everything below is REAL, DONE infrastructure this Phase 9 work will build on top of — named here
so the rest of this document can be concrete about what's still a genuine open question versus
what already has a real, working answer elsewhere in the codebase:

- **Phase 3d, true incremental indexing** (ADR-0012, ADR-0020) — `ctxd` is real, watches and
  re-indexes incrementally, not a placeholder.
- **Multi-project daemon** (ADR-0019) — `ctxd <path> [<path>...]` already watches several projects
  concurrently from one process; each `<path>` argument is already resolved through the project
  registry below (`cmd/ctxd/main.go`'s own doc comment references it directly).
- **The project registry** (ADR-0016, `internal/project`) — `ctx project add/list/remove` already
  persists a name→path mapping at **`~/.cartograph/projects.json`**, a real file at a real,
  existing path, not a placeholder location. This directly answers part of "explicitly not decided
  here" below (see that section).
- **A working, read-only HTTP API** (ADR-0013/0015/0018/0019, `internal/httpserver`) — `ctxd --web`
  already serves `/api/projects`, `/api/stats`, `/api/graph`, `/api/find`, `/api/inspect`,
  `/api/related`, `/api/impact`, `/api/source`, `/api/operations` to a running daemon. This is a
  real, precedented answer to "can `ctx` talk to a running `ctxd` over HTTP" (yes, today, for
  reads) — see "Explicitly not decided here" below for what this does and doesn't settle.
- **The one still-open gap this surfaces most directly**: `docs/MVP.md`'s own known-issues list
  already names it — "a real `ctxd project add/list` that adds/removes a project from an
  *already-running* daemon (today the project list is fixed at `ctxd` startup)." This document's
  "How `ctx init` and the daemon connect" section below is the same gap, from the installer's
  point of view instead of the CLI's.

## The user's request, verbatim intent

Today, using Cartograph means cloning the repo and running `./bin/ctx` with a relative path —
fine for developing Cartograph itself, wrong for a real user. The request was explicit: **`ctx`
should be a global command**, installed once, usable from any directory on the machine, the way
`git`, `docker`, or a Homebrew-installed CLI already work — not something invoked as
`./bin/ctx <path/to/some/project>` from inside this project's own checkout. Alongside that, the
installer should **also set up the daemon (`ctxd`) as a persistent, system-level service** — not
something a user manually starts and keeps a terminal open for. And critically: **the only thing
that should exist inside a user's project directory is their local configuration** —
`.cartograph.json` (ADR-0011) — nothing else. Every derived artifact (the index snapshot, the
daemon's per-project state, logs) stays out of the project tree entirely.

## What "global install" means concretely

- A one-command installer (`curl -fsSL https://.../install.sh | sh`, and/or a Homebrew tap —
  `brew install cartograph/tap/cartograph`, mirroring the mechanism `docs/MVP.md`'s Phase 9 entry
  already names) that:
  - Downloads/builds the `ctx` and `ctxd` binaries for the host OS/arch.
  - Places them on `PATH` (`/usr/local/bin`, or a user-local `~/.local/bin` / `~/go/bin`-style
    location when the user has no admin rights — the installer should not require `sudo` by
    default).
  - Never requires the user to clone this repository or have Go installed.
- After install, `ctx index ~/any/project/anywhere` works immediately, with no `./bin/` prefix and
  no dependency on being inside `~/code/cartograph`.

## What "daemon as a system-level service" means concretely

- The installer registers `ctxd` as a **persistent background service** using each OS's native
  mechanism — a macOS `launchd` user agent (`~/Library/LaunchAgents/com.cartograph.ctxd.plist`,
  `launchctl load`), a Linux `systemd --user` unit (`~/.config/systemd/user/ctxd.service`,
  `systemctl --user enable --now`) — not a shell script the user has to remember to re-run after a
  reboot, and not something requiring root just to watch the user's own files.
- Once installed, `ctxd` starts at login and stays running with ~0% idle CPU (the same target
  Phase 3d's own daemon logic already commits to), watching whichever projects have been
  registered with it.
- Uninstalling reverses this cleanly (`launchctl unload` / `systemctl --user disable`) — the
  installer is responsible for teardown, not just setup.

## What stays local to a project, and what doesn't

| Lives in the project directory | Lives outside it |
|---|---|
| `.cartograph.json` — language selection (ADR-0011), the only project-level setting today | `~/.cartograph/<repo>-<hash>/` — the binary snapshot, session ledger (already the case since ADR-0005/0003's `store.RepoDir`; this requirement makes it a hard invariant, not an implementation detail) |
| (nothing else) | `~/.cartograph/projects.json` — which projects are registered at all (ADR-0016, real, already the answer to "which projects does the daemon know about" — see the section above) |
| | The daemon's own per-project watch state (file hashes, debounce timers, exclusion/quarantine state — Phase 3d) |
| | Daemon logs, PID/lock file, launch-agent/service-unit definitions — still a real Phase 9 implementation decision (XDG-style `~/.config/cartograph/`+`~/.cache/cartograph/`? reuse `~/.cartograph/` for these too, alongside `projects.json` and the snapshots?), narrower now that `projects.json`'s own location is already settled by ADR-0016, not invented here |

The `.cartograph.json` / `~/.cartograph/` split already exists (ADR-0005 chose a derived,
disposable cache outside the repo specifically so deleting it is always safe); this requirement
is that principle extended to the daemon's own state, not a new one.

## How `ctx init` and the daemon connect

Both halves of this connection already exist independently today — `ctx init` (ADR-0011) writes
`.cartograph.json`; `ctx project add` (ADR-0016) writes an entry to `~/.cartograph/projects.json`;
`ctxd <path>...` (ADR-0019) watches whatever paths/names it's given at startup, each resolved
through that same registry. **What's still missing is the live wire between them**: adding a
project to the registry while a global `ctxd` is already running does not make that `ctxd` start
watching it — the process would need to be restarted with the new path added to its argument
list. Two concrete shapes for closing this, neither built, both worth naming explicitly now that
the pieces around them are real (not two hypothetical designs in a vacuum):

1. **`ctxd` reads `projects.json` itself, not just its own argv.** At startup (or once installed
   as the system-level service this document is about), `ctxd` run with NO path arguments watches
   every project currently in the registry, and picks up additions by polling the registry file
   for changes (the same debounce/re-check pattern its own file watcher already uses for source
   changes) — no new IPC needed, `ctx project add` just writes a file the daemon already knows how
   to re-read.
2. **A write-capable HTTP endpoint.** `internal/httpserver` already serves a real, working
   READ-ONLY API to a running `ctxd` (`/api/projects` et al., listed above) — a `POST
   /api/projects` (add) / `DELETE /api/projects/<name>` (remove) extending that same server would
   let `ctx project add` (or `ctx init`) notify an already-running daemon directly, no polling
   delay. Needs the daemon's web server to be listening in every real deployment for this to work
   universally (today `--web` is optional — a decision this document's installer design should
   make explicit, not inherit by accident).

Option 1 is simpler and needs no protocol design; option 2 is more immediate and reuses a
component that already exists for a different reason. Not decided here — a real Phase 9
implementation choice, now made concrete instead of an open blank.

## Explicitly not decided here

- **Whether the daemon talks to `ctx` over a Unix socket, a local HTTP port, or something else** —
  narrower than when this was first written: a real, working HTTP transport already exists
  (`internal/httpserver`, today read-only, optional via `--web`), so this is no longer "pick a
  transport from scratch" but specifically "extend the existing HTTP API to accept writes, make
  `--web` non-optional for a system-service install, or build something else instead" — see "How
  `ctx init` and the daemon connect" above for the two shapes actually on the table.
- Exact installer mechanism (shell script vs. Homebrew tap vs. both) — both are plausible; `docs/MVP.md`'s
  existing Phase 9 entry already names "Homebrew tap + script," kept as-is here.
- Windows support — out of scope until a real need appears (this project's CI matrix is macOS +
  Linux only today).

## Status

Requirement captured 2026-08-29; reviewed and refined 2026-09-05 (Go, C#, and Python extraction —
Phase 3a/3b/3c — and the Similarity Engine's identifier normalization — ADR-0025 — landed in
between); **built the same day, at the user's own explicit follow-up request** ("hagamos la fase 9
de una vez y cerremos el tema y las preguntas abiertas") after the review pass above had already
confirmed it should stay documentation-only for the moment. See **ADR-0026** for the full
implementation record: both concrete shapes this document named for "how `ctx init` and the
daemon connect" were resolved (polling `projects.json` was chosen over a write-capable HTTP
endpoint); `ctx service install/uninstall/status` (`internal/sysservice`) implements "daemon as a
system-level service" for macOS (`launchd`) and Linux (`systemd --user`); `install.sh` +
`.github/workflows/release.yml` implement "global install." `v0.1.0` was tagged and released the
same day, at a further, separate explicit go-ahead from the user — `install.sh` verified live
end-to-end against the real published release (see ADR-0026's "Update: v0.1.0 released"). One
thing remains deliberately undone: actually running `ctx service install` on a live machine (every
code path is unit-tested with a fake command runner, but a live launchd/systemd registration is a
real, persistent system change this ADR's author did not take unilaterally) — see ADR-0026's own
"what still needs a human decision."
