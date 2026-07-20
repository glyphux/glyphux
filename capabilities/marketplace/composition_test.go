package marketplace

import (
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/pkg/contract"
)

func heroPreset() contract.CompositionPreset {
	return contract.CompositionPreset{
		ContractVersion: contract.CompositionPresetV1,
		Name:            "hero-section",
		Layout: contract.Layout{
			ContractVersion: contract.LayoutCompositionV1,
			Regions: map[string]contract.Region{
				"main": {Blocks: []contract.Block{{Type: "hero"}}},
			},
		},
		Manifest: contract.Manifest{
			RequiresContract: contract.LayoutCompositionV1,
			Blocks:           []string{"hero"},
			Slots:            []string{"main"},
		},
	}
}

func starterBundle() contract.CompositionBundle {
	return contract.CompositionBundle{
		ContractVersion: contract.CompositionBundleV1,
		Name:            "starter-site",
		Theme:           "starter",
		Pages: map[string]contract.Layout{
			"home": {
				ContractVersion: contract.LayoutCompositionV1,
				Regions: map[string]contract.Region{
					"main": {Blocks: []contract.Block{{Type: "hero"}}},
				},
			},
		},
		Manifest: contract.Manifest{
			RequiresContract: contract.LayoutCompositionV1,
			Blocks:           []string{"hero"},
			Slots:            []string{"main"},
		},
	}
}

func TestPackagePresetThenDecodeRoundTrips(t *testing.T) {
	pkg, result, err := PackagePreset("acme-hero-pro", "1.0.0", "proprietary", ">=0.2.0", heroPreset())
	if err != nil {
		t.Fatalf("PackagePreset: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("PackagePreset result = %+v, want compatible", result)
	}
	if pkg.Kind != KindPreset {
		t.Fatalf("Kind = %q, want %q", pkg.Kind, KindPreset)
	}

	decoded, err := DecodePreset(pkg)
	if err != nil {
		t.Fatalf("DecodePreset: %v", err)
	}
	if decoded.Name != "hero-section" {
		t.Fatalf("decoded preset Name = %q, want hero-section", decoded.Name)
	}
}

func TestPackagePresetRejectsUndeclaredBlockUsage(t *testing.T) {
	p := heroPreset()
	p.Manifest.Blocks = nil // no longer declares "hero", but the tree still uses it

	_, result, err := PackagePreset("acme-hero-pro", "1.0.0", "proprietary", "", p)
	if err == nil {
		t.Fatal("PackagePreset with an undeclared block usage succeeded, want an error")
	}
	if result.Compatible {
		t.Fatalf("result.Compatible = true, want false (result: %+v)", result)
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
}

func TestPackagePresetRejectsStructurallyInvalidPreset(t *testing.T) {
	p := heroPreset()
	p.ContractVersion = "wrong-version"
	if _, _, err := PackagePreset("x", "1.0.0", "MIT", "", p); err == nil {
		t.Fatal("PackagePreset with a structurally invalid preset succeeded, want an error")
	}
}

func TestDecodePresetRejectsWrongKind(t *testing.T) {
	pkg, _, err := PackageBundle("acme-starter", "1.0.0", "MIT", "", starterBundle())
	if err != nil {
		t.Fatalf("PackageBundle: %v", err)
	}
	if _, err := DecodePreset(pkg); !errors.Is(err, ErrArtifactKindMismatch) {
		t.Fatalf("DecodePreset(bundle package) err = %v, want ErrArtifactKindMismatch", err)
	}
}

func TestPackageBundleThenDecodeRoundTrips(t *testing.T) {
	pkg, result, err := PackageBundle("acme-starter", "1.0.0", "MIT", ">=0.1.0", starterBundle())
	if err != nil {
		t.Fatalf("PackageBundle: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("PackageBundle result = %+v, want compatible", result)
	}
	if pkg.Kind != KindBundle {
		t.Fatalf("Kind = %q, want %q", pkg.Kind, KindBundle)
	}

	decoded, err := DecodeBundle(pkg)
	if err != nil {
		t.Fatalf("DecodeBundle: %v", err)
	}
	if decoded.Name != "starter-site" {
		t.Fatalf("decoded bundle Name = %q, want starter-site", decoded.Name)
	}
}

func TestPackageBundleRejectsUndeclaredBlockUsage(t *testing.T) {
	b := starterBundle()
	b.Manifest.Blocks = nil

	_, result, err := PackageBundle("acme-starter", "1.0.0", "MIT", "", b)
	if err == nil {
		t.Fatal("PackageBundle with an undeclared block usage succeeded, want an error")
	}
	if result.Compatible {
		t.Fatalf("result.Compatible = true, want false (result: %+v)", result)
	}
}

func TestDecodeBundleRejectsWrongKind(t *testing.T) {
	pkg, _, err := PackagePreset("acme-hero-pro", "1.0.0", "proprietary", "", heroPreset())
	if err != nil {
		t.Fatalf("PackagePreset: %v", err)
	}
	if _, err := DecodeBundle(pkg); !errors.Is(err, ErrArtifactKindMismatch) {
		t.Fatalf("DecodeBundle(preset package) err = %v, want ErrArtifactKindMismatch", err)
	}
}

// TestSignThenVerifyDetectsTamperedLicense proves License now rides inside
// the signed payload alongside the original Name/Version/Manifest/
// ArtifactHash fields (package.go's signingPayload) — a paid preset package
// silently relabeled "free" after signing must fail Verify exactly like a
// tampered artifact byte already does.
func TestSignThenVerifyDetectsTamperedLicense(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pkg, _, err := PackagePreset("acme-hero-pro", "1.0.0", "proprietary", ">=0.2.0", heroPreset())
	if err != nil {
		t.Fatalf("PackagePreset: %v", err)
	}
	sp, err := Sign(priv, pkg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sp.Package.License = "free"
	if err := Verify(pub, sp); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify of a package with a tampered License = %v, want ErrInvalidSignature", err)
	}
}

// TestSignThenVerifyDetectsTamperedRequiresCore mirrors the License test for
// RequiresCore — loosening a declared kernel-version constraint after
// signing must also be caught.
func TestSignThenVerifyDetectsTamperedRequiresCore(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	pkg, _, err := PackagePreset("acme-hero-pro", "1.0.0", "proprietary", ">=5.0.0", heroPreset())
	if err != nil {
		t.Fatalf("PackagePreset: %v", err)
	}
	sp, err := Sign(priv, pkg)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	sp.Package.RequiresCore = ">=0.0.1"
	if err := Verify(pub, sp); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify of a package with a tampered RequiresCore = %v, want ErrInvalidSignature", err)
	}
}

func TestCoreConstraintSatisfied(t *testing.T) {
	cases := []struct {
		name       string
		constraint string
		kernel     string
		want       bool
	}{
		{"empty constraint always satisfied", "", "0.1.0", true},
		{">= satisfied", ">=0.2.0", "0.3.0", true},
		{">= not satisfied", ">=0.5.0", "0.3.0", false},
		{"exact match, no operator", "0.3.0", "0.3.0", true},
		{"exact mismatch, no operator", "0.3.0", "0.4.0", false},
		{"<= satisfied", "<=1.0.0", "0.9.0", true},
		{"invalid constraint fails closed", "not-a-version", "0.3.0", false},
		{"invalid kernel version fails closed", ">=0.1.0", "not-a-version", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CoreConstraintSatisfied(tc.constraint, tc.kernel)
			if got != tc.want {
				t.Errorf("CoreConstraintSatisfied(%q, %q) = %v, want %v", tc.constraint, tc.kernel, got, tc.want)
			}
		})
	}
}
