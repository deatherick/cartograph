# ADR-0026: Phase 9 — global install and system-level daemon

- **Status**: Accepted, released, and live-verified. `v0.1.0` was tagged and published later the
  same day, at a separate explicit follow-up request ("Haz lo de las versiones del release") —
  see "Update: v0.1.0 released." `ctx service install` was then run for real, at a further
  explicit go-ahead, and `ctxd` is now a real running `launchd` agent on the maintainer's own
  machine, watching real registered projects. Every item under "what still needs a human
  decision" is now closed.
- **Date**: 2026-09-05
- **Related**: `docs/requirements/phase9-global-install-and-daemon.md` (the requirements this
  implements, reviewed and refined earlier the same day), ADR-0016 (project registry), ADR-0019
  (multi-project daemon), ADR-0018 (opstatus/HTTP operations API), `docs/research/edge-case-
  backlog.md` G3 (the build-tag-not-runtime.GOOS lesson this ADR explicitly follows)

## Context

Phase 9 was captured as documentation-only in a prior session, at the user's explicit instruction
not to build it until asked a third time, explicitly. This session: the user asked to review/
refine the requirements doc (no implementation), then — in a separate, later message — asked
explicitly to build it and close its open questions. This ADR is that build.

The requirements doc's own review (same day) had already narrowed the two biggest open questions
to concrete choices instead of a blank slate; this ADR made those choices and built them.

## Decision 1: the daemon-side gap — `ctxd` with no arguments watches the live project registry

The single most-referenced open gap across `docs/MVP.md` and the requirements doc: "a real `ctxd
project add/list` that adds/removes a project from an *already-running* daemon." Of the
requirements doc's two named shapes (poll `projects.json`, or extend the HTTP API to accept
writes), **polling was chosen** — no new protocol, no dependency on `--web` being enabled, and
`ctx project add` already writes a file `ctxd` can just re-read.

`cmd/ctxd` now has two modes:
- **Explicit arguments** (`ctxd <path> [<path>...]`) — ADR-0019's original behavior, byte-for-byte
  unchanged: a fixed project set for the process's whole lifetime.
- **No arguments** — new: seeds its watched set from every entry in `~/.cartograph/projects.json`
  (`internal/project.List`), then `reconcile` re-reads that file every 5 seconds (a deliberately
  coarser cadence than the Web UI's own 3-second live-refresh poll, ADR-0019 — real file I/O
  against a registry that changes far less often than an in-memory stats view) and starts/stops/
  **restarts** individual projects' watcher goroutines as the registry changes — a project
  re-registered at a new path gets its stale watcher stopped and a fresh one started at the new
  location, not silently left watching nowhere real.

`internal/httpserver.ProjectRegistry` (new) replaces the old static `[]Project` slice `New` took:
a thread-safe, mutable registry every handler reads fresh per request (`Snapshot()`), so a project
`Set`/`Remove`-ed while the server is already handling requests is visible immediately, with no
restart — this is what makes the daemon-side reconciliation actually observable through the same
HTTP API the Web UI already uses. An empty registry (a service installed before anything was ever
registered) now reports a clear 503 on every project-scoped endpoint instead of `New`'s old
construction-time panic on an empty slice — a real, intended shape now, not an error case.

## Decision 2: system-level service install — one native mechanism per OS, in its own build-tag file

`internal/sysservice` (`Install`/`Uninstall`/`CheckStatus`/`FilePath`) wraps launchd (macOS) and
systemd `--user` (Linux), wired to `ctx service install|uninstall|status`. Each OS's real
mechanism lives in its own `//go:build darwin` / `//go:build linux` file, never a `runtime.GOOS`
switch in one shared file — this project's own research already catalogued exactly this bug
category (`edge-case-backlog.md` G3, from Grafel's real issue #6218: "the cost model is selected
by build tag, not by `runtime.GOOS`"), so it was followed here rather than rediscovered. A third
file (`sysservice_other.go`, `//go:build !darwin && !linux`) covers everything else — Windows
explicitly, per the requirements doc's own scoping — with a clear, honest "unsupported" error on
every entry point, never a silent no-op.

A service install always sets `--web` (the requirements doc's own review flagged this as a real
decision to make explicit, not inherit by accident): the daemon's HTTP API is the one real,
working way (today) to observe a system-service `ctxd` at all once no terminal is attached to
watch its stdout.

**A real design choice this surfaced, not anticipated going in**: `runCommand` (both OS files) is a
package-level variable, not a hardcoded `exec.Command` call. Neither GitHub Actions' macOS runner
nor its Linux runner has a real per-user login session to register a launchd agent or a systemd
`--user` unit against — meaning **this project's own CI cannot exercise a real service
install/uninstall at all**. Every test substitutes a fake runner that records the exact command
that would have run, verifying the plist/unit file's own content and the install/uninstall control
flow (rejecting a missing `ctxd` binary, an empty `--web`, idempotent uninstall, restart-on-
reinstall) without ever actually invoking `launchctl`/`systemctl`. This is not a workaround for a
testing inconvenience — it is the honest, permanent shape of what CI can and cannot verify for
this specific feature, stated plainly rather than glossed over.

## Decision 3: a real, prebuilt-binary install path, without a release yet to point at

`install.sh` (repo root) detects OS/arch (`darwin`/`linux`, `arm64`/`amd64` — matching this
project's own CI matrix, no cross-compilation attempted given every language extractor's cgo
dependency on tree-sitter), downloads the matching release asset from GitHub Releases, and places
`ctx`/`ctxd`/`ctxmcp` in `~/.local/bin` (no `sudo`, matching the requirements doc's own explicit
ask) — falling back to a clear "build from source" message, not a confusing failure, when no
release exists yet for this platform (verified live: this repo currently has zero releases, so
this fallback path is what actually runs today, not a hypothetical).

`.github/workflows/release.yml` (new, triggered only by pushing a `v*` tag, never by a plain push
to `main`) builds `ctx`/`ctxd`/`ctxmcp` natively on `macos-latest` (darwin/arm64) and
`ubuntu-latest` (linux/amd64) — the same two-OS matrix `ci.yml` already uses — packages each as a
tarball, and publishes them to a GitHub Release with a `checksums.txt`.

A Homebrew tap Formula (`packaging/homebrew/cartograph.rb.template`) is written but explicitly **not
activated** — a real tap needs its own separate repository and a real published release's actual
checksums, neither of which exists yet; both are named in the template's own header as what
activating it requires.

## What still needs a human decision — not built without it

- ~~Cutting an actual `vX.Y.Z` release~~ — **done, same day**: `v0.1.0` tagged and released at a
  separate, explicit follow-up request. See "Update: v0.1.0 released" below.
- ~~Actually running `ctx service install` on a real machine~~ — **done**, at a further explicit
  go-ahead ("Hazlo, así hacemos una prueba real y se mapean los proyectos en tiempo real"): `ctxd`
  is a real, running `launchd` agent on the maintainer's own machine, watching five real
  registered projects (this repo, plus four fixtures), left running to be monitored (~0% idle CPU,
  ~75MB RSS observed). One of the five (`similarity-eval`) was registered WHILE the service was
  already running and picked up live via ADR-0026's own reconciliation — the exact daemon-side
  gap this ADR set out to close, confirmed on a real machine, not just under a temp `$HOME` in CI.
- **Creating a `homebrew-tap` repository** — a new, separate, public GitHub repo — not created
  without being asked to.

## Verification

`go build/vet/test -race/lint` all clean, including on the linux/windows build-tag files
(cross-`go vet` checked, since this machine cannot run Linux-tagged or Windows-tagged tests
directly). New tests: `internal/httpserver` (empty-registry 503, live `Set` visible immediately —
2 new, replacing the old panic-on-empty-slice test, which described a construction-time
restriction ADR-0026 deliberately removed); `cmd/ctxd` (`reconcile`'s start/stop/restart logic
against a REAL `internal/project` registry under a temp `$HOME`, not a mock — 2 new); 13 new tests
across `internal/sysservice`'s three build variants. `install.sh` was run for real against this
repo (no releases published yet) and correctly detected darwin/arm64 and printed the documented
source-build fallback — not a hypothetical, an actual observed run. `cmd/ctxd`'s zero-argument
mode was smoke-tested live under a temporary `$HOME` (never the real user's `~/.cartograph`):
started watching one registered project, a SECOND project added via `ctx project add` while
already running was picked up and served through `/api/projects` within one poll cycle with no
restart, and removing a project via `ctx project remove` stopped its watcher the same way — the
exact daemon-side gap this ADR set out to close, observed working, not just argued to work from
reading the code.

## What this is explicitly NOT

- **Not an activated Homebrew tap.** A template only (`packaging/homebrew/cartograph.rb.template`)
  — needs its own separate `homebrew-tap` repository, not created without being asked to.
- **Not Windows support.** Explicitly out of scope, per the requirements doc's own prior decision.
- **Not a write-capable HTTP endpoint for project registration.** The requirements doc's OTHER
  named option (extending `internal/httpserver` with `POST`/`DELETE /api/projects`) was not built
  — polling was chosen instead (Decision 1); a write endpoint remains a legitimate, un-attempted
  alternative if polling's 5-second latency ever proves too slow for a real use case.
- **Not a live end-to-end launchd/systemd verification on a real machine** — every unit test uses a
  fake command runner; see "What still needs a human decision."

## Update: v0.1.0 released

Same day, in a separate, explicit follow-up message ("Haz lo de las versiones del release"): tagged
and pushed `v0.1.0`, which triggered `.github/workflows/release.yml` for the first time for real —
both `build` jobs (macos-latest → darwin/arm64, ubuntu-latest → linux/amd64) and the `release` job
succeeded, publishing `cartograph_darwin_arm64.tar.gz`, `cartograph_linux_amd64.tar.gz`, and
`checksums.txt` to a real GitHub Release: <https://github.com/deatherick/cartograph/releases/tag/v0.1.0>.

`install.sh` was then run for real (into a throwaway directory, not the actual `PATH`) against
this real release — not a rerun of the earlier "no release exists" fallback path this ADR
originally verified, a genuinely different code path: it downloaded
`cartograph_darwin_arm64.tar.gz`, extracted it, and the resulting `ctx` binary was executed and
printed its real usage output. This is the first time this project's install path has been
verified end-to-end against a real, published artifact rather than argued to work from reading the
code or from the empty-release fallback message.

**Still not done, unchanged from this ADR's original scope**: the Homebrew tap (needs its own
repository), and a live `ctx service install` run on a real machine (registers a real, persistent
background service — still the one item needing the user's own separate go-ahead before this ADR's
author runs it).
