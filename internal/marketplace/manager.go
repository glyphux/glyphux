package marketplace

import (
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/config"
)

// Manager is the daemon's marketplace bundle: the resolved catalog, the
// entitlement registry, and the key-ID-aware trust set the install path and
// entitlement verification resolve against. Wired into the API surface via
// api.WithMarketplace (internal/api).
type Manager struct {
	Catalog      *Catalog
	Entitlements *Store
	// Keys is the trust set: trusted_keys records decoded to public keys,
	// keyed by their record id. packagefmt.Decode resolves a container's
	// signature key id here; entitlement verification tries every key (the
	// purpose discrimination — package-signing vs entitlement-signing — is
	// deferred, per the locked T8 decision).
	Keys map[string]ed25519.PublicKey
}

// NewManager assembles the Manager. keys must be non-nil (an empty trust
// set is valid — every package then fails ErrUntrustedKey, deny-by-default).
func NewManager(cat *Catalog, tokens *Store, keys map[string]ed25519.PublicKey) *Manager {
	return &Manager{Catalog: cat, Entitlements: tokens, Keys: keys}
}

// DecodeTrustedKeys decodes the config's trusted_keys records into the
// key-ID-aware trust map. A record with an unknown algorithm or an
// undecodable public key is a fail-fast configuration error (the operator
// declared a key the daemon cannot use); a revoked/expired record is still
// decoded — the status field is descriptive today (the T10 era/root split
// will branch on it).
func DecodeTrustedKeys(records []config.TrustedKey) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey, len(records))
	for _, r := range records {
		if r.Algorithm != "ed25519" {
			return nil, fmt.Errorf("marketplace: trusted key %q: unsupported algorithm %q (want ed25519)", r.ID, r.Algorithm)
		}
		raw, err := hex.DecodeString(r.PublicKey)
		if err != nil || len(raw) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("marketplace: trusted key %q: public_key is not a valid ed25519 key (want 64 hex chars)", r.ID)
		}
		keys[r.ID] = ed25519.PublicKey(raw)
	}
	return keys, nil
}

// TokenStatus verifies tok against the trust set at now and returns its
// status: "active", "expired", "not_yet_valid", or "invalid". Every trusted
// key is tried (purpose discrimination deferred); a token that verifies
// under any key reports its window status, a token that verifies under none
// is "invalid" (tampered or issued by an untrusted key).
func (m *Manager) TokenStatus(tok marketplace.EntitlementToken, now time.Time) string {
	for _, pub := range m.Keys {
		_, err := marketplace.VerifyEntitlement(pub, tok, now)
		switch err {
		case nil:
			return "active"
		case marketplace.ErrEntitlementExpired:
			return "expired"
		case marketplace.ErrEntitlementNotYetValid:
			return "not_yet_valid"
		default:
			// ErrEntitlementInvalidSignature: try the next key.
		}
	}
	return "invalid"
}
