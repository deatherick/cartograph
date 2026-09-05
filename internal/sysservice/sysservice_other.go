//go:build !darwin && !linux

package sysservice

import "fmt"

// This build covers every OS other than macOS and Linux — Windows named
// explicitly, per docs/requirements/phase9-global-install-and-daemon.md's
// own "Explicitly not decided here: Windows support — out of scope until
// a real need appears (this project's CI matrix is macOS + Linux only
// today)." A clear, honest "unsupported" error on every entry point,
// never a silent no-op that would let `ctx service install` report
// success while actually doing nothing.
var errUnsupported = fmt.Errorf("sysservice: system-level service installation is not supported on this OS yet (only macOS/launchd and Linux/systemd --user are implemented) — see docs/requirements/phase9-global-install-and-daemon.md")

func FilePath() (string, error)    { return "", errUnsupported }
func Install(cfg Config) error     { return errUnsupported }
func Uninstall() error             { return errUnsupported }
func CheckStatus() (Status, error) { return Status{}, errUnsupported }
