package bundle_test

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/capabilities/marketplace"
	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
)

func signedStarterBundlePackage(t *testing.T, priv ed25519.PrivateKey, requiresCore string) marketplace.SignedPackage {
	t.Helper()
	pkg, result, err := marketplace.PackageBundle("acme-starter-site", "1.0.0", "MIT", requiresCore, *starterSite())
	if err != nil {
		t.Fatalf("PackageBundle: %v (result: %+v)", err, result)
	}
	sp, err := marketplace.Sign(priv, pkg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	return sp
}

// TestInstallFromPackageThenImportMergesWhenHostHasTheBlock mirrors
// internal/preset's identical test: a bundle packaged/signed the way a
// marketplace operator would, installed, then imported through the SAME
// Import method a locally-authored bundle uses.
func TestInstallFromPackageThenImportMergesWhenHostHasTheBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, priv, ">=0.1.0")
	h := testHarness(t)

	installed, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage: %v", err)
	}
	if installed.Name != "starter-site" {
		t.Fatalf("installed.Name = %q, want starter-site", installed.Name)
	}

	result, err := h.bundles.Import(context.Background(), admin, h.reg, h.layouts, h.content, []string{"main"}, installed.ID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Compat.Compatible {
		t.Fatalf("Import result = %+v, want compatible", result)
	}
	if len(result.ImportedPages) != 1 || result.ImportedPages[0] != "home" {
		t.Fatalf("ImportedPages = %v, want [home]", result.ImportedPages)
	}
}

// TestInstallFromPackageThenImportDeclinesGracefullyWhenHostLacksTheBlock is
// the graceful-degradation counterpart: InstallFromPackage must succeed
// even though this host lacks the declared block, and Import must decline
// with a structured compat.Result rather than InstallFromPackage
// hard-erroring earlier.
func TestInstallFromPackageThenImportDeclinesGracefullyWhenHostLacksTheBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, priv, "")
	h := testHarness(t)
	hostRegistry := blocks.New() // "hero" never registered on this host

	installed, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage: %v, want it to succeed even though this host lacks the block", err)
	}

	result, err := h.bundles.Import(context.Background(), admin, hostRegistry, h.layouts, h.content, []string{"main"}, installed.ID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Compat.Compatible {
		t.Fatal("Import result.Compat.Compatible = true, want false (host lacks the hero block)")
	}
	if len(result.ImportedPages) != 0 {
		t.Fatalf("ImportedPages = %v, want none (incompatible bundle must merge nothing)", result.ImportedPages)
	}
}

func TestInstallFromPackageRejectsTamperedPackage(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, priv, "")
	sp.Package.Artifact = append([]byte(nil), sp.Package.Artifact...)
	sp.Package.Artifact[0] ^= 0xFF

	h := testHarness(t)
	if _, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0"); !errors.Is(err, marketplace.ErrInvalidSignature) {
		t.Fatalf("InstallFromPackage of a tampered package err = %v, want ErrInvalidSignature", err)
	}
}

func TestInstallFromPackageRejectsUnsatisfiedRequiresCore(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, priv, ">=5.0.0")

	h := testHarness(t)
	_, err = h.bundles.InstallFromPackage(context.Background(), admin, sp, pub, "0.5.0")
	if !errors.Is(err, bundle.ErrRequiresCoreNotSatisfied) {
		t.Fatalf("InstallFromPackage with an unsatisfied requires.core err = %v, want ErrRequiresCoreNotSatisfied", err)
	}
}

func TestInstallFromPackageRequiresPresetsManageCapability(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, priv, "")

	h := testHarness(t)
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := h.bundles.InstallFromPackage(context.Background(), editor, sp, pub, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as editor err = %v, want ErrDenied", err)
	}
}
