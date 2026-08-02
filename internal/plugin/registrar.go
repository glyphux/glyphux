// Package plugin is the daemon's in-process (Tier A) plugin registrar: the
// single place cmd/glyphuxd registers the first-party capability plugins
// (forms, seo, commerce, membership, notifications — Ticket T5 / gap 3) and,
// from Ticket T6 on, the base the 3-tier loader extends.
//
// The registrar owns the shared KernelDeps every plugin host is built from:
// the four domain stores the daemon already has (Compositions, Content,
// Media, Identities) plus the process-lifetime defaults it creates for the
// rest — one shared EventBus, one shared MemoryKVBackend (Ticket T6 swaps
// the pluginstore DB backend in), the shared blocks registry (so plugin-
// registered Layer-2 blocks and the daemon's own layout transport see the
// same set), and Audit (nil until Ticket T7 wires the audit store in).
package plugin

import (
	"context"
	"fmt"
	"sync"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// Registrar collects Tier-A plugins and, on Activate, builds each plugin's
// HostAPI from the shared KernelDeps and calls its Register method.
//
// Invariant handling deliberately mirrors blocks/firstparty.RegisterAll's
// fatal-fast style: RegisterPlugin refuses an empty plugin name, a duplicate
// name, or an invalid manifest immediately, before any registration side
// effect — a first-party programming bug, not an operator misconfiguration,
// so the daemon fails at startup rather than half-booting.
type Registrar struct {
	mu      sync.Mutex
	deps    sdk.KernelDeps
	plugins []sdk.Plugin
}

// New builds a registrar over the given shared KernelDeps. Compositions,
// Content, Media and Identities must be supplied by the caller (they are
// the daemon's real stores); Bus, KV and Blocks are defaulted when nil to
// fresh process-lifetime instances — one EventBus, one MemoryKVBackend, one
// blocks registry — shared by every plugin this registrar activates.
func New(deps sdk.KernelDeps) *Registrar {
	if deps.Bus == nil {
		deps.Bus = sdk.NewEventBus()
	}
	if deps.KV == nil {
		deps.KV = sdk.NewMemoryKVBackend()
	}
	if deps.Blocks == nil {
		deps.Blocks = blocks.New()
	}
	return &Registrar{deps: deps}
}

// RegisterPlugin adds an in-process plugin to the registrar. It fails fast —
// before recording anything — on an empty plugin name, a name already
// registered, or a manifest that does not validate.
func (r *Registrar) RegisterPlugin(p sdk.Plugin) error {
	m := p.Manifest()
	if m.Name == "" {
		return fmt.Errorf("register plugin: empty plugin name")
	}
	if err := m.Validate(); err != nil {
		return fmt.Errorf("register plugin %q: invalid manifest: %w", m.Name, err)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.plugins {
		if existing.Manifest().Name == m.Name {
			return fmt.Errorf("register plugin %q: duplicate plugin name", m.Name)
		}
	}
	r.plugins = append(r.plugins, p)
	return nil
}

// Registered returns a snapshot of the plugins registered so far, in
// registration order.
func (r *Registrar) Registered() []sdk.Plugin {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]sdk.Plugin, len(r.plugins))
	copy(out, r.plugins)
	return out
}

// Bus returns the shared EventBus every activated plugin host was built
// with (nil only if the registrar was never used — the deps are defaulted
// in New).
func (r *Registrar) Bus() *sdk.EventBus {
	return r.deps.Bus
}

// KV returns the shared KVBackend every activated plugin host was built
// with.
func (r *Registrar) KV() sdk.KVBackend {
	return r.deps.KV
}

// Activate builds each registered plugin's HostAPI from the shared
// KernelDeps and calls its Register method, in registration order, and
// stops at the first failure — a failed activation aborts the whole boot
// rather than leaving a partially-registered plugin set. Activation is
// idempotent in the sense that a failed plugin leaves nothing behind beyond
// the domain side effects its Register already committed.
func (r *Registrar) Activate(ctx context.Context) error {
	for _, p := range r.Registered() {
		m := p.Manifest()
		host, err := sdk.NewHostAPI(m, r.deps)
		if err != nil {
			return fmt.Errorf("activate plugin %q: build host: %w", m.Name, err)
		}
		if err := p.Register(host); err != nil {
			return fmt.Errorf("activate plugin %q: %w", m.Name, err)
		}
	}
	return nil
}
