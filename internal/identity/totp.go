package identity

// RFC 6238 TOTP (Time-based One-Time Password), the standard, provider-
// agnostic second factor: no external dependency, works with any
// authenticator app (Google Authenticator, Authy, 1Password, ...). This
// file has no Service state — it is pure code over a secret and a clock, so
// mfa.go can unit test enrollment/verification without touching the
// database for the crypto itself.

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

const (
	totpSecretBytes = 20 // 160 bits, the RFC 4226 recommendation for HMAC-SHA1
	totpDigits      = 6
	totpPeriod      = 30 * time.Second
	// totpSkewSteps tolerates clock drift between the server and the
	// authenticator device: one step (30s) either side of the current one.
	totpSkewSteps = 1
)

var base32NoPad = base32.StdEncoding.WithPadding(base32.NoPadding)

// generateTOTPSecret returns a fresh random secret, base32-encoded (the
// otpauth:// URI and every authenticator app expect base32, not raw bytes).
func generateTOTPSecret() (string, error) {
	raw := make([]byte, totpSecretBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate TOTP secret: %w", err)
	}
	return base32Encode(raw), nil
}

// base32Encode is the shared secret encoding, split out so tests can encode a
// known RFC 6238 test-vector secret without going through generateTOTPSecret.
func base32Encode(raw []byte) string {
	return base32NoPad.EncodeToString(raw)
}

// otpauthURL builds the otpauth:// URI enrollment QR codes encode. issuer and
// accountName both appear in the authenticator app's UI so the user can tell
// accounts apart.
func otpauthURL(issuer, accountName, secret string) string {
	label := issuer + ":" + accountName
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", strconv.Itoa(totpDigits))
	v.Set("period", strconv.Itoa(int(totpPeriod.Seconds())))
	return "otpauth://totp/" + url.PathEscape(label) + "?" + v.Encode()
}

// totpCodeAt computes the TOTP code for secret at the time step containing
// at, per RFC 6238 (itself RFC 4226/HOTP keyed by a time counter instead of
// an incrementing one).
func totpCodeAt(secret string, at time.Time) string {
	key, err := base32NoPad.DecodeString(secret)
	if err != nil {
		return ""
	}
	counter := uint64(at.Unix()) / uint64(totpPeriod.Seconds())
	return hotp(key, counter)
}

// hotp is RFC 4226's HMAC-based OTP algorithm: an HMAC-SHA1 of the counter,
// dynamically truncated to a totpDigits-digit code.
func hotp(key []byte, counter uint64) string {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)

	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 |
		uint32(sum[offset+1])<<16 |
		uint32(sum[offset+2])<<8 |
		uint32(sum[offset+3])
	mod := uint32(1)
	for i := 0; i < totpDigits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, code%mod)
}

// validateTOTP reports whether code is valid for secret at "now", tolerating
// totpSkewSteps of clock drift either side of the current time step.
func validateTOTP(secret, code string, now time.Time) bool {
	if len(code) != totpDigits {
		return false
	}
	for skew := -totpSkewSteps; skew <= totpSkewSteps; skew++ {
		at := now.Add(time.Duration(skew) * totpPeriod)
		if constantTimeEq(totpCodeAt(secret, at), code) {
			return true
		}
	}
	return false
}

// constantTimeEq compares two equal-shaped short strings (OTP/recovery
// codes) without leaking timing information about where they first differ.
func constantTimeEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	var diff byte
	for i := 0; i < len(a); i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}
