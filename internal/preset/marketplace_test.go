package preset_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
)

func signedHeroPresetPackage(t *testing.T, priv ed25519.PrivateKey, requiresCore string) marketplace.SignedPackage {
	t.Helper()
	pkg, result, err := marketplace.PackagePreset("acme-hero-pro", "1.0.0", "proprietary", requiresCore, *heroPreset())
	if err != nil {
		t.Fatalf("PackagePreset: %v (result: %+v)", err, result)
	}
	sp, err := marketplace.Sign(priv, pkg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return sp
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
	sp := signedHeroPresetPackage(t, priv, ">=0.1.0")

	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	installed, err := presets.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
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
	got, err := layouts.Load(context.Background(), "home")
	if err != nil {
		t.Fatalf("Load merged layout: %v", err)
	}
	if len(got.Regions["main"].Blocks) != 1 || got.Regions["main"].Blocks[0].Type != "hero" {
		t.Fatalf("merged layout = %+v, want the preset's hero block", got)
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
	sp := signedHeroPresetPackage(t, priv, "")

	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	hostRegistry := blocks.New() // "hero" never registered on this host

	installed, err := presets.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
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

func TestInstallFromPackageRejectsTamperedPackage(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, priv, "")
	sp.Package.Artifact = append([]byte(nil), sp.Package.Artifact...)
	sp.Package.Artifact[0] ^= 0xFF

	presets := preset.NewStore(testDB(t))
	if _, err := presets.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0"); !errors.Is(err, marketplace.ErrInvalidSignature) {
		t.Fatalf("InstallFromPackage of a tampered package err = %v, want ErrInvalidSignature", err)
	}
}

func TestInstallFromPackageRejectsWrongSigningKey(t *testing.T) {
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, priv, "")

	presets := preset.NewStore(testDB(t))
	if _, err := presets.InstallFromPackage(context.Background(), admin, sp, otherPub, "0.5.0"); !errors.Is(err, marketplace.ErrInvalidSignature) {
		t.Fatalf("InstallFromPackage against the wrong public key err = %v, want ErrInvalidSignature", err)
	}
}

func TestInstallFromPackageRejectsUnsatisfiedRequiresCore(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, priv, ">=5.0.0")

	presets := preset.NewStore(testDB(t))
	_, err = presets.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
	if !errors.Is(err, preset.ErrRequiresCoreNotSatisfied) {
		t.Fatalf("InstallFromPackage with an unsatisfied requires.core err = %v, want ErrRequiresCoreNotSatisfied", err)
	}
}

func TestInstallFromPackageRequiresPresetsManageCapability(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedHeroPresetPackage(t, priv, "")

	presets := preset.NewStore(testDB(t))
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := presets.InstallFromPackage(context.Background(), editor, sp, pub, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as editor err = %v, want ErrDenied", err)
	}
	if _, err := presets.InstallFromPackage(context.Background(), nil, sp, pub, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as anonymous err = %v, want ErrDenied", err)
	}
}
