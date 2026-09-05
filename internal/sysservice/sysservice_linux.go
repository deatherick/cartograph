//go:build linux

package sysservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// unitName is the systemd --user unit's identifier — also the unit
// file's own filename.
const unitName = "ctxd.service"

// unitTemplate is a plain string, not text/template — see
// sysservice_darwin.go's plistTemplate doc for why (no template-injection
// surface worth guarding against inputs that only ever come from this
// package's own validated Config, not arbitrary user text).
const unitTemplate = `[Unit]
Description=Cartograph context daemon

[Service]
ExecStart=%s --web %s
Restart=on-failure

[Install]
WantedBy=default.target
`

// FilePath returns where the systemd --user unit lives —
// ~/.config/systemd/user/ctxd.service, the standard per-user (no root
// needed) unit location systemd itself defines.
func FilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("sysservice: resolving home directory: %w", err)
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

// Install writes the systemd --user unit, reloads systemd's view of unit
// files, then enables and starts it in one step (`--now`) — so `ctx
// service install` leaves ctxd both registered for every future login
// AND actually running immediately, matching launchd's `RunAtLoad`
// (sysservice_darwin.go) doing the same on macOS.
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

	path, err := FilePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("sysservice: creating %s: %w", filepath.Dir(path), err)
	}
	content := fmt.Sprintf(unitTemplate, cfg.CtxdPath, cfg.WebAddr)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return fmt.Errorf("sysservice: writing %s: %w", path, err)
	}

	if out, err := runCommand("systemctl", "--user", "daemon-reload"); err != nil {
		return fmt.Errorf("sysservice: systemctl --user daemon-reload: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	if out, err := runCommand("systemctl", "--user", "enable", "--now", unitName); err != nil {
		return fmt.Errorf("sysservice: systemctl --user enable --now %s: %w (output: %s)", unitName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Uninstall disables/stops the unit and removes its file — a unit file
// that doesn't exist (never installed, or already removed) is a no-op,
// not an error, matching sysservice_darwin.go's Uninstall exactly.
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

	// disable --now's own error is deliberately not fatal to Uninstall as
	// a whole — same reasoning as launchctl unload in
	// sysservice_darwin.go: the file removal below is what actually makes
	// "uninstalled" true.
	_, _ = runCommand("systemctl", "--user", "disable", "--now", unitName)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("sysservice: removing %s: %w", path, err)
	}
	_, _ = runCommand("systemctl", "--user", "daemon-reload")
	return nil
}

// CheckStatus reports whether the unit file exists and whether systemd
// currently considers it active (`systemctl --user is-active` exits 0 and
// prints "active" when running, non-zero/"inactive" otherwise — checked
// by exit code, not by pattern-matching the printed word).
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

	out, runErr := runCommand("systemctl", "--user", "is-active", unitName)
	st.Detail = strings.TrimSpace(string(out))
	st.Running = runErr == nil
	return st, nil
}
