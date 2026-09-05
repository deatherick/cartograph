//go:build !darwin && !linux

package sysservice

import "testing"

// TestUnsupportedOS_EveryEntryPointReturnsAClearError verifies the
// fallback build never silently no-ops — see sysservice_other.go's doc.
func TestUnsupportedOS_EveryEntryPointReturnsAClearError(t *testing.T) {
	if _, err := FilePath(); err == nil {
		t.Error("expected FilePath to return an error on an unsupported OS")
	}
	if err := Install(Config{CtxdPath: "/bin/true", WebAddr: "127.0.0.1:7420"}); err == nil {
		t.Error("expected Install to return an error on an unsupported OS")
	}
	if err := Uninstall(); err == nil {
		t.Error("expected Uninstall to return an error on an unsupported OS")
	}
	if _, err := CheckStatus(); err == nil {
		t.Error("expected CheckStatus to return an error on an unsupported OS")
	}
}
