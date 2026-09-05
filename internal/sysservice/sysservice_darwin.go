//go:build darwin

package sysservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// label is the launchd service identifier — also the plist's own
// filename stem (Label and file basename are conventionally the same in
// launchd, though not required; kept identical here to avoid a second
// naming scheme).
const label = "com.cartograph.ctxd"

// plistTemplate is a plain string, not text/template — the values
// substituted (a file path, a host:port) never contain XML metacharacters
// in any real, expected use, and this avoids a template-injection concern
// entirely rather than sanitizing inputs whose only source is the CLI's
// own flag parsing. CtxdPath and WebAddr are still validated (non-empty,
// CtxdPath must exist) before this is ever formatted — see Install below.
const plistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>--web</string>
		<string>%s</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`

// FilePath returns where the launchd plist lives —
// ~/Library/LaunchAgents/com.cartograph.ctxd.plist, the standard
// per-user (not system-wide, no root needed) LaunchAgents location.
func FilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sysservice: resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "LaunchAgents", label+".plist"), nil
}

// logDir returns where ctxd's stdout/stderr are redirected once running
// as a service (no terminal is attached to see them otherwise) —
// ~/Library/Logs/cartograph/, the conventional per-user log location on
// macOS, kept separate from ~/.cartograph/ (ADR-0011's own "only
// .cartograph.json lives in a project directory, everything derived lives
// under ~/.cartograph/" invariant is about PROJECT state, not the
// daemon's own process logs — a real, deliberate distinction, not an
// oversight: logs belong alongside where every other macOS app puts them,
// so `log show`/Console.app find them the way a user already expects).
func logDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sysservice: resolving home directory: %w", err)
	}
	return filepath.Join(home, "Library", "Logs", "cartograph"), nil
}

// Install writes the launchd plist and loads it — `launchctl load -w`,
// the traditional (still fully functional on every macOS version this
// project's CI targets) mechanism; the newer `launchctl bootstrap`/
// `enable` pair is more precise about targeting the current user's GUI
// domain but requires knowing the caller's UID and session type, real
// extra complexity for no behavioral difference in the single-user,
// per-user-agent case this package only ever targets. `-w` (write
// overrides.plist) ensures a previous `launchctl unload` (without `-w`,
// which Uninstall does NOT use — see its own doc) doesn't leave the
// service disabled after a fresh Install.
func Install(cfg Config) error {
	if cfg.CtxdPath == "" {
		return fmt.Errorf("sysservice: CtxdPath must not be empty")
	}
	if cfg.WebAddr == "" {
		return fmt.Errorf("sysservice: WebAddr must not be empty for a service install")
	}
	if _, err := os.Stat(cfg.CtxdPath); err != nil {
		return fmt.Errorf("sysservice: ctxd binary %s: %w", cfg.CtxdPath, err)
	}

	dir, err := logDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("sysservice: creating log directory %s: %w", dir, err)
	}
	outLog := filepath.Join(dir, "ctxd.log")
	errLog := filepath.Join(dir, "ctxd.err.log")

	path, err := FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sysservice: creating %s: %w", filepath.Dir(path), err)
	}
	content := fmt.Sprintf(plistTemplate, label, cfg.CtxdPath, cfg.WebAddr, outLog, errLog)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("sysservice: writing %s: %w", path, err)
	}

	if out, err := runCommand("launchctl", "load", "-w", path); err != nil {
		return fmt.Errorf("sysservice: launchctl load -w %s: %w (output: %s)", path, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall unloads the service and removes its plist — teardown is this
// package's own responsibility, not left to the caller
// (docs/requirements/phase9-global-install-and-daemon.md's own explicit
// requirement: "Uninstalling reverses this cleanly"). A plist that
// doesn't exist (never installed, or already removed) is a no-op, not an
// error — the same "removing something not there still achieves the
// caller's goal" convention internal/project.Remove already uses.
func Uninstall() error {
	path, err := FilePath()
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(path); statErr != nil {
		if os.IsNotExist(statErr) {
			return nil
		}
		return fmt.Errorf("sysservice: checking %s: %w", path, statErr)
	}

	// unload's own error is deliberately not fatal to Uninstall as a
	// whole: an already-unloaded (e.g. crashed, or unloaded manually
	// outside this tool) service reports a non-zero exit here too, and
	// the file removal below is what actually matters for "uninstalled"
	// to be true afterward.
	_, _ = runCommand("launchctl", "unload", path)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sysservice: removing %s: %w", path, err)
	}
	return nil
}

// CheckStatus reports whether the plist file exists and whether launchd
// currently has it loaded (`launchctl list <label>` exits 0 with the
// service's info if loaded, non-zero if not — the standard way to check,
// not a fragile text-match on its output).
func CheckStatus() (Status, error) {
	path, err := FilePath()
	if err != nil {
		return Status{}, err
	}
	var st Status
	if _, statErr := os.Stat(path); statErr == nil {
		st.Installed = true
	} else if !os.IsNotExist(statErr) {
		return Status{}, fmt.Errorf("sysservice: checking %s: %w", path, statErr)
	}

	out, runErr := runCommand("launchctl", "list", label)
	st.Detail = strings.TrimSpace(string(out))
	st.Running = runErr == nil
	return st, nil
}
