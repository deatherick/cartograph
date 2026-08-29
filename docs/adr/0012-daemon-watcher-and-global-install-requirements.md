# ADR-0012: Daemon watcher (Phase 3d vertical slice) + global-install requirements captured

- **Status**: Accepted (watcher done; global install captured as requirements only, not built)
- **Date**: 2026-08-29
- **Related**: ADR-0011 (plugin language architecture), docs/MVP.md, docs/research/05-watcher-and-invalidation.md

## Context

Immediately after ADR-0011 shipped, the user raised a separate, explicit request: `ctx` should be
installable as a global system command (not `./bin/ctx` from inside this repo's checkout), the
installer should also set up `ctxd` as a persistent system-level service, and the *only* thing
that should ever live inside a user's project directory is `.cartograph.json` — everything else
derived stays outside it. The user was explicit that this should be **documented only** for now,
and asked to continue with "the next phase" — Phase 3d (daemon, incremental indexing, file
watcher) per `docs/MVP.md`'s own roadmap.

This ADR covers two distinct pieces of work done in response: the global-install requirement
(captured, not implemented) and Phase 3d's actual first vertical slice (watcher + automatic
re-index, implemented and tested).

## Global install: requirements captured, not implemented

Per the explicit "solo documéntalo" instruction, nothing was built here. The full requirement —
one-command global installer, `launchd`/`systemd --user` service registration for `ctxd`, and the
hard invariant that `.cartograph.json` is the only project-local artifact — is written up in
[`docs/requirements/phase9-global-install-and-daemon.md`](../requirements/phase9-global-install-and-daemon.md),
tracked in `docs/MVP.md`'s Phase 9 entry, mirroring exactly how `docs/requirements/phase6-web-ui.md`
already captured UI requirements ahead of the phase that implements them.

## Phase 3d vertical slice: `internal/watch` + `cmd/ctxd`

`internal/watch` wraps `github.com/fsnotify/fsnotify` (kqueue on macOS, inotify on Linux) into a
directory-tree watcher that emits one debounced "something changed" signal per burst of activity
— never one event per file, and honoring `internal/exclude.SkipDir`'s exact same directory
blacklist a real index walk uses, so the watcher never opens a descriptor on `node_modules`,
`vendor`, `.git`, etc. Newly created subdirectories are added to the watch dynamically (fsnotify
is not recursive on its own).

`cmd/ctxd` — previously an unimplemented stub — now does something real: index once via
`service.Index` (the same call `ctx index` makes), then watch the same path and re-index
automatically whenever `internal/watch` signals a change, printing what changed each time, until
`Ctrl+C`/`SIGTERM`.

### The deliberate scoping decision: full re-index, not per-file incremental

The project plan's own stated restriction is "never a full reindex for a small change." This V0
does exactly that anyway, for one measured reason: on this project's own real source (58 files
across two languages), a full index run takes well under a second (ADR-0010's self-hosting
numbers). A debounced full reindex delivers the actual user-facing value — no staleness detection,
a known issue on `docs/MVP.md`'s list since Phase 1 — at negligible cost at today's scale.

True per-file incremental indexing is real, substantially harder work, not a small optimization:
`internal/resolve`'s same-file/same-package/import-table tiers mean one file's export list
changing can affect edges the resolver computed for OTHER files that import it. Correctly
invalidating only what actually changed needs content-hash-based re-anchoring and a real
dependency-tracking scheme (the `F1`-`F9` cases already catalogued in
`docs/research/edge-case-backlog.md` from studying Grafel's own incremental-indexing bugs) —
deferred, not silently skipped. This is recorded as a known issue on `docs/MVP.md`, not hidden
behind the daemon "working."

### Verified end-to-end, not just unit-tested

Beyond `internal/watch`'s own tests (file change fires an event, a burst of writes coalesces into
one event, a newly created subdirectory is watched too — all pass), `ctxd` was run against a real
two-language synthetic fixture: started the daemon, appended a new function to a Go file while it
was running, and confirmed the daemon's own log showed a debounced "change detected" reindex with
the entity count moving from 4 to 5 — the actual content change reflected automatically, with no
manual `ctx index` re-run.

### Known, documented gaps carried forward from `docs/research/05`

- **Descriptor scaling**: fsnotify's kqueue backend costs one descriptor per watched directory
  (not per file, unlike the *original* Grafel finding this project's research notes describe,
  which was about a naive one-descriptor-per-file scheme) — still worth revisiting with a real
  FSEvents binding before this runs against a repo with thousands of directories, per
  `docs/research/05-watcher-and-invalidation.md`'s own recommendation. Not yet a problem at any
  scale this project has actually measured.
- **No exclusion churn quarantine, no `.git/HEAD` branch-change poller, no crash-reconcile-on-
  restart** — the `F`/`G` sections of `docs/research/edge-case-backlog.md` catalogue these
  real Grafel-derived cases; none are implemented yet. This V0 is "watch and reindex," not the
  full daemon lifecycle the master plan eventually wants.
- **No multi-project registry** (`ctxd project add/list`) — `ctxd` takes exactly one path and
  runs in the foreground for it. Multi-project management remains explicitly deferred.

## Verification

`go build ./...`, `go vet ./...`, `go test ./... -race` (including three new `internal/watch`
tests), and `golangci-lint run ./...` all clean. The end-to-end daemon run described above was
performed manually against a disposable synthetic fixture, not committed to this repo.
