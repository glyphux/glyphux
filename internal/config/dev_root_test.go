//go:build dev

// Ticket T10a: the era/prod-root split. This file builds ONLY under
// `-tags dev` — the dev workflow's trust semantics: the embedded default
// seed is the dev root (which remains absent from normal builds).
package config_test

import (
	"testing"

	"github.com/glyphux/glyphux/internal/config"
)

// defaultSeedID is the id of the Default() seed in THIS build mode — the
// dev root under `-tags dev`, the prod root in normal builds (see
// prod_root_test.go). Untagged tests use it so they stay correct in both
// build modes.
func defaultSeedID() string { return config.DevRootKeyID }

// --- Acceptance criterion (b): a `-tags dev` build seeds the dev root —
// the dev workflow (signing fixtures with the dev key, testing installs)
// stays intact. ---

func TestDevBuildSeedsDevRoot(t *testing.T) {
	cfg := config.Default()
	if len(cfg.Marketplace.TrustedKeys) != 1 {
		t.Fatalf("TrustedKeys = %d, want exactly the embedded dev root", len(cfg.Marketplace.TrustedKeys))
	}
	k := cfg.Marketplace.TrustedKeys[0]
	if k.ID != config.DevRootKeyID || k.Algorithm != "ed25519" || k.Issuer != "glyphux" || k.Status != "active" {
		t.Errorf("dev root = %+v, want id=%s algorithm=ed25519 issuer=glyphux status=active", k, config.DevRootKeyID)
	}
	if k.PublicKey != config.DevRootPublicKey {
		t.Errorf("dev root public key = %q, want the pinned dev key", k.PublicKey)
	}
	if len(k.Purpose) != 1 || k.Purpose[0] != "package-signing" {
		t.Errorf("purpose = %v, want [package-signing]", k.Purpose)
	}
	if hasTrustedKey(cfg, config.ProdRootKeyID) {
		t.Error("prod root must NOT be a seed in a dev-tagged build")
	}
}
