package preset_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// signedHeroPresetPackage builds a genuinely verified packagefmt
// SignedPackage the way the install path produces one: encode a preset
// container, then Decode it against the trust set — the same
// archive → packagefmt → InstallFromPackage pipeline the marketplace
// endpoint runs (Ticket T8 / gap 8). The container is signed by priv under
// the test key id and verified with pub.
func signedHeroPresetPackage(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, requiresCore string) packagefmt.SignedPackage {
	t.Helper()
	payload, err := json.Marshal(heroPreset())
	if err != nil {
		t.Fatalf("marshal hero preset: %v", err)
	}
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "acme-hero-pro",
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "proprietary",
			RequiresCore:  requiresCore,
		},
		Files: []packagefmt.PackageFile{
			{Path: "preset.json", SHA256: sha256Hex(payload), Data: payload},
		},
		Checksums: map[string]string{"preset.json": sha256Hex(payload)},
	}
	data, err := packagefmt.Encode(sp, priv, "glyphux-dev-2026-01")
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	decoded, err := packagefmt.Decode(data, map[string]ed25519.PublicKey{"glyphux-dev-2026-01": pub})
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return *decoded
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%x", sum)
}

// TestInstallFromPackageThenImportMergesWhenHostHasTheBlock is the
// end-to-end, real-signing, real-compat-check happy path this ticket (P4.7)
// is built around: a preset packaged and signed exactly as a marketplace
// operator would, installed on a host that actually has the "hero" block
// registered, then merged into a route via the SAME Import method a
// locally-authored preset uses — no marketplace-specific merge path exists.
func TestInstallFromPackageThenImportMergesWhenHostHasTheBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, pub, priv, ">=0.1.0")

	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	installed, err := presets.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage: %v", err)
	}
	if installed.Name != "hero-section" {
		t.Fatalf("installed.Name = %q, want hero-section", installed.Name)
	}

	result, err := presets.Import(context.Background(), admin, reg, layouts, []string{"main"}, installed.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("Import result = %+v, want compatible", result)
	}
}

// TestInstallFromPackageThenImportDeclinesGracefullyWhenHostLacksTheBlock is
// the other half of the same story: a host that installs a marketplace
// package built against a block it does not have must still be able to
// INSTALL it (InstallFromPackage succeeds — it deliberately does not run
// Save's live-registry gate), and Import must then decline the merge with a
// structured, explainable compat.Result rather than InstallFromPackage
// hard-erroring earlier. This is the behavior the ticket specifically calls
// out: marketplace import must go through Import's existing
// tolerant-and-explainable path, not reimplement a hard reject.
func TestInstallFromPackageThenImportDeclinesGracefullyWhenHostLacksTheBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, pub, priv, "")

	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	hostRegistry := blocks.New() // "hero" never registered on this host

	installed, err := presets.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage: %v, want it to succeed even though this host lacks the block", err)
	}

	result, err := presets.Import(context.Background(), admin, hostRegistry, layouts, []string{"main"}, installed.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Compatible {
		t.Fatal("Import result.Compatible = true, want false (host lacks the hero block)")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
	if _, err := layouts.Load(context.Background(), "home"); !errors.Is(err, layout.ErrNotFound) {
		t.Fatalf("Load after declined import err = %v, want ErrNotFound (nothing should have merged)", err)
	}
}

// TestInstallFromPackageTrustsTheVerifiedRepresentation pins the T8
// contract: InstallFromPackage consumes the output of packagefmt.Decode and
// does NOT re-verify signatures or checksums — that gate lives in exactly
// one place (packagefmt.Decode, whose own suite covers tampered payloads,
// tampered manifests, wrong keys, and unknown key ids). A SignedPackage
// constructed directly (bypassing Decode, with a bogus signature) still
// installs, because this store trusts the verified representation exactly
// as it trusts its own storage. Defense in depth comes from the API layer:
// the install endpoint only ever hands this store a Decode-verified
// package.
func TestInstallFromPackageTrustsTheVerifiedRepresentation(t *testing.T) {
	payload, err := json.Marshal(heroPreset())
	if err != nil {
		t.Fatalf("marshal hero preset: %v", err)
	}
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "acme-hero-pro",
			Version:       "1.0.0",
			Kind:          packagefmt.KindPreset,
			License:       "proprietary",
			RequiresCore:  "",
		},
		Files: []packagefmt.PackageFile{
			{Path: "preset.json", SHA256: "bogus", Data: payload},
		},
		Checksums:  map[string]string{"preset.json": "bogus"},
		Signatures: []packagefmt.Signature{{KeyID: "untrusted", Algorithm: "ed25519", Value: []byte("bogus")}},
	}

	presets := preset.NewStore(testDB(t))
	installed, err := presets.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage of a Decode-bypassed representation err = %v, want success (verification is packagefmt.Decode's job)", err)
	}
	if installed.Name != "hero-section" {
		t.Fatalf("installed.Name = %q, want hero-section", installed.Name)
	}
}

func TestInstallFromPackageRejectsUnsatisfiedRequiresCore(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, pub, priv, ">=5.0.0")

	presets := preset.NewStore(testDB(t))
	_, err = presets.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if !errors.Is(err, preset.ErrRequiresCoreNotSatisfied) {
		t.Fatalf("InstallFromPackage with an unsatisfied requires.core err = %v, want ErrRequiresCoreNotSatisfied", err)
	}
}

func TestInstallFromPackageRequiresPresetsManageCapability(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, pub, priv, "")

	presets := preset.NewStore(testDB(t))
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := presets.InstallFromPackage(context.Background(), editor, sp, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as editor err = %v, want ErrDenied", err)
	}
	if _, err := presets.InstallFromPackage(context.Background(), nil, sp, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as anonymous err = %v, want ErrDenied", err)
	}
}
