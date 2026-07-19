package marketplace

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Entitlement is what a purchase issues (PRD §12.5 mechanism 2): a buyer's
// license, the extension and version range it entitles, a validity window,
// and an allowed scope. This is the payload signed into an
// EntitlementToken — never used or trusted on its own.
type Entitlement struct {
	// LicenseID identifies the buyer/license (PRD §12.5: "buyer/license
	// ID"). Opaque to this package — the marketplace's own concern.
	LicenseID string
	// ExtensionName is the entitled extension (package Name, matching
	// Package.Name above).
	ExtensionName string
	// MinVersion/MaxVersion bound the entitled version range (semver,
	// inclusive; empty means unbounded on that side) — e.g. a one-year
	// license bought against 1.x might entitle MinVersion "1.0.0" with no
	// MaxVersion, or a specific-major license might cap MaxVersion "1.999.999".
	MinVersion string
	MaxVersion string
	// NotBefore/NotAfter are the token's validity window (PRD §12.5:
	// "validity window"). NotAfter is the expiry an un-renewed license
	// eventually crosses.
	NotBefore time.Time
	NotAfter  time.Time
	// Scope is the allowed scope (PRD §12.5: "allowed scope") — e.g.
	// ["updates", "support"] — opaque strings this package does not
	// interpret; a future registry-integration slice defines their
	// vocabulary.
	Scope []string
}

// EntitlementToken pairs an Entitlement with its Ed25519 signature,
// computed by the marketplace's own private key at issuance time.
type EntitlementToken struct {
	Entitlement Entitlement
	Signature   []byte
}

// Sentinel errors distinguishing WHY VerifyEntitlement failed. PRD-driven
// requirement: an expired token and a tampered/wrongly-signed token must be
// distinguishable, not collapsed into one generic failure — expired is a
// normal, expected lifecycle state (the buyer's license lapsed), while an
// invalid signature is a tamper/forgery signal.
var (
	// ErrEntitlementInvalidSignature means the token's signature does not
	// verify under the given public key — either the payload was altered
	// after signing (any field, including the validity window itself) or
	// it was never signed by the claimed marketplace key at all.
	ErrEntitlementInvalidSignature = errors.New("marketplace: entitlement token has an invalid signature")
	// ErrEntitlementExpired means the token verified correctly (genuinely
	// issued, untampered) but the current time is after its NotAfter.
	// This is the normal, graceful-expiry outcome (PRD §12.5) — not a
	// tamper signal.
	ErrEntitlementExpired = errors.New("marketplace: entitlement token has expired")
	// ErrEntitlementNotYetValid means the token verified correctly but the
	// current time is before its NotBefore.
	ErrEntitlementNotYetValid = errors.New("marketplace: entitlement token is not yet valid")
)

// IssueEntitlement signs ent with priv (the marketplace's own Ed25519
// private key), producing the token a buyer receives. This is the
// marketplace operator's own issuance step — never run on a host or by a
// buyer.
func IssueEntitlement(priv ed25519.PrivateKey, ent Entitlement) (EntitlementToken, error) {
	payload, err := entitlementSigningPayload(ent)
	if err != nil {
		return EntitlementToken{}, fmt.Errorf("marketplace: build entitlement payload: %w", err)
	}
	sig := ed25519.Sign(priv, payload)
	return EntitlementToken{Entitlement: ent, Signature: sig}, nil
}

// VerifyEntitlement verifies tok using ONLY pub (the marketplace's public
// key, baked into the verifying host — PRD §12.5's "the host ships the
// marketplace's public key and verifies the token locally, offline, at
// install and load — no network call to confirm entitlement") and now (an
// explicitly injected clock, never time.Now() internally, so callers — and
// this package's own tests — can check expiry deterministically without
// sleeping or mocking a network dependency at all).
//
// Nothing in this function's call graph touches the network: it takes a
// public key and a token, both already in memory, and returns a decision
// from local computation alone. See entitlement_test.go
// (TestVerifyEntitlementDoesNotRequireNetworkAccess, which breaks every
// process-wide HTTP transport before calling this) and noimports_test.go
// (which statically proves this package imports no networking package at
// all) for the two independent proofs of that property.
//
// The signature is checked BEFORE the validity window, deliberately: the
// window fields themselves are part of the signed payload, so an
// unverified token's NotBefore/NotAfter cannot be trusted enough to even
// consult. This ordering is also what makes the two failure modes
// distinguishable — see ErrEntitlementInvalidSignature vs
// ErrEntitlementExpired's doc comments.
func VerifyEntitlement(pub ed25519.PublicKey, tok EntitlementToken, now time.Time) (Entitlement, error) {
	payload, err := entitlementSigningPayload(tok.Entitlement)
	if err != nil {
		return Entitlement{}, fmt.Errorf("marketplace: build entitlement payload: %w", err)
	}
	if !ed25519.Verify(pub, payload, tok.Signature) {
		return Entitlement{}, ErrEntitlementInvalidSignature
	}
	if now.Before(tok.Entitlement.NotBefore) {
		return Entitlement{}, ErrEntitlementNotYetValid
	}
	if now.After(tok.Entitlement.NotAfter) {
		return Entitlement{}, ErrEntitlementExpired
	}
	return tok.Entitlement, nil
}

// entitlementSigningPayload builds the canonical byte payload signed and
// verified for ent. Time fields are marshaled via their RFC 3339 JSON
// encoding (time.Time's default json.Marshal behavior), which is what
// makes them tamper-evident like every other field.
func entitlementSigningPayload(ent Entitlement) ([]byte, error) {
	return json.Marshal(ent)
}
