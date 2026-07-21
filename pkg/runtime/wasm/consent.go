package wasm

// ConsentChecker is the named seam slice 2.7's install-time consent engine
// plugs into (PRD §10.2's second point: "install-time consent... the admin
// sees a consent screen... Allow?"). This slice (2.4) deliberately does not
// build that engine — it only needs somewhere for it to attach later without
// forcing a rewrite of Runtime.
//
// Consented reports whether pluginName has actually been granted capability
// by whatever consent authority backs the checker (a human admin's
// install-time decision, per §10.2 point 2). Runtime calls this once per
// declared capability while building a guest's host-function import set
// (see hostfuncs.go): a capability only gets its host functions imported if
// BOTH the manifest declared it (checked by the caller before this is ever
// invoked) AND this reports true.
type ConsentChecker interface {
	Consented(pluginName, capability string) bool
}

// AlwaysConsent is the slice-2.4 placeholder ConsentChecker: it treats every
// declared capability as already consented to. This is an explicit,
// documented stand-in — PRD §10.2 draws install-time consent out as its own
// distinct point in the trust model, separate from marketplace review and
// runtime enforcement, and slice 2.7 (running in parallel with this one) is
// where that real consent screen/decision gets built. Until 2.7 lands and a
// caller wires in its real ConsentChecker, "declared in the manifest" is the
// only signal Runtime has, so it is treated as sufficient — this keeps the
// two slices decoupled without either blocking on the other landing first.
type AlwaysConsent struct{}

// Consented always reports true — see AlwaysConsent's doc comment.
func (AlwaysConsent) Consented(pluginName, capability string) bool { return true }
