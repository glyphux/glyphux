// Ticket T4 (gap 2): the daemon's single mapping between the two runtime
// consent seams — pkg/runtime/wasm.ConsentChecker (per-capability) and
// pkg/runtime/rpc.ConsentChecker (per-scope) — and the consent engine's
// persisted granted subsets (consent.Engine.IsConsented). This is the ONLY
// place the two seam signatures meet the engine; the daemon path never uses
// the AlwaysConsent placeholders (wasm) or "declared == consented" (rpc).
//
// Both seams derive from the same source of truth: the most recent live
// Decision for the plugin's exact manifest fingerprint. An undecided (or
// denied, or stale-fingerprint) manifest is refused at load — Consented and
// Allowed both report false. A live decision grants exactly what the admin
// approved: the wasm seam (capability-level, no scope) consents a
// capability when the grant includes it with at least one scope; the rpc
// seam (scope-level) allows a specific scope only when the grant includes
// exactly that (capability, scope) pair.
package plugin

import (
	"context"

	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/pkg/sdk"
)

// ConsentAdapter implements both runtime consent seams backed by a single
// consent.Engine. It is built from the engine and the manifests of the
// plugins loaded at boot (the T5 registrar's Registered() set); a plugin
// not in that set is never consented.
type ConsentAdapter struct {
	engine    *consent.Engine
	manifests map[string]sdk.Manifest
}

// NewConsentAdapter snapshots the given plugins' manifests and wires them to
// engine. Callers (cmd/glyphuxd at boot, the T6 loader) rebuild the adapter
// after registration so the snapshot tracks the loaded plugin set.
func NewConsentAdapter(engine *consent.Engine, plugins []sdk.Plugin) *ConsentAdapter {
	m := make(map[string]sdk.Manifest, len(plugins))
	for _, p := range plugins {
		mm := p.Manifest()
		m[mm.Name] = mm
	}
	return &ConsentAdapter{engine: engine, manifests: m}
}

// Consented implements wasm.ConsentChecker. It reports whether the plugin's
// live decision grants capability with at least one scope — the wasm host
// module builder's per-capability gate (pkg/runtime/wasm/hostfuncs.go).
func (a *ConsentAdapter) Consented(pluginName, capability string) bool {
	return a.granted(pluginName, func(d consent.Decision) bool {
		for _, s := range d.GrantedAPI {
			if s.Capability == capability && len(s.Scopes) > 0 {
				return true
			}
		}
		return false
	})
}

// Allowed implements rpc.ConsentChecker. It reports whether the plugin's
// live decision grants exactly the (capability, scope) pair — the broker's
// per-scope seam (pkg/runtime/rpc/broker.go).
func (a *ConsentAdapter) Allowed(pluginName, capability, scope string) bool {
	return a.granted(pluginName, func(d consent.Decision) bool {
		for _, s := range d.GrantedAPI {
			if s.Capability == capability {
				for _, sc := range s.Scopes {
					if sc == scope {
						return true
					}
				}
			}
		}
		return false
	})
}

// granted resolves the plugin's live decision (if any) and asks has whether
// it covers the requested grant. Any failure — unknown plugin, undecided
// manifest, stale fingerprint, denied decision, engine/db error — resolves
// to false (deny-by-default).
func (a *ConsentAdapter) granted(pluginName string, has func(consent.Decision) bool) bool {
	m, ok := a.manifests[pluginName]
	if !ok {
		return false
	}
	d, live, err := a.engine.IsConsented(context.Background(), m)
	if err != nil || !live {
		return false
	}
	return has(d)
}
