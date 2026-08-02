//go:build !dev

// Ticket T10a: the era/prod-root split. This file builds ONLY in normal
// (non-`-tags dev`) builds — exactly the trust semantics a production
// daemon must have: the embedded default seed is the PROD-era root, and the
// dev root is NOT trusted unless an operator explicitly re-enables it via
// trusted_keys[] (additive) or opts into custom-only.
package config_test

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// defaultSeedID is the id of the Default() seed in THIS build mode — the
// prod root in normal builds, the dev root under `-tags dev` (see
// dev_root_test.go). Untagged tests use it so they stay correct in both
// build modes.
func defaultSeedID() string { return config.ProdRootKeyID }

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// signedPreset encodes a signed preset-kind container under keyID with priv
// — the verification tests exercise WHICH ids a seed trusts, so the
// signature bytes ride on the same in-test keypair the trust set binds.
func signedPreset(t *testing.T, priv ed25519.PrivateKey, keyID string) []byte {
	t.Helper()
	payload := []byte("t10a-prod-root-fixture")
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "theme-a",
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "free",
			RequiresCore:  ">=0.1.0",
		},
		Files:     []packagefmt.PackageFile{{Path: "preset.json", SHA256: sha256Hex(payload), Data: payload}},
		Checksums: map[string]string{"preset.json": sha256Hex(payload)},
	}
	data, err := packagefmt.Encode(sp, priv, keyID)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return data
}

// seedTrustSet binds every Default() seed id to a fresh in-test keypair and
// returns the keys plus the matching private keys — the seed's TRUST SET
// (which ids it trusts), decoupled from the pinned key bytes (those are
// asserted separately in TestProdRootIsDefaultSeed).
func seedTrustSet(t *testing.T) (map[string]ed25519.PublicKey, map[string]ed25519.PrivateKey) {
	t.Helper()
	keys := make(map[string]ed25519.PublicKey)
	privs := make(map[string]ed25519.PrivateKey)
	for _, k := range config.Default().Marketplace.TrustedKeys {
		pub, priv, err := ed25519.GenerateKey(nil)
		if err != nil {
			t.Fatal(err)
		}
		keys[k.ID] = pub
		privs[k.ID] = priv
	}
	return keys, privs
}

// --- Acceptance criterion (a): the default build seeds the PROD root, NOT
// the dev root. ---

func TestProdRootIsDefaultSeed(t *testing.T) {
	cfg := config.Default()
	if cfg.Marketplace.TrustMode != "" {
		t.Errorf("TrustMode = %q, want default additive (\"\")", cfg.Marketplace.TrustMode)
	}
	if len(cfg.Marketplace.TrustedKeys) != 1 {
		t.Fatalf("TrustedKeys = %d, want exactly the embedded prod root (T10 era split)", len(cfg.Marketplace.TrustedKeys))
	}
	k := cfg.Marketplace.TrustedKeys[0]
	if k.ID != config.ProdRootKeyID || k.Algorithm != "ed25519" || k.Issuer != "glyphux" || k.Status != "active" {
		t.Errorf("prod root = %+v, want id=%s algorithm=ed25519 issuer=glyphux status=active", k, config.ProdRootKeyID)
	}
	if k.PublicKey != config.ProdRootPublicKey {
		t.Errorf("prod root public key = %q, want the pinned prod key", k.PublicKey)
	}
	if len(k.Purpose) != 1 || k.Purpose[0] != "package-signing" {
		t.Errorf("purpose = %v, want [package-signing]", k.Purpose)
	}
	if hasTrustedKey(cfg, config.DevRootKeyID) {
		t.Error("dev root must NOT be a default seed in a normal (non-dev-tagged) build")
	}
}

// --- Acceptance criterion (c): a package signed under the dev key id fails
// verification against a prod-seed trust set with ErrUntrustedKey. ---

func TestProdSeedRejectsDevSignedPackage(t *testing.T) {
	keys, _ := seedTrustSet(t)
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = pub // the dev id is NOT in the seed — its key is unknown to it
	data := signedPreset(t, priv, config.DevRootKeyID)

	_, err = packagefmt.Decode(data, keys)
	if !errors.Is(err, packagefmt.ErrUntrustedKey) {
		t.Fatalf("Decode error = %v, want ErrUntrustedKey (dev root must not be a default seed in a prod build)", err)
	}
}

// --- Acceptance criterion (d): a package signed under the prod key id
// verifies against the prod seed. ---

func TestProdSeedAcceptsProdSignedPackage(t *testing.T) {
	keys, privs := seedTrustSet(t)
	priv, ok := privs[config.ProdRootKeyID]
	if !ok {
		t.Fatalf("prod root %q is not a Default() seed — the era split is missing", config.ProdRootKeyID)
	}
	data := signedPreset(t, priv, config.ProdRootKeyID)

	got, err := packagefmt.Decode(data, keys)
	if err != nil {
		t.Fatalf("prod-signed package must verify against the prod seed: %v", err)
	}
	if got.Manifest.Name != "theme-a" {
		t.Errorf("decoded manifest name = %q, want theme-a", got.Manifest.Name)
	}
}

// --- Acceptance criterion (e): an operator can explicitly add the dev key
// via trusted_keys[] even in a prod build — additive semantics intact. ---

func TestOperatorCanExplicitlyAddDevKeyInProdBuild(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glyphux.json")
	doc := `{"marketplace": {"trusted_keys": [{
		"id": "glyphux-dev-2026-01",
		"algorithm": "ed25519",
		"public_key": "` + config.DevRootPublicKey + `",
		"purpose": ["package-signing"],
		"issuer": "glyphux",
		"status": "active"
	}]}}`
	if err := os.WriteFile(p, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	// Additive: the operator's dev key joins the prod seed — it does not
	// replace it.
	if len(cfg.Marketplace.TrustedKeys) != 2 {
		t.Fatalf("TrustedKeys = %d, want prod root + operator dev key (additive)", len(cfg.Marketplace.TrustedKeys))
	}
	if findTrustedKey(t, cfg, config.ProdRootKeyID) == nil {
		t.Error("prod root missing after operator dev key added")
	}
	if findTrustedKey(t, cfg, config.DevRootKeyID) == nil {
		t.Error("explicit operator dev key missing")
	}
}

// --- Acceptance criterion (f): custom-only still fully replaces the seed —
// neither the prod nor the dev root survives. ---

func TestCustomOnlyReplacesProdSeed(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glyphux.json")
	if err := os.WriteFile(p, []byte(`{
		"marketplace": {
			"trust_mode": "custom-only",
			"trusted_keys": [{"id": "acme-prod-2026", "algorithm": "ed25519", "public_key": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789", "status": "active"}]
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Marketplace.TrustedKeys) != 1 {
		t.Fatalf("TrustedKeys = %d, want only the operator key (custom-only replaces the seed)", len(cfg.Marketplace.TrustedKeys))
	}
	if cfg.Marketplace.TrustedKeys[0].ID != "acme-prod-2026" {
		t.Errorf("only key = %+v, want acme-prod-2026", cfg.Marketplace.TrustedKeys[0])
	}
}
