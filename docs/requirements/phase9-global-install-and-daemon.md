# Requirements: global installation and system-level daemon (Phase 9)

Captured ahead of the phase that implements it — the same pattern
`docs/requirements/phase6-web-ui.md` already uses: write down what the user actually asked for in
full, so it isn't reconstructed from memory later, without starting the implementation before its
phase arrives. **Nothing in this document is built yet.** Phase 3d (immediately following this
capture) builds the daemon's watching/incremental-indexing *logic*; this document is about how
that daemon gets **installed and run persistently at the system level**, which is Phase 9 scope
(hardening, installer, distribution) per `docs/MVP.md`.

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
| (nothing else) | The daemon's own per-project watch state (file hashes, debounce timers, exclusion/quarantine state — Phase 3d) |
| | Daemon logs, PID/socket file, its own config (which projects it watches) — a single global location (XDG-style: `~/.config/cartograph/`, `~/.cache/cartograph/`, or reusing `~/.cartograph/` for all of it — a Phase 9 implementation decision, not decided here) |

The `.cartograph.json` / `~/.cartograph/` split already exists (ADR-0005 chose a derived,
disposable cache outside the repo specifically so deleting it is always safe); this requirement
is that principle extended to the daemon's own state, not a new one.

## How `ctx init` and the daemon connect (once both exist)

Once Phase 3d's daemon logic exists and Phase 9 installs it globally, `ctx init` (ADR-0011) is the
natural place to also **register** a project with the running daemon — so a user runs the wizard
once and the project is both configured (`.cartograph.json`) and watched (via the daemon), without
a separate registration step. This document does not specify that command's exact shape (`ctxd
project add`? an RPC `ctx init` makes to a running daemon?) — that is Phase 9 implementation work,
captured here only as the intended connection point.

## Explicitly not decided here

- Whether the daemon talks to `ctx` over a Unix socket, a local HTTP port, or something else —
  Phase 9 implementation.
- Exact installer mechanism (shell script vs. Homebrew tap vs. both) — both are plausible; `docs/MVP.md`'s
  existing Phase 9 entry already names "Homebrew tap + script," kept as-is here.
- Windows support — out of scope until a real need appears (this project's CI matrix is macOS +
  Linux only today).

## Status

Requirement captured, 2026-08-29. Tracked in `docs/MVP.md`'s deferred list under Phase 9. Not
started — Phase 3d (daemon watching/incremental-indexing logic, no system-service installation)
is next.
