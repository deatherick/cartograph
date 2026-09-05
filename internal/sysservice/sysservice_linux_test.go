//go:build linux

package sysservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner mirrors sysservice_darwin_test.go's own — real systemctl
// calls are never exercised here (a GitHub Actions Linux runner is a
// container with no systemd --user session to register against). Records
// every invocation so a test can assert on the exact command this
// package WOULD have run.
type fakeRunner struct {
	calls [][]string
	err   error
	out   []byte
}

func (f *fakeRunner) run(name string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.out, f.err
}

func withFakeRunner(t *testing.T) *fakeRunner {
	t.Helper()
	f := &fakeRunner{}
	orig := runCommand
	runCommand = f.run
	t.Cleanup(func() { runCommand = orig })
	return f
}

func TestInstall_WritesUnitAndEnablesIt(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := withFakeRunner(t)

	ctxdPath := filepath.Join(t.TempDir(), "ctxd")
	if err := os.WriteFile(ctxdPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := Install(Config{CtxdPath: ctxdPath, WebAddr: "127.0.0.1:7420"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	path, err := FilePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected the unit file to exist at %s: %v", path, err)
	}
	content := string(data)
	for _, want := range []string{ctxdPath, "127.0.0.1:7420", "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(content, want) {
			t.Errorf("unit file missing expected content %q:\n%s", want, content)
		}
	}

	if len(fake.calls) != 2 {
		t.Fatalf("expected exactly two systemctl calls (daemon-reload, enable --now), got %v", fake.calls)
	}
	wantReload := []string{"systemctl", "--user", "daemon-reload"}
	wantEnable := []string{"systemctl", "--user", "enable", "--now", unitName}
	if fmt.Sprint(fake.calls[0]) != fmt.Sprint(wantReload) {
		t.Errorf("got first call %v, want %v", fake.calls[0], wantReload)
	}
	if fmt.Sprint(fake.calls[1]) != fmt.Sprint(wantEnable) {
		t.Errorf("got second call %v, want %v", fake.calls[1], wantEnable)
	}
}

func TestInstall_RejectsEmptyConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withFakeRunner(t)

	if err := Install(Config{CtxdPath: "", WebAddr: "127.0.0.1:7420"}); err == nil {
		t.Error("expected an error for an empty CtxdPath")
	}
	if err := Install(Config{CtxdPath: "/bin/true", WebAddr: ""}); err == nil {
		t.Error("expected an error for an empty WebAddr — a service install must always expose --web (ADR-0026)")
	}
}

func TestInstall_RejectsMissingCtxdBinary(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	withFakeRunner(t)

	if err := Install(Config{CtxdPath: "/no/such/ctxd", WebAddr: "127.0.0.1:7420"}); err == nil {
		t.Error("expected an error when CtxdPath does not exist — never write a unit pointing at nothing")
	}
}

func TestUninstall_RemovesUnitAndDisables(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := withFakeRunner(t)

	ctxdPath := filepath.Join(t.TempDir(), "ctxd")
	if err := os.WriteFile(ctxdPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Install(Config{CtxdPath: ctxdPath, WebAddr: "127.0.0.1:7420"}); err != nil {
		t.Fatalf("Install: %v", err)
	}
	fake.calls = nil

	if err := Uninstall(); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}

	path, _ := FilePath()
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("expected the unit file to be removed after Uninstall, stat error: %v", err)
	}
	var sawDisable bool
	for _, c := range fake.calls {
		if len(c) >= 3 && c[0] == "systemctl" && c[2] == "disable" {
			sawDisable = true
		}
	}
	if !sawDisable {
		t.Errorf("expected a systemctl --user disable call, got %v", fake.calls)
	}
}

func TestUninstall_NeverInstalled_IsANoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := withFakeRunner(t)

	if err := Uninstall(); err != nil {
		t.Errorf("expected Uninstall on a never-installed service to be a no-op, got error: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected no systemctl calls when nothing was ever installed, got %v", fake.calls)
	}
}

func TestCheckStatus_ReportsInstalledAndRunning(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := withFakeRunner(t)

	st, err := CheckStatus()
	if err != nil {
		t.Fatalf("CheckStatus: %v", err)
	}
	if st.Installed {
		t.Error("expected Installed=false before anything was ever installed")
	}

	ctxdPath := filepath.Join(t.TempDir(), "ctxd")
	_ = os.WriteFile(ctxdPath, []byte("#!/bin/sh\n"), 0o755)
	if err := Install(Config{CtxdPath: ctxdPath, WebAddr: "127.0.0.1:7420"}); err != nil {
		t.Fatalf("Install: %v", err)
	}

	fake.err = nil
	st, err = CheckStatus()
	if err != nil {
		t.Fatalf("CheckStatus: %v", err)
	}
	if !st.Installed {
		t.Error("expected Installed=true after Install")
	}
	if !st.Running {
		t.Error("expected Running=true when the fake is-active call succeeds")
	}

	fake.err = fmt.Errorf("exit status 3") // systemd's own "inactive" exit code
	st, err = CheckStatus()
	if err != nil {
		t.Fatalf("CheckStatus: %v", err)
	}
	if st.Running {
		t.Error("expected Running=false when the fake is-active call fails")
	}
}
