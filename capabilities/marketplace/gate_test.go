package marketplace

import (
	"crypto/ed25519"
	"errors"
	"testing"
	"time"
)

// TestCanExecuteAlwaysAllows proves CanExecute takes no entitlement input at
// all and is unconditionally true — the strongest form of PRD §12.5's
// "no license check ever disables a running production site" rule: there
// is no token, key, or clock a caller could even attempt to pass in that
// would make it return false.
func TestCanExecuteAlwaysAllows(t *testing.T) {
	if !CanExecute() {
		t.Fatal("CanExecute() = false, want true unconditionally")
	}
}

// TestCanExecuteNeverConflatesWithUpdateGate is the explicit regression
// test the ticket calls for: it constructs a token that is definitively
// NOT entitled to fetch updates (expired, long past its window) and proves
// that fact via CanFetchUpdate, then proves CanExecute's answer is
// completely unaffected by that — because CanExecute doesn't even accept a
// token. If a future change ever "wired the run gate to check entitlement"
// (e.g. by giving CanExecute a token parameter and an early-return on
// VerifyEntitlement failure), this test's second half would need to start
// passing the same expired token to CanExecute, and if that code path
// existed and rejected, this test's assertion that execution is still
// allowed would fail.
func TestCanExecuteNeverConflatesWithUpdateGate(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issued := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	longExpired := Entitlement{
		LicenseID:     "lic_expired",
		ExtensionName: "acme-seo-pro",
		MinVersion:    "1.0.0",
		NotBefore:     issued,
		NotAfter:      issued.Add(30 * 24 * time.Hour), // expired years ago
		Scope:         []string{"updates"},
	}
	tok, err := IssueEntitlement(priv, longExpired)
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

	// Confirm the update gate genuinely rejects this token (sanity check
	// that this test's premise — "definitely not entitled to update" — is
	// real, not assumed).
	canUpdate, err := CanFetchUpdate(pub, tok, "1.1.0", now)
	if canUpdate || !errors.Is(err, ErrEntitlementExpired) {
		t.Fatalf("CanFetchUpdate(expired token) = (%v, %v), want (false, ErrEntitlementExpired)", canUpdate, err)
	}

	// Execution must still be allowed — CanExecute has no entitlement
	// input to gate on in the first place.
	if !CanExecute() {
		t.Fatal("CanExecute() = false with an expired entitlement in play; execution must NEVER be gated by entitlement status")
	}
}

func TestCanFetchUpdateAllowsWithinEntitledVersionRange(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	ok, err := CanFetchUpdate(pub, tok, "1.5.0", now)
	if err != nil || !ok {
		t.Fatalf("CanFetchUpdate(in-range version) = (%v, %v), want (true, nil)", ok, err)
	}
}

func TestCanFetchUpdateRejectsVersionOutsideEntitledRange(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(now)) // entitled to 1.0.0-1.999.999

	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	ok, err := CanFetchUpdate(pub, tok, "2.0.0", now)
	if ok || !errors.Is(err, ErrUpdateVersionNotEntitled) {
		t.Fatalf("CanFetchUpdate(out-of-range version) = (%v, %v), want (false, ErrUpdateVersionNotEntitled)", ok, err)
	}
}

func TestCanFetchUpdateRejectsExpiredEntitlement(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(issued))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	longAfter := issued.Add(400 * 24 * time.Hour)
	ok, err := CanFetchUpdate(pub, tok, "1.5.0", longAfter)
	if ok || !errors.Is(err, ErrEntitlementExpired) {
		t.Fatalf("CanFetchUpdate(expired) = (%v, %v), want (false, ErrEntitlementExpired)", ok, err)
	}
}

func TestDescribeExpiryClassifiesTokenState(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	issued := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	tok, err := IssueEntitlement(priv, testEntitlement(issued))
	if err != nil {
		t.Fatalf("IssueEntitlement: %v", err)
	}

	if got := DescribeExpiry(pub, tok, issued); got != ExpiryActive {
		t.Fatalf("DescribeExpiry(valid) = %q, want %q", got, ExpiryActive)
	}
	if got := DescribeExpiry(pub, tok, issued.Add(-2*time.Hour)); got != ExpiryNotYetValid {
		t.Fatalf("DescribeExpiry(before window) = %q, want %q", got, ExpiryNotYetValid)
	}
	if got := DescribeExpiry(pub, tok, issued.Add(400*24*time.Hour)); got != ExpiryExpired {
		t.Fatalf("DescribeExpiry(after window) = %q, want %q", got, ExpiryExpired)
	}

	tampered := tok
	tampered.Entitlement.LicenseID = "someone-else"
	if got := DescribeExpiry(pub, tampered, issued); got != ExpiryInvalid {
		t.Fatalf("DescribeExpiry(tampered) = %q, want %q", got, ExpiryInvalid)
	}
}
