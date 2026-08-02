// Ticket T10b RED: the three decoupled update channels exist as a small
// interface, and the no-op wiring reports "unconfigured" without error.
package update

import "testing"

func TestChannelsExistAndUnconfigured(t *testing.T) {
	m := New()
	if m.Core.ChannelName() != "core" {
		t.Errorf("Core.ChannelName() = %q, want core", m.Core.ChannelName())
	}
	if m.Plugins.ChannelName() != "plugins" {
		t.Errorf("Plugins.ChannelName() = %q, want plugins", m.Plugins.ChannelName())
	}
	if m.Catalog.ChannelName() != "catalog" {
		t.Errorf("Catalog.ChannelName() = %q, want catalog", m.Catalog.ChannelName())
	}
	if got := m.State(); got != StateUnconfigured {
		t.Errorf("State() = %q, want %q (scaffolding only — no network)", got, StateUnconfigured)
	}
}
