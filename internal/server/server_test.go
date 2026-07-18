package server_test

// Integration test for the Phase-0 walking skeleton: first boot serves the
// wizard, completing it writes the initial composition and locks the route,
// and the contract-driven endpoints survive a daemon restart on the same
// database (§16 Phase 0 definition of done).

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/api"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/server"
	"github.com/glyphux/glyphux/internal/setup"
)

// boot assembles the daemon's handler on the given database file, exactly as
// cmd/glyphuxd does.
func boot(t *testing.T, dbPath string) http.Handler {
	t.Helper()
	return bootWithCORS(t, dbPath, nil)
}

// bootWithCORS is boot, but with an explicit allowed-origins list wired in
// (slice 1.9) — pass nil to preserve the "no CORS ever" default boot uses.
func bootWithCORS(t *testing.T, dbPath string, allowedOrigins []string) http.Handler {
	t.Helper()
	ctx := context.Background()
	database, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })

	migrations := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migrations = append(migrations, content.Migrations...)
	migrations = append(migrations, media.Migrations...)
	if err := database.Migrate(ctx, migrations); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	compositions := composition.NewStore(database)
	identities := identity.NewService(database)
	sessions := identity.NewSessions(database)
	wizard, err := setup.New(ctx, compositions, identities, database, log, false)
	if err != nil {
		t.Fatal(err)
	}
	mediaAPI := media.NewAPI(media.NewStore(database), filepath.Join(filepath.Dir(dbPath), "media"))
	apiServer := api.New(compositions, content.NewAPI(compositions, content.NewStore(database)), mediaAPI, identities, sessions, log)
	return server.Handler(apiServer, wizard, server.WithCORS(allowedOrigins))
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.RemoteAddr = "127.0.0.1:12345"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func postForm(t *testing.T, h http.Handler, path string, form url.Values, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// postFormHTTPS is postForm for a simulated remote HTTPS request — needed
// because §6.4 now rejects remote setup access outright over plain HTTP.
func postFormHTTPS(t *testing.T, h http.Handler, path string, form url.Values, remoteAddr string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	req.TLS = &tls.ConnectionState{}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestFirstRunLifecycle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	h := boot(t, dbPath)

	// Pre-setup: health works, contract endpoints refuse, root redirects to wizard.
	if rec := get(t, h, "/healthz"); rec.Code != http.StatusOK {
		t.Fatalf("/healthz = %d", rec.Code)
	}
	if rec := get(t, h, "/api/v0/composition"); rec.Code != http.StatusConflict {
		t.Fatalf("pre-setup /api/v0/composition = %d, want 409", rec.Code)
	}
	if rec := get(t, h, "/"); rec.Code != http.StatusTemporaryRedirect || rec.Header().Get("Location") != "/setup" {
		t.Fatalf("pre-setup / = %d → %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := get(t, h, "/setup"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Welcome to Glyphux") {
		t.Fatalf("/setup form: %d", rec.Code)
	}

	// Complete the wizard from localhost (no token required).
	form := url.Values{
		"site_name":      {"Phase Zero"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"sqlite"},
	}
	if rec := postForm(t, h, "/setup", form, "127.0.0.1:9"); rec.Code != http.StatusOK {
		t.Fatalf("wizard submit = %d: %s", rec.Code, rec.Body.String())
	}

	// Post-setup: the route is permanently locked.
	if rec := get(t, h, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("post-setup GET /setup = %d, want 410", rec.Code)
	}
	if rec := postForm(t, h, "/setup", form, "127.0.0.1:9"); rec.Code != http.StatusGone {
		t.Fatalf("post-setup POST /setup = %d, want 410", rec.Code)
	}

	// The contract-driven domain API now serves the composition it wrote.
	rec := get(t, h, "/api/v0/content/ping")
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/v0/content/ping = %d", rec.Code)
	}
	var ping struct {
		ContractVersion string `json:"contract_version"`
		Site            string `json:"site"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &ping); err != nil {
		t.Fatal(err)
	}
	if ping.Site != "Phase Zero" || ping.ContractVersion != "content-composition/v0" {
		t.Errorf("ping = %+v", ping)
	}

	// Restart: a fresh boot on the same database keeps state and the lock.
	h2 := boot(t, dbPath)
	if rec := get(t, h2, "/setup"); rec.Code != http.StatusGone {
		t.Fatalf("after restart GET /setup = %d, want 410", rec.Code)
	}
	if rec := get(t, h2, "/api/v0/composition"); rec.Code != http.StatusOK {
		t.Fatalf("after restart /api/v0/composition = %d", rec.Code)
	}
}

func TestWizardRequiresTokenForRemoteAccess(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	form := url.Values{
		"site_name":      {"Intruder Site"},
		"admin_email":    {"evil@example.com"},
		"admin_password": {"evil password"},
	}
	// Remote submit without (or with a wrong) token must be rejected — over a
	// simulated HTTPS connection, so this test isolates the token check from
	// the separate §6.4 HTTPS-required check (covered in internal/setup).
	rec := postFormHTTPS(t, h, "/setup", form, "203.0.113.7:4444")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("remote submit without token = %d, want 422", rec.Code)
	}
	form.Set("setup_token", "wrong-token")
	rec = postFormHTTPS(t, h, "/setup", form, "203.0.113.7:4444")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("remote submit with wrong token = %d, want 422", rec.Code)
	}
	// Setup must still be pending afterward.
	if rec := get(t, h, "/api/v0/composition"); rec.Code != http.StatusConflict {
		t.Fatalf("composition after failed remote setup = %d, want 409", rec.Code)
	}
}

// Oversized request bodies are rejected before they reach a handler — a DoS
// defense (slice 1.9: security primitives).
func TestOversizedRequestBodyRejected(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	huge := strings.Repeat("a", 2<<20) // 2 MiB, well over the request cap
	body := `{"email":"admin@example.com","password":"` + huge + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body = %d, want 413", rec.Code)
	}
}

// TestGeneralAPIRateLimitPerRemoteAddress proves the daemon throttles a
// single remote address that hammers the API surface generally — not just
// failed logins (internal/api/ratelimit.go's loginLimiter) — while leaving
// /healthz reachable for an orchestrator's liveness polling and leaving a
// different remote address wholly unaffected (slice 1.9: the tracking doc's
// open question on general rate limiting, resolved in favor of including
// it).
func TestGeneralAPIRateLimitPerRemoteAddress(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	hit := func(remoteAddr string) int {
		req := httptest.NewRequest(http.MethodGet, "/api/v0/content/ping", nil)
		req.RemoteAddr = remoteAddr
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	var last int
	for i := 0; i < server.RequestsPerWindow+5; i++ {
		last = hit("203.0.113.9:1")
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last request from a hammering address = %d, want 429", last)
	}

	// A different remote address has its own independent budget.
	if got := hit("203.0.113.10:1"); got == http.StatusTooManyRequests {
		t.Fatalf("unrelated remote address was rate-limited too: %d", got)
	}

	// Liveness polling is exempt from the general limiter.
	healthReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthReq.RemoteAddr = "203.0.113.9:1"
	healthRec := httptest.NewRecorder()
	h.ServeHTTP(healthRec, healthReq)
	if healthRec.Code != http.StatusOK {
		t.Fatalf("/healthz for a rate-limited address = %d, want 200", healthRec.Code)
	}
}

// Every response carries baseline security headers, and cross-origin
// requests are not granted CORS access by default (slice 1.9).
func TestSecurityHeadersAndNoCORSByDefault(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q, want DENY", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Access-Control-Allow-Origin = %q, want unset for an unconfigured cross-origin request", got)
	}
}

// TestCORSAppliesOnlyWhenConfigured proves CORS is opt-in (slice 1.9): an
// origin not on the configured allow-list gets no CORS headers at all (same
// as the default posture), while a listed origin gets the headers a
// cross-origin browser client needs, on both a simple request and a
// preflight OPTIONS request.
func TestCORSAppliesOnlyWhenConfigured(t *testing.T) {
	h := bootWithCORS(t, filepath.Join(t.TempDir(), "glyphux.db"), []string{"https://trusted.example"})

	// Unlisted origin: no CORS headers, exactly like the unconfigured default.
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("unlisted origin Access-Control-Allow-Origin = %q, want unset", got)
	}

	// Listed origin: a simple GET gets the origin echoed back.
	req = httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "https://trusted.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://trusted.example" {
		t.Errorf("listed origin Access-Control-Allow-Origin = %q, want https://trusted.example", got)
	}

	// Listed origin: a CORS preflight gets the methods/headers it needs.
	req = httptest.NewRequest(http.MethodOptions, "/api/v0/content/article", nil)
	req.Header.Set("Origin", "https://trusted.example")
	req.Header.Set("Access-Control-Request-Method", "POST")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "POST") {
		t.Errorf("Access-Control-Allow-Methods = %q, want it to include POST", got)
	}
}

// Media uploads carry real file bytes and are exempt from the generic 1 MiB
// request cap; a payload well over that cap must still reach the handler
// rather than being rejected by the global body limiter (slice 1.6).
func TestMediaUploadExemptFromGlobalBodyCap(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))

	oversized := bytes.Repeat([]byte{0xFF}, 2<<20) // 2 MiB — over the generic 1 MiB cap
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", "big.bin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(oversized); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/media", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	// Unauthenticated, so 401 — but critically not 413: the body was fully
	// read and parsed rather than rejected by the global 1 MiB body limiter.
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("oversized media upload = %d, want 401 (not 413 — the body must not hit the global cap)", rec.Code)
	}
}

// The wizard's "database" field records a choice for the record; it does not
// itself switch backends (that happens via GLYPHUX_DB_DRIVER before the
// daemon boots, slice 1.10), so "postgres" is an accepted value here.
func TestWizardRejectsPostgresChoiceWithoutDSN(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))
	form := url.Values{
		"site_name":      {"PG Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"postgres"},
	}
	rec := postForm(t, h, "/setup", form, "127.0.0.1:9")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("postgres choice without DSN = %d, want 422: %s", rec.Code, rec.Body.String())
	}
}

func TestWizardRejectsUnknownDatabaseChoice(t *testing.T) {
	h := boot(t, filepath.Join(t.TempDir(), "glyphux.db"))
	form := url.Values{
		"site_name":      {"Bad DB Site"},
		"admin_email":    {"admin@example.com"},
		"admin_password": {"strong password"},
		"database":       {"mysql"},
	}
	rec := postForm(t, h, "/setup", form, "127.0.0.1:9")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("unknown db choice = %d, want 422", rec.Code)
	}
}
