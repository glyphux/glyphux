// Package service manages the glyphux system service (Ticket T10b) — the
// CLI's `glyphux service install|uninstall|status|restart` surface. The
// daemon behavior lives in core; platform packaging (installers, plists,
// services) lives in the installer repo — this package renders the small
// unit definition and drives the platform service manager through an
// injectable executor so CI never touches a real systemd/launchd.
//
// RED checkpoint (T10b): the types and contract exist; the behaviors are
// stubs returning ErrAdvancedModeNotYet so the RED tests fail for the
// right reasons.
package service

import (
	"context"
	"errors"
	"runtime"
)

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
	o := Options{Platform: runtime.GOOS}
	for _, opt := range opts {
		opt(&o)
	}
	return &Manager{exec: exec, opts: o}
}

// Install renders the service unit and enables it.
func (m *Manager) Install(ctx context.Context) error { return ErrAdvancedModeNotYet }

// Uninstall stops, disables, and removes the service unit.
func (m *Manager) Uninstall(ctx context.Context) error { return ErrAdvancedModeNotYet }

// Status maps the service manager's is-active output to Status.
func (m *Manager) Status(ctx context.Context) (Status, error) { return StatusUnknown, nil }

// Restart restarts the service.
func (m *Manager) Restart(ctx context.Context) error { return ErrAdvancedModeNotYet }
