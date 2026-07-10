package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Repeated failed logins from the same remote address are throttled — a core
// brute-force defense (slice 1.9: security primitives).
func TestLoginIsRateLimitedPerRemoteAddr(t *testing.T) {
	h, deps := testServerWithAuth(t)
	ctx := context.Background()
	if err := deps.identities.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
	}

	loginFrom := func(remoteAddr string) int {
		body := map[string]any{"email": "admin@example.com", "password": "wrong"}
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", bytes.NewReader(b))
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	const attempts = 20
	sawTooManyRequests := false
	for i := 0; i < attempts; i++ {
		code := loginFrom("198.51.100.7:5555")
		if code == http.StatusTooManyRequests {
			sawTooManyRequests = true
			break
		}
		if code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401 or 429", i, code)
		}
	}
	if !sawTooManyRequests {
		t.Fatalf("%d failed logins from one address never triggered 429", attempts)
	}

	// A different remote address is not affected by the first one's limit.
	if code := loginFrom("203.0.113.9:6666"); code != http.StatusUnauthorized {
		t.Errorf("fresh address login = %d, want 401 (not rate limited)", code)
	}
}
