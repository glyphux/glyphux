package bundle_test

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/packagefmt"
)

// signedStarterBundlePackage builds a genuinely verified packagefmt
// SignedPackage the way the install path produces one: encode a bundle
// container, then Decode it against the trust set (the same
// archive → packagefmt → InstallFromPackage pipeline the marketplace
// endpoint runs, Ticket T8 / gap 8).
func signedStarterBundlePackage(t *testing.T, pub ed25519.PublicKey, priv ed25519.PrivateKey, requiresCore string) packagefmt.SignedPackage {
	t.Helper()
	payload, err := json.Marshal(starterSite())
	if err != nil {
		t.Fatalf("marshal starter site: %v", err)
	}
	sum := sha256.Sum256(payload)
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "acme-starter-site",
			Version:       "1.0.0",
			Kind:          packagefmt.KindBundle,
			License:       "MIT",
			RequiresCore:  requiresCore,
		},
		Files: []packagefmt.PackageFile{
			{Path: "bundle.json", SHA256: fmt.Sprintf("%x", sum), Data: payload},
		},
		Checksums: map[string]string{"bundle.json": fmt.Sprintf("%x", sum)},
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

// TestInstallFromPackageThenImportMergesWhenHostHasTheBlock mirrors
// internal/preset's identical test: a bundle packaged/signed the way a
// marketplace operator would, installed, then imported through the SAME
// Import method a locally-authored bundle uses.
func TestInstallFromPackageThenImportMergesWhenHostHasTheBlock(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, pub, priv, ">=0.1.0")
	h := testHarness(t)

	installed, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
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
	sp := signedStarterBundlePackage(t, pub, priv, "")
	h := testHarness(t)
	hostRegistry := blocks.New() // "hero" never registered on this host

	installed, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
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

// TestInstallFromPackageTrustsTheVerifiedRepresentation mirrors the preset
// store's test of the same name: InstallFromPackage consumes the output of
// packagefmt.Decode and does NOT re-verify signatures or checksums — that
// gate lives in exactly one place (packagefmt.Decode, whose own suite
// covers tampering and untrusted keys). Defense in depth comes from the
// API layer, which only ever hands this store a Decode-verified package.
func TestInstallFromPackageTrustsTheVerifiedRepresentation(t *testing.T) {
	payload, err := json.Marshal(starterSite())
	if err != nil {
		t.Fatalf("marshal starter site: %v", err)
	}
	sp := packagefmt.SignedPackage{
		Manifest: packagefmt.Manifest{
			SchemaVersion: packagefmt.SchemaVersionV1,
			Name:          "acme-starter-site",
			Version:       "1.0.0",
			Kind:          packagefmt.KindBundle,
			License:       "MIT",
		},
		Files: []packagefmt.PackageFile{
			{Path: "bundle.json", SHA256: "bogus", Data: payload},
		},
		Checksums:  map[string]string{"bundle.json": "bogus"},
		Signatures: []packagefmt.Signature{{KeyID: "untrusted", Algorithm: "ed25519", Value: []byte("bogus")}},
	}

	h := testHarness(t)
	installed, err := h.bundles.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if err != nil {
		t.Fatalf("InstallFromPackage of a Decode-bypassed representation err = %v, want success (verification is packagefmt.Decode's job)", err)
	}
	if installed.Name != "starter-site" {
		t.Fatalf("installed.Name = %q, want starter-site", installed.Name)
	}
}

func TestInstallFromPackageRejectsUnsatisfiedRequiresCore(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, pub, priv, ">=5.0.0")

	h := testHarness(t)
	_, err = h.bundles.InstallFromPackage(context.Background(), admin, sp, "0.5.0")
	if !errors.Is(err, bundle.ErrRequiresCoreNotSatisfied) {
		t.Fatalf("InstallFromPackage with an unsatisfied requires.core err = %v, want ErrRequiresCoreNotSatisfied", err)
	}
}

func TestInstallFromPackageRequiresPresetsManageCapability(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp := signedStarterBundlePackage(t, pub, priv, "")

	h := testHarness(t)
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := h.bundles.InstallFromPackage(context.Background(), editor, sp, "0.5.0"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("InstallFromPackage as editor err = %v, want ErrDenied", err)
	}
}
