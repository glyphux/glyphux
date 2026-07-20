package marketplace

import (
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/glyphux/glyphux/pkg/sdk"
)

func testPackage() Package {
	return Package{
		Name:    "acme-seo-pro",
		Version: "1.2.3",
		Manifest: sdk.Manifest{
			Name:    "acme-seo-pro",
			Version: "1.2.3",
			Runtime: sdk.RuntimeWASM,
			Requires: sdk.Requires{
				Core:     ">=0.2.0",
				Contract: "content-composition/v0",
			},
			API: []sdk.APIScope{
				{Capability: "content", Scopes: []string{"read"}},
			},
		},
		Artifact: []byte("this is not really a wasm binary, just test bytes"),
	}
}

func TestSignThenVerifySucceeds(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp, err := Sign(priv, testPackage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if err := Verify(pub, sp); err != nil {
		t.Fatalf("Verify of a genuinely signed package failed: %v", err)
	}
}

func TestVerifyRejectsTamperedArtifact(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp, err := Sign(priv, testPackage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Flip a single byte in the artifact after signing.
	tampered := append([]byte(nil), sp.Package.Artifact...)
	tampered[0] ^= 0xFF
	sp.Package.Artifact = tampered

	if err := Verify(pub, sp); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify of a tampered package = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsTamperedManifest(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	sp, err := Sign(priv, testPackage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Smuggle in an extra permission after review/signing.
	sp.Package.Manifest.Permissions = append(sp.Package.Manifest.Permissions, sdk.Permission{Name: "network", Args: []string{"evil.example"}})

	if err := Verify(pub, sp); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify of a tampered manifest = %v, want ErrInvalidSignature", err)
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	sp, err := Sign(priv, testPackage())
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	if err := Verify(otherPub, sp); !errors.Is(err, ErrInvalidSignature) {
		t.Fatalf("Verify against the wrong public key = %v, want ErrInvalidSignature", err)
	}
}
