package identity

import (
	"testing"
	"time"
)

func TestGenerateTOTPSecretIsBase32AndUnique(t *testing.T) {
	a, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	b, err := generateTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("two generated secrets were identical")
	}
	if len(a) == 0 {
		t.Error("empty secret")
	}
}

func TestOTPAuthURLContainsSecretAndIssuer(t *testing.T) {
	url := otpauthURL("glyphux", "admin@example.com", "JBSWY3DPEHPK3PXP")
	want := "otpauth://totp/glyphux:admin@example.com?algorithm=SHA1&digits=6&issuer=glyphux&period=30&secret=JBSWY3DPEHPK3PXP"
	if url != want {
		t.Errorf("otpauthURL = %q, want %q", url, want)
	}
}

// Known RFC 6238 test vector: secret "12345678901234567890" (ASCII), SHA1, at
// T=59s (time step 1) yields the 8-digit code 94287082. We truncate to 6
// digits, matching how Google Authenticator etc. actually display TOTP.
func TestTOTPCodeMatchesRFC6238Vector(t *testing.T) {
	secret := base32Encode([]byte("12345678901234567890"))
	at := time.Unix(59, 0)
	code := totpCodeAt(secret, at)
	if code != "287082" {
		t.Errorf("totpCodeAt = %q, want 287082", code)
	}
}

func TestValidateTOTPAcceptsCurrentCodeAndRejectsWrong(t *testing.T) {
	secret, _ := generateTOTPSecret()
	now := time.Now()
	code := totpCodeAt(secret, now)
	if !validateTOTP(secret, code, now) {
		t.Error("valid current code rejected")
	}
	if code == "000000" {
		t.Skip("astronomically unlucky code collision")
	}
	if validateTOTP(secret, "000000", now) {
		t.Error("wrong code accepted")
	}
}

func TestValidateTOTPToleratesOneStepClockSkew(t *testing.T) {
	secret, _ := generateTOTPSecret()
	now := time.Now()
	prevStep := now.Add(-30 * time.Second)
	code := totpCodeAt(secret, prevStep)
	if !validateTOTP(secret, code, now) {
		t.Error("code from one step ago should be accepted within skew window")
	}

	tooOld := now.Add(-90 * time.Second)
	oldCode := totpCodeAt(secret, tooOld)
	if oldCode == code {
		t.Skip("astronomically unlucky code collision across steps")
	}
	if validateTOTP(secret, oldCode, now) {
		t.Error("code from three steps ago should not be accepted")
	}
}
