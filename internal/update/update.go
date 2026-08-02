// Package update is the scaffolding for the three decoupled update
// channels (Ticket T10b): core / plugins / catalog. This ticket wires the
// channels and the "unconfigured" state only — no network, no polling, no
// version resolution. The channels exist so the CLI surface and config can
// name them; the real fetching/verification lives behind them later.
package update

// State is the update subsystem's wiring state.
type State string

const (
	// StateUnconfigured means no channel has been configured yet — the
	// scaffold's only state.
	StateUnconfigured State = "unconfigured"
)

// Channel is one decoupled update channel: a named feed of signed updates
// (core = the daemon binary, plugins = tier-b/c plugins, catalog = the
// marketplace catalog). ChannelName is the stable identifier.
type Channel interface {
	ChannelName() string
}

type channel struct{ name string }

func (c channel) ChannelName() string { return c.name }

// Manager exposes the three channels and the wiring state.
type Manager struct {
	Core    Channel
	Plugins Channel
	Catalog Channel
}

// New builds the scaffold: three named channels, all unconfigured.
func New() *Manager {
	return &Manager{
		Core:    channel{"core"},
		Plugins: channel{"plugins"},
		Catalog: channel{"catalog"},
	}
}

// State reports the wiring state — scaffolding only, no network.
func (m *Manager) State() State { return StateUnconfigured }
