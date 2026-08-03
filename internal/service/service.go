// Package service manages the glyphux system service (Ticket T10b) — the
// CLI's `glyphux service install|uninstall|status|restart` surface. The
// daemon behavior lives in core; platform packaging (installers, plists,
// services) lives in the installer repo — this package renders the small
// unit definition and drives the platform service manager through an
// injectable executor so CI never touches a real systemd/launchd.
package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// serviceUnitName is the systemd unit this package manages.
const serviceUnitName = "glyphux.service"

// Executor runs a platform service-manager command (systemctl on Linux,
// launchctl on macOS) — injected so tests use a recorder instead of a real
// supervisor.
type Executor interface {
	Run(name string, args ...string) ([]byte, error)
}

// Status is the mapped lifecycle state of the glyphux service.
type Status int

const (
	StatusUnknown Status = iota
	StatusRunning
	StatusStopped
)

func (s Status) String() string {
	switch s {
	case StatusRunning:
		return "running"
	case StatusStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ErrAdvancedModeNotYet is returned for platforms whose service packaging
// lives in the installer repo — the documented "advanced mode — not yet"
// stub (no half-baked service files from the CLI).
var ErrAdvancedModeNotYet = errors.New("service: advanced mode — not yet (platform service packaging lives in the installer repo)")

// Options configure a Manager.
type Options struct {
	// UnitDir is where the rendered service unit is written (Linux:
	// /etc/systemd/system by default; tests inject a temp dir).
	UnitDir string
	// BinPath is the glyphuxd binary the unit's ExecStart points at.
	BinPath string
	// Platform is the target platform ("linux", "darwin", "windows", …);
	// empty defaults to runtime.GOOS.
	Platform string
}

// Option mutates Options.
type Option func(*Options)

// WithUnitDir sets where the rendered unit is written.
func WithUnitDir(dir string) Option { return func(o *Options) { o.UnitDir = dir } }

// WithBinPath sets the glyphuxd binary path the unit ExecStart uses.
func WithBinPath(p string) Option { return func(o *Options) { o.BinPath = p } }

// WithPlatform pins the target platform (tests).
func WithPlatform(goos string) Option { return func(o *Options) { o.Platform = goos } }

// Manager drives the glyphux system service through an injected Executor.
type Manager struct {
	exec Executor
	opts Options
}

// NewManager builds a Manager. The exec runner is required; options default
// to the host platform, the system unit dir, and the glyphuxd binary next
// to the running glyphux binary.
func NewManager(exec Executor, opts ...Option) *Manager {
	bin, err := os.Executable()
	if err != nil {
		bin = "/usr/local/bin/glyphux"
	}
	o := Options{
		UnitDir:  "/etc/systemd/system",
		BinPath:  bin,
		Platform: runtime.GOOS,
	}
	for _, opt := range opts {
		opt(&o)
	}
	return &Manager{exec: exec, opts: o}
}

// unitTemplate renders the systemd unit for the configured binary. The
// template is the CLI's whole packaging footprint (the installer repo owns
// the rest); fields are the locked T10b shape — the daemon runs as the
// glyphux user, is restarted on failure, and works out of the binary's
// directory.
func (m *Manager) unitTemplate() string {
	return fmt.Sprintf(`[Unit]
Description=Glyphux daemon
After=network.target

[Service]
User=glyphux
ExecStart=%s
WorkingDirectory=%s
Restart=on-failure

[Install]
WantedBy=multi-user.target
`, m.opts.BinPath, filepath.Dir(m.opts.BinPath))
}

// Install renders the service unit and enables it. Linux runs systemctl
// enable --now; any other platform returns ErrAdvancedModeNotYet — its
// packaging lives in the installer repo.
func (m *Manager) Install(ctx context.Context) error {
	if m.opts.Platform != "linux" {
		return ErrAdvancedModeNotYet
	}
	if err := os.MkdirAll(m.opts.UnitDir, 0o755); err != nil {
		return fmt.Errorf("service: mkdir %s: %w", m.opts.UnitDir, err)
	}
	if err := os.WriteFile(filepath.Join(m.opts.UnitDir, serviceUnitName), []byte(m.unitTemplate()), 0o644); err != nil {
		return fmt.Errorf("service: write unit: %w", err)
	}
	if _, err := m.exec.Run("systemctl", "enable", "--now", serviceUnitName); err != nil {
		return fmt.Errorf("service: systemctl enable: %w", err)
	}
	return nil
}

// Uninstall stops, disables, and removes the service unit.
func (m *Manager) Uninstall(ctx context.Context) error {
	if m.opts.Platform != "linux" {
		return ErrAdvancedModeNotYet
	}
	if _, err := m.exec.Run("systemctl", "stop", serviceUnitName); err != nil {
		return fmt.Errorf("service: systemctl stop: %w", err)
	}
	if _, err := m.exec.Run("systemctl", "disable", serviceUnitName); err != nil {
		return fmt.Errorf("service: systemctl disable: %w", err)
	}
	if err := os.Remove(filepath.Join(m.opts.UnitDir, serviceUnitName)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("service: remove unit: %w", err)
	}
	return nil
}

// Status maps the service manager's is-active output to Status. systemctl
// exits non-zero for a stopped/failed unit, so the trimmed output is
// authoritative when present; an empty output with an executor error
// resolves to unknown.
func (m *Manager) Status(ctx context.Context) (Status, error) {
	if m.opts.Platform != "linux" {
		return StatusUnknown, ErrAdvancedModeNotYet
	}
	out, err := m.exec.Run("systemctl", "is-active", serviceUnitName)
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" {
		if err != nil {
			return StatusUnknown, fmt.Errorf("service: systemctl is-active: %w", err)
		}
		return StatusUnknown, nil
	}
	switch trimmed {
	case "active":
		return StatusRunning, nil
	case "inactive":
		return StatusStopped, nil
	default: // "failed", "activating", anything else
		return StatusUnknown, nil
	}
}

// Restart restarts the service.
func (m *Manager) Restart(ctx context.Context) error {
	if m.opts.Platform != "linux" {
		return ErrAdvancedModeNotYet
	}
	if _, err := m.exec.Run("systemctl", "restart", serviceUnitName); err != nil {
		return fmt.Errorf("service: systemctl restart: %w", err)
	}
	return nil
}
