//go:build darwin

package sysservice

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner replaces runCommand for the duration of one test — real
// launchctl calls are never exercised here (see sysservice.go's own doc
// for why: no GitHub Actions macOS runner has a real per-user launchd GUI
// session to register against). Records every invocation so a test can
// assert on the exact command this package WOULD have run.
type fakeRunner struct {
	calls [][]string
	err   error // returned by every call, if set
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

func TestInstall_WritesPlistAndLoadsIt(t *testing.T) {
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
		t.Fatalf("expected the plist to exist at %s: %v", path, err)
	}
	content := string(data)
	for _, want := range []string{label, ctxdPath, "127.0.0.1:7420", "RunAtLoad", "KeepAlive"} {
		if !strings.Contains(content, want) {
			t.Errorf("plist missing expected content %q:\n%s", want, content)
		}
	}

	if len(fake.calls) != 1 {
		t.Fatalf("expected exactly one launchctl call, got %v", fake.calls)
	}
	got := fake.calls[0]
	want := []string{"launchctl", "load", "-w", path}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("got launchctl call %v, want %v", got, want)
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
		t.Error("expected an error when CtxdPath does not exist — never write a plist pointing at nothing")
	}
}

func TestUninstall_RemovesPlistAndUnloads(t *testing.T) {
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
		t.Errorf("expected the plist to be removed after Uninstall, stat error: %v", err)
	}
	if len(fake.calls) != 1 || fake.calls[0][0] != "launchctl" || fake.calls[0][1] != "unload" {
		t.Errorf("expected exactly one launchctl unload call, got %v", fake.calls)
	}
}

func TestUninstall_NeverInstalled_IsANoOp(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	fake := withFakeRunner(t)

	if err := Uninstall(); err != nil {
		t.Errorf("expected Uninstall on a never-installed service to be a no-op, got error: %v", err)
	}
	if len(fake.calls) != 0 {
		t.Errorf("expected no launchctl calls when nothing was ever installed, got %v", fake.calls)
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

	fake.err = nil // launchctl list succeeding = running
	st, err = CheckStatus()
	if err != nil {
		t.Fatalf("CheckStatus: %v", err)
	}
	if !st.Installed {
		t.Error("expected Installed=true after Install")
	}
	if !st.Running {
		t.Error("expected Running=true when the fake launchctl list call succeeds")
	}

	fake.err = fmt.Errorf("exit status 1") // launchctl list failing = not running
	st, err = CheckStatus()
	if err != nil {
		t.Fatalf("CheckStatus: %v", err)
	}
	if st.Running {
		t.Error("expected Running=false when the fake launchctl list call fails")
	}
}
