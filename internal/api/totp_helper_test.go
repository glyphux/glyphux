package api_test

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"testing"
	"time"
)

// totpCodeForTest computes a real RFC 6238 TOTP code for secret at the
// current time step — an independent implementation from
// internal/identity's (unexported, so unreachable from this external test
// package) so these HTTP-level tests can drive a real end-to-end MFA login
// without depending on identity's internals.
func totpCodeForTest(t *testing.T, secret string) string {
	t.Helper()
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil {
		t.Fatalf("decode secret: %v", err)
	}
	counter := uint64(time.Now().Unix()) / 30
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	code := (uint32(sum[offset])&0x7f)<<24 | uint32(sum[offset+1])<<16 | uint32(sum[offset+2])<<8 | uint32(sum[offset+3])
	return fmt.Sprintf("%06d", code%1_000_000)
}
