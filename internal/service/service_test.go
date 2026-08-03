// Ticket T10b RED: the service manager contract, behavior-first. The
// executor is a recorder — no real systemd in CI.
package service

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// recordingExec records every Run call and returns canned output.
type recordingExec struct {
	calls []string
	out   string
	err   error
}

func (r *recordingExec) Run(name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, strings.Join(append([]string{name}, args...), " "))
	return []byte(r.out), r.err
}

// --- Acceptance criterion (a): Install on Linux renders the systemd unit
// (User=glyphux, ExecStart → the glyphux binary, WorkingDirectory,
// Restart=on-failure) and runs `systemctl enable --now glyphux.service`. ---

func TestInstallLinuxWritesUnitAndEnables(t *testing.T) {
	fe := &recordingExec{}
	dir := t.TempDir()
	bin := "/usr/local/bin/glyphuxd"
	m := NewManager(fe, WithUnitDir(dir), WithBinPath(bin), WithPlatform("linux"))

	if err := m.Install(context.Background()); err != nil {
		t.Fatalf("Install: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, "glyphux.service"))
	if err != nil {
		t.Fatalf("unit file glyphux.service not written: %v", err)
	}
	unit := string(raw)
	for _, want := range []string{
		"User=glyphux",
		"ExecStart=" + bin,
		"WorkingDirectory=" + filepath.Dir(bin),
		"Restart=on-failure",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q:\n%s", want, unit)
		}
	}
	if len(fe.calls) == 0 || fe.calls[0] != "systemctl enable --now glyphux.service" {
		t.Errorf("exec calls = %v, want first call systemctl enable --now glyphux.service", fe.calls)
	}
}

// --- Acceptance criterion (b): Status runs `systemctl is-active
// glyphux.service` and maps the output → running/stopped/unknown. ---

func TestStatusMapsSystemctlOutput(t *testing.T) {
	for _, tc := range []struct {
		out  string
		want Status
	}{
		{"active\n", StatusRunning},
		{"inactive\n", StatusStopped},
		{"failed\n", StatusUnknown},
		{"", StatusUnknown},
	} {
		fe := &recordingExec{out: tc.out}
		m := NewManager(fe, WithPlatform("linux"))
		got, err := m.Status(context.Background())
		if err != nil {
			t.Fatalf("Status(%q): %v", tc.out, err)
		}
		if got != tc.want {
			t.Errorf("Status(%q) = %v, want %v", tc.out, got, tc.want)
		}
		if len(fe.calls) != 1 || fe.calls[0] != "systemctl is-active glyphux.service" {
			t.Errorf("exec calls = %v, want [systemctl is-active glyphux.service]", fe.calls)
		}
	}
}

// --- Acceptance criterion (c): Uninstall stops, disables, and removes the
// unit file. ---

func TestUninstallStopsDisablesRemovesUnit(t *testing.T) {
	dir := t.TempDir()
	unitPath := filepath.Join(dir, "glyphux.service")
	if err := os.WriteFile(unitPath, []byte("stale unit"), 0o644); err != nil {
		t.Fatal(err)
	}
	fe := &recordingExec{}
	m := NewManager(fe, WithUnitDir(dir), WithPlatform("linux"))

	if err := m.Uninstall(context.Background()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if len(fe.calls) < 2 || fe.calls[0] != "systemctl stop glyphux.service" || fe.calls[1] != "systemctl disable glyphux.service" {
		t.Errorf("exec calls = %v, want [systemctl stop glyphux.service systemctl disable glyphux.service]", fe.calls)
	}
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Errorf("unit file still present after Uninstall (stat err = %v)", err)
	}
}

// --- Acceptance criterion (d): Restart runs `systemctl restart
// glyphux.service`. ---

func TestRestartRunsSystemctlRestart(t *testing.T) {
	fe := &recordingExec{}
	m := NewManager(fe, WithPlatform("linux"))

	if err := m.Restart(context.Background()); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if len(fe.calls) != 1 || fe.calls[0] != "systemctl restart glyphux.service" {
		t.Errorf("exec calls = %v, want [systemctl restart glyphux.service]", fe.calls)
	}
}

// --- Acceptance criterion (e): on an unsupported platform (Windows),
// Install returns the documented "advanced mode — not yet" stub error and
// never invokes the executor. ---

func TestInstallWindowsReturnsAdvancedModeStub(t *testing.T) {
	fe := &recordingExec{}
	m := NewManager(fe, WithPlatform("windows"))

	err := m.Install(context.Background())
	if !errors.Is(err, ErrAdvancedModeNotYet) {
		t.Fatalf("Install on windows = %v, want ErrAdvancedModeNotYet (advanced mode — not yet)", err)
	}
	if len(fe.calls) != 0 {
		t.Errorf("no executor calls expected on windows, got %v", fe.calls)
	}
}
