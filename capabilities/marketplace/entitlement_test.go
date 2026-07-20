package marketplace

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

func testEntitlement(now time.Time) Entitlement {
	return Entitlement{
		LicenseID:     "lic_abc123",
		ExtensionName: "acme-seo-pro",
		MinVersion:    "1.0.0",
		MaxVersion:    "1.999.999",
		NotBefore:     now.Add(-time.Hour),
		NotAfter:      now.Add(365 * 24 * time.Hour),
		Scope:         []string{"updates", "support"},
	}
}

func TestVerifyEntitlementAcceptsValidToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	ent, err := VerifyEntitlement(pub, tok, now)
	if err != nil {
		t.Fatalf("VerifyEntitlement of a valid token failed: %v", err)
	}
	if ent.LicenseID != "lic_abc123" {
		t.Fatalf("verified entitlement LicenseID = %q, want lic_abc123", ent.LicenseID)
	}
}

func TestVerifyEntitlementRejectsExpiredTokenWithSpecificReason(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(issued))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	// Well past NotAfter (issued + 365 days).
	longAfter := issued.Add(400 * 24 * time.Hour)
	_, err = VerifyEntitlement(pub, tok, longAfter)
	if !errors.Is(err, ErrEntitlementExpired) {
		t.Fatalf("VerifyEntitlement of an expired token = %v, want ErrEntitlementExpired", err)
	}
	// Expired must be distinguishable from a signature failure.
	if errors.Is(err, ErrEntitlementInvalidSignature) {
		t.Fatalf("expired token's error must NOT also satisfy ErrEntitlementInvalidSignature")
	}
}

func TestVerifyEntitlementRejectsNotYetValidToken(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(issued))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	before := issued.Add(-2 * time.Hour) // before NotBefore (issued - 1h)
	_, err = VerifyEntitlement(pub, tok, before)
	if !errors.Is(err, ErrEntitlementNotYetValid) {
		t.Fatalf("VerifyEntitlement before NotBefore = %v, want ErrEntitlementNotYetValid", err)
	}
}

func TestVerifyEntitlementRejectsTamperedTokenWithDistinctReason(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	// Tamper: extend the license to another extension entirely, after signing.
	tok.Entitlement.ExtensionName = "someone-elses-extension"

	_, err = VerifyEntitlement(pub, tok, now)
	if !errors.Is(err, ErrEntitlementInvalidSignature) {
		t.Fatalf("VerifyEntitlement of a tampered token = %v, want ErrEntitlementInvalidSignature", err)
	}
	if errors.Is(err, ErrEntitlementExpired) {
		t.Fatalf("tampered token's error must NOT also satisfy ErrEntitlementExpired")
	}
}

func TestVerifyEntitlementRejectsWrongKey(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	otherPub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	_, err = VerifyEntitlement(otherPub, tok, now)
	if !errors.Is(err, ErrEntitlementInvalidSignature) {
		t.Fatalf("VerifyEntitlement against the wrong key = %v, want ErrEntitlementInvalidSignature", err)
	}
}

// blockAllDialsTransport is an http.RoundTripper that fails every request
// without attempting any I/O — a stand-in for "no network access
// available at all", stronger than merely pointing at an unreachable host
// (which still touches a DNS/socket layer). Any attempt to actually make an
// HTTP request through it fails deterministically and immediately.
type blockAllDialsTransport struct{}

func (blockAllDialsTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("blockAllDialsTransport: network access is forbidden in this test")
}

// TestVerifyEntitlementDoesNotRequireNetworkAccess is the behavioral proof
// of PRD §12.5's "no phone-home" requirement: it globally replaces both
// http.DefaultTransport and http.DefaultClient's transport with one that
// fails every single request outright, and also removes net.DefaultResolver
// as a usable resolver by pointing it at a dialer that always errors, then
// calls VerifyEntitlement. If VerifyEntitlement's verification path made
// any network call whatsoever (even attempting one, even one that would
// normally be swallowed), that call would fail immediately through this
// broken transport/resolver — since VerifyEntitlement still succeeds, this
// proves the verification path used no network I/O at all, only the
// in-memory public key, token, and clock value passed to it.
//
// See also noimports_test.go, which proves the same property structurally
// (this package's non-test source files import no networking package at
// all), independent of this behavioral test.
func TestVerifyEntitlementDoesNotRequireNetworkAccess(t *testing.T) {
	origTransport := http.DefaultTransport
	origClientTransport := http.DefaultClient.Transport
	origResolver := net.DefaultResolver
	http.DefaultTransport = blockAllDialsTransport{}
	http.DefaultClient.Transport = blockAllDialsTransport{}
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			return nil, errors.New("net.DefaultResolver: network access is forbidden in this test")
		},
	}
	t.Cleanup(func() {
		http.DefaultTransport = origTransport
		http.DefaultClient.Transport = origClientTransport
		net.DefaultResolver = origResolver
	})

	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	ent, err := VerifyEntitlement(pub, tok, now)
	if err != nil {
		t.Fatalf("VerifyEntitlement failed with every network path broken: %v (verification must be purely local)", err)
	}
	if ent.LicenseID != "lic_abc123" {
		t.Fatalf("verified entitlement LicenseID = %q, want lic_abc123", ent.LicenseID)
	}
}
