// Package sysservice installs/uninstalls/checks `ctxd` as a persistent,
// system-level background service — the part of Phase 9
// (docs/requirements/phase9-global-install-and-daemon.md, ADR-0026) that
// makes "ctxd starts at login and stays running" true without a user
// manually invoking it and keeping a terminal open.
//
// One native mechanism per OS, in its own build-tag-scoped file
// (sysservice_darwin.go, sysservice_linux.go, sysservice_other.go) —
// NOT a runtime.GOOS switch in one shared file. This mirrors a real,
// previously-found bug category this project's own research already
// catalogued (docs/research/edge-case-backlog.md G3, from Grafel's own
// #6218: "the cost model is selected by build tag, not by runtime.GOOS")
// — a build-tag split means a platform's binary physically cannot
// contain another platform's control-mechanism code, not just avoid
// calling it by convention.
//
// Windows is explicitly out of scope (docs/requirements/phase9-global-
// install-and-daemon.md's own "Explicitly not decided here" section;
// this project's CI matrix is macOS + Linux only) — sysservice_other.go
// covers it (and anything else) with a clear, honest "unsupported"
// error, never a silent no-op.
package sysservice

import "os/exec"

// Config is what Install needs to know to register ctxd as a service.
type Config struct {
	// CtxdPath is the absolute path to the ctxd binary to run — resolved
	// by the caller (cmd/ctx's `ctx service install`), not this package:
	// finding "the right ctxd" (same directory as the running `ctx`
	// binary? somewhere on PATH?) is an installer-UX decision, not a
	// service-registration one.
	CtxdPath string
	// WebAddr is the --web address ctxd is started with — always set,
	// never empty, for a service install: docs/requirements/phase9's own
	// review (ADR-0026) named this explicitly — `--web` being optional
	// for an interactive `ctxd` run is fine, but a system-service install
	// needs its HTTP API listening unconditionally, since that's the
	// one real, working way (today) for `ctx` to observe a running
	// daemon at all (operations status, the live project registry
	// endpoints) once no terminal is attached to watch its stdout.
	WebAddr string
}

// Status is what CheckStatus reports.
type Status struct {
	// Installed reports whether the service definition file exists
	// (the plist/unit) — independent of whether it's currently loaded/
	// running (Running below), since a file can exist but fail to load
	// (a stale reference to a deleted ctxd binary, say).
	Installed bool
	// Running reports whether the OS's own service manager currently
	// considers it loaded/active.
	Running bool
	// Detail is the raw, unparsed output from the OS command used to
	// check (launchctl list / systemctl --user status) — shown to the
	// user on `ctx service status` for troubleshooting, never parsed
	// further by this package beyond the Installed/Running booleans
	// above, which come from checking well-defined conditions (file
	// existence, exit code), not by pattern-matching this text.
	Detail string
}

// runCommand executes an external OS command and returns its combined
// output — a package-level variable, not a hardcoded exec.Command call,
// specifically so tests (sysservice_darwin_test.go,
// sysservice_linux_test.go) can substitute a fake that never actually
// invokes launchctl/systemctl. Real system-service registration cannot be
// exercised in CI at all (GitHub Actions' macOS runners have no real user
// login session for launchctl to register against; its Linux runners are
// containers with no systemd --user session either) — every test that
// needs to verify what COMMAND this package would have run substitutes
// this variable instead of skipping the assertion entirely.
var runCommand = func(name string, args ...string) ([]byte, error) {
	return exec.Command(name, args...).CombinedOutput() //nolint:gosec // name/args are this package's own fixed launchctl/systemctl invocations, never user-supplied
}
