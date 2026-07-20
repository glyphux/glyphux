package setup_test

// §6.4: HTTPS is required for any non-localhost wizard access in production.
// A remote request that isn't TLS (and isn't explicitly vouched for by a
// trusted proxy) must never reach the form or the submit handler, since both
// carry secrets (the setup token, the admin password).

import (
	"context"
	"crypto/tls"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/setup"
)

func newTestWizard(t *testing.T, trustProxyHeaders bool) *setup.Wizard {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	w, err := setup.New(context.Background(), composition.NewStore(d), identity.NewService(d), d, log, trustProxyHeaders)
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func handler(w *setup.Wizard) http.Handler {
	mux := http.NewServeMux()
	w.Routes(mux)
	return mux
}

func remoteGet(h http.Handler, secure bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "203.0.113.7:4444"
	if secure {
		req.TLS = &tls.ConnectionState{}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandleFormRejectsRemoteInsecureRequest(t *testing.T) {
	h := handler(newTestWizard(t, false))
	rec := remoteGet(h, false)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote insecure GET /setup = %d, want 403", rec.Code)
	}
}

func TestHandleFormAllowsRemoteHTTPSRequest(t *testing.T) {
	h := handler(newTestWizard(t, false))
	rec := remoteGet(h, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("remote HTTPS GET /setup = %d, want 200", rec.Code)
	}
}

func TestHandleFormTrustsForwardedProtoOnlyWhenConfigured(t *testing.T) {
	untrusted := handler(newTestWizard(t, false))
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "203.0.113.7:4444"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	untrusted.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("untrusted proxy header GET /setup = %d, want 403 (header must not be trusted by default)", rec.Code)
	}

	trusted := handler(newTestWizard(t, true))
	req2 := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req2.RemoteAddr = "203.0.113.7:4444"
	req2.Header.Set("X-Forwarded-Proto", "https")
	rec2 := httptest.NewRecorder()
	trusted.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("trusted proxy header GET /setup = %d, want 200", rec2.Code)
	}
}

func TestHandleSubmitRejectsRemoteInsecureRequest(t *testing.T) {
	w := newTestWizard(t, false)
	h := handler(w)

	form := url.Values{
		"site_name":      {"Intruder Site"},
		"admin_email":    {"evil@example.com"},
		"admin_password": {"evil password"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "203.0.113.7:4444"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("remote insecure POST /setup = %d, want 403", rec.Code)
	}
	if w.Complete() {
		t.Fatal("setup marked complete after a rejected insecure remote submit")
	}
}

// A failed submission must never leave a partial admin account behind — the
// original bug (§17): a composition-save failure after the admin account
// had already been created meant retrying with the same email failed on a
// duplicate. The default commit path now wraps both writes in one
// transaction (db.WithTx), so a failed attempt writes nothing at all.
func TestFailedSubmitDoesNotBlockRetryWithSameEmail(t *testing.T) {
	w := newTestWizard(t, false)
	h := handler(w)

	weak := url.Values{
		"site_name":      {"Retry Site"},
		"admin_email":    {"retry@example.com"},
		"admin_password": {"short"}, // under the 8-char minimum: createAccountWith rejects it
		"database":       {"sqlite"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(weak.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("weak-password submit = %d, want 422: %s", rec.Code, rec.Body.String())
	}
	if w.Complete() {
		t.Fatal("wizard marked complete after a rejected submission")
	}

	retry := url.Values{
		"site_name":      {"Retry Site"},
		"admin_email":    {"retry@example.com"}, // same email as the failed attempt
		"admin_password": {"strong enough password"},
		"database":       {"sqlite"},
	}
	req2 := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(retry.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req2.RemoteAddr = "127.0.0.1:9"
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("retry with same email = %d, want 200: %s", rec2.Code, rec2.Body.String())
	}
	if !w.Complete() {
		t.Fatal("wizard not marked complete after a successful retry")
	}
}

func TestHandleFormAllowsLocalhostWithoutTLS(t *testing.T) {
	h := handler(newTestWizard(t, false))
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.RemoteAddr = "127.0.0.1:9"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("localhost GET /setup = %d, want 200", rec.Code)
	}
}
