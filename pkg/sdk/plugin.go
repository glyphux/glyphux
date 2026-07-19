package sdk

// Plugin is the public extension contract every runtime tier implements
// (PRD §8.3): "All tiers register through one conceptual interface.
// Transport differs (Go interface for Tier A, WIT-generated bindings for
// Tier B, gRPC for Tier C), the contract does not." A Tier-A (in-process)
// plugin implements this directly in Go; Tier B/C plugins speak the same
// two calls across their own transport (WASM export calls / gRPC methods)
// rather than this Go interface literally — see pkg/runtime/wasm and
// pkg/runtime/rpc for how each tier's host adapts its own transport to this
// same conceptual shape.
type Plugin interface {
	// Manifest returns this plugin's declared identity, requirements, and
	// the two consent-relevant axes (api/permissions) — see Manifest's own
	// doc comment.
	Manifest() Manifest
	// Register is called once, after a HostAPI has been constructed for
	// this plugin's manifest (via NewHostAPI, already capability-gated),
	// to let the plugin declare its content types, blocks, admin pages,
	// jobs, and event subscriptions. host is the ONLY surface this plugin
	// may reach (PRD §8.3) — a well-behaved Plugin never imports kernel
	// internals to bypass it.
	Register(host HostAPI) error
}
