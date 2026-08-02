package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/consent"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/internal/setup"
	"github.com/glyphux/glyphux/pkg/contract"
)

// bootDaemon runs the real buildFullHandler the daemon serves from: a real
// SQLite database migrated with the same migration list main.go appends,
// a seeded composition (setup complete), an admin account, and the real
// setup wizard. This is the daemon-level httptest the T5 acceptance
// criteria call for — no mocks anywhere except the fake AI provider each
// AI test stands up itself.
func bootDaemon(t *testing.T, cfg config.Config) (http.Handler, *db.DB) {
	t.Helper()
	return bootDaemonAt(t, cfg, filepath.Join(t.TempDir(), "test.db"), true)
}

// bootDaemonAt is bootDaemon over an explicit SQLite path — the restart-
// persistence seam (a decision made on one boot must survive a daemon
// restart onto the same database file). seed=false skips the first-run
// seeding (composition save + admin account) for a restart onto an already
// provisioned database.
func bootDaemonAt(t *testing.T, cfg config.Config, dbPath string, seed bool) (http.Handler, *db.DB) {
	t.Helper()
	p := bootDaemonParts(t, cfg, dbPath, seed)
	return p.h, p.d
}

// daemonParts is what a daemon boot builds: the full handler plus the
// domain pieces a test needs to drive the post-setup handler rebuild — the
// wizard committer's gateway switch — from a virgin boot.
type daemonParts struct {
	h      http.Handler
	d      *db.DB
	comps  *composition.Store
	ids    *identity.Service
	wizard *setup.Wizard
}

// bootDaemonParts is bootDaemonAt returning every daemon piece instead of
// just the handler. seed=false skips the first-run seeding for a restart
// onto an already provisioned database — and, for the virgin-install
// regression test, boots the daemon with no composition stored at all.
func bootDaemonParts(t *testing.T, cfg config.Config, dbPath string, seed bool) daemonParts {
	t.Helper()
	ctx := context.Background()

	d, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })

	migs := append(append([]db.Migration{}, composition.Migrations...), identity.Migrations...)
	migs = append(migs, content.Migrations...)
	migs = append(migs, media.Migrations...)
	migs = append(migs, layout.Migrations...)
	migs = append(migs, preset.Migrations...)
	migs = append(migs, pluginstore.Migrations...)
	migs = append(migs, audit.Migrations...)
	migs = append(migs, consent.Migrations...)
	if err := d.Migrate(ctx, migs); err != nil {
		t.Fatal(err)
	}

	comps := composition.NewStore(d)
	if seed {
		if err := comps.Save(ctx, nil, &contract.Composition{
			ContractVersion: contract.ContentCompositionV0,
			Site:            contract.Site{Name: "Test"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	ids := identity.NewService(d)
	if seed {
		if err := ids.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
			t.Fatal(err)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wizard, err := setup.New(ctx, comps, ids, d, log, false)
	if err != nil {
		t.Fatal(err)
	}

	h, err := buildFullHandler(cfg, log)(d, comps, ids, wizard)
	if err != nil {
		t.Fatalf("buildFullHandler: %v", err)
	}
	return daemonParts{h: h, d: d, comps: comps, ids: ids, wizard: wizard}
}

// daemonSession carries the two cookies a real admin browser holds after
// login (session + CSRF) and mirrors the CSRF token back in the header —
// the exact double-submit pattern internal/api/csrf.go enforces.
type daemonSession struct{ session, csrf string }

func (c daemonSession) addTo(r *http.Request) {
	r.AddCookie(&http.Cookie{Name: "glyphux_session", Value: c.session})
	r.AddCookie(&http.Cookie{Name: "glyphux_csrf", Value: c.csrf})
	r.Header.Set("X-CSRF-Token", c.csrf)
}

func loginAdmin(t *testing.T, h http.Handler) daemonSession {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"email": "admin@example.com", "password": "correct horse battery"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/v0/auth/login = %d, body %s", rec.Code, rec.Body.String())
	}
	var sess daemonSession
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "glyphux_session":
			sess.session = c.Value
		case "glyphux_csrf":
			sess.csrf = c.Value
		}
	}
	if sess.session == "" || sess.csrf == "" {
		t.Fatalf("login response missing session/csrf cookies: %v", rec.Result().Cookies())
	}
	return sess
}

func doJSON(t *testing.T, h http.Handler, method, path string, c daemonSession, body any) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req := httptest.NewRequest(method, path, r)
	c.addTo(req)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	return out
}

// fakeProvider is the httptest fake AI provider: an OpenAI-shaped
// chat-completions endpoint that returns completion verbatim as the model's
// answer — the documented fake-adapter seam (internal/api/ai_test.go's
// fakeAIAdapter is the in-process variant; this one exercises the real
// openai-compatible adapter's HTTP transport end to end).
func fakeProvider(t *testing.T, completion string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"model":"fake-model","choices":[{"message":{"role":"assistant","content":%s},"finish_reason":"stop"}]}`, strconv.Quote(completion))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// validFragmentJSON is a composition-preset fragment using only registered
// block types — the same proven-valid shape internal/api/ai_test.go uses to
// prove the compose endpoint's validation path.
const validFragmentJSON = `{
	"contract_version": "composition-preset/v1",
	"name": "ai-hero",
	"description": "a hero section",
	"layout": {
		"contract_version": "layout-composition/v1",
		"regions": {
			"main": {
				"blocks": [ { "type": "heading", "props": { "text": "Welcome" } } ]
			}
		}
	},
	"manifest": {
		"requires_contract": "layout-composition/v1",
		"blocks": ["heading"],
		"slots": ["main"]
	}
}`

// --- Ticket T5 acceptance criteria, daemon-level ---

// TestDaemonRegistersFirstPartyCapabilities boots the real daemon and
// proves the five first-party capability plugins registered through the
// registrar are API-visible: their content types on GET /api/v0/content-types
// and only-first-party blocks on GET /api/v0/blocks.
func TestDaemonRegistersFirstPartyCapabilities(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	h, _ := bootDaemon(t, cfg)

	rec := doJSON(t, h, http.MethodGet, "/api/v0/content-types", daemonSession{}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /content-types = %d, body %s", rec.Code, rec.Body.String())
	}
	types, _ := decodeBody(t, rec)["content_types"].(map[string]any)
	for _, want := range []string{"form_submission", "product", "order", "membership_tier", "membership_subscription"} {
		if _, ok := types[want]; !ok {
			t.Errorf("content_types missing %q (got %v)", want, keysOf(types))
		}
	}

	rec = doJSON(t, h, http.MethodGet, "/api/v0/blocks", daemonSession{}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /blocks = %d, body %s", rec.Code, rec.Body.String())
	}
	blockList, _ := decodeBody(t, rec)["blocks"].([]any)
	names := map[string]bool{}
	for _, b := range blockList {
		if def, ok := b.(map[string]any); ok {
			if n, ok := def["name"].(string); ok {
				names[n] = true
			}
		}
	}
	// Only first-party blocks: none of the five capability plugins registers
	// blocks, so the block surface is exactly blocks/firstparty's set.
	for _, want := range []string{"heading", "paragraph", "image", "container"} {
		if !names[want] {
			t.Errorf("blocks missing first-party %q (got %v)", want, names)
		}
	}
	if len(blockList) != 4 {
		t.Errorf("blocks = %d entries, want exactly the 4 first-party blocks", len(blockList))
	}
}

func keysOf(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestDaemonAIComposeLiveWhenConfigured is the WithAI wiring proof: with
// ai.provider configured to a real (openai-compatible) adapter pointed at an
// httptest fake provider, a booted daemon serves POST /api/v0/ai/compose
// with 200 and a compatible fragment.
func TestDaemonAIComposeLiveWhenConfigured(t *testing.T) {
	provider := fakeProvider(t, validFragmentJSON)
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AI = config.AIConfig{
		Provider: "openai-compatible",
		Model:    "fake-model",
		BaseURL:  provider.URL,
		APIKey:   "sk-test",
	}
	h, _ := bootDaemon(t, cfg)
	sess := loginAdmin(t, h)

	rec := doJSON(t, h, http.MethodPost, "/api/v0/ai/compose", sess, map[string]any{
		"prompt": "a friendly hero section",
		"route":  "home",
		"model":  "fake-model",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("POST ai/compose = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := decodeBody(t, rec)["compatible"]; got != true {
		t.Errorf("compatible = %v, want true", got)
	}
}

// TestDaemonAICompose404sWhenUnset proves AI stays disabled with no
// ai.provider: the daemon boots fine with no API key anywhere, and the
// compose endpoint 404s (its default when WithAI is absent).
func TestDaemonAICompose404sWhenUnset(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	h, _ := bootDaemon(t, cfg)
	sess := loginAdmin(t, h)

	rec := doJSON(t, h, http.MethodPost, "/api/v0/ai/compose", sess, map[string]any{
		"prompt": "a friendly hero section",
		"model":  "fake-model",
	})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("POST ai/compose without AI = %d, want 404, body %s", rec.Code, rec.Body.String())
	}
}

// TestDaemonUnknownAIProviderFailsFast proves an unknown ai.provider is a
// boot-time error naming the valid adapter set — the daemon never starts.
func TestDaemonUnknownAIProviderFailsFast(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.AI = config.AIConfig{Provider: "unknown-adapter"}

	// buildFullHandler must fail before any request can be served; drive it
	// the same way bootstrap would, with real stores, and require the error.
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx, composition.Migrations); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(ctx, nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}
	ids := identity.NewService(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wizard, err := setup.New(ctx, comps, ids, d, log, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildFullHandler(cfg, log)(d, comps, ids, wizard)
	if err == nil {
		t.Fatal("expected boot to fail on unknown ai.provider")
	}
	for _, want := range []string{"claude", "openai", "gemini", "openai-compatible"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("fail-fast error %q does not name valid provider %q", err, want)
		}
	}
}

// TestDaemonConsentPersistsAcrossRestart is the acceptance criterion
// "decision made in UI; daemon restarts (real SQLite); decision persists":
// an admin makes a partial consent decision on one boot of the real daemon,
// the database is closed and re-opened on the same file, and a fresh boot
// serves the same granted subset — plus, a denied plugin surfaces re-consent
// as pending on the restarted daemon.
func TestDaemonConsentPersistsAcrossRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	cfg := config.Default()
	cfg.DataDir = t.TempDir()

	// Boot 1: deny commerce (so it surfaces as pending re-consent), then
	// partially approve forms (content:read only).
	h1, d1 := bootDaemonAt(t, cfg, dbPath, true)
	sess1 := loginAdmin(t, h1)

	rec := doJSON(t, h1, http.MethodPost, "/api/v0/plugins/consent-requests/commerce/decide", sess1, map[string]any{
		"decision": "denied",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("boot1 decide deny commerce = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, h1, http.MethodPost, "/api/v0/plugins/consent-requests/forms/decide", sess1, map[string]any{
		"decision":    "partial",
		"granted_api": []any{map[string]any{"capability": "content", "scopes": []string{"read"}}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("boot1 decide partial forms = %d, body %s", rec.Code, rec.Body.String())
	}
	d1.Close() // the daemon "restarts": same file, new handle

	// Boot 2: the decisions must have persisted.
	h2, _ := bootDaemonAt(t, cfg, dbPath, false)
	sess2 := loginAdmin(t, h2)

	rec = doJSON(t, h2, http.MethodGet, "/api/v0/plugins", sess2, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("boot2 GET /plugins = %d, body %s", rec.Code, rec.Body.String())
	}
	plugins, _ := decodeBody(t, rec)["plugins"].([]any)
	for _, p := range plugins {
		pm := p.(map[string]any)
		switch pm["name"] {
		case "forms":
			if pm["status"] != "partial" {
				t.Errorf("restarted daemon: forms status = %v, want partial (decision persisted)", pm["status"])
			}
		case "commerce":
			if pm["status"] != "denied" {
				t.Errorf("restarted daemon: commerce status = %v, want denied (decision persisted)", pm["status"])
			}
		}
	}

	// Denied commerce surfaces as pending re-consent on the restarted daemon.
	rec = doJSON(t, h2, http.MethodGet, "/api/v0/plugins/consent-requests", sess2, nil)
	reqs, _ := decodeBody(t, rec)["requests"].([]any)
	found := false
	for _, r := range reqs {
		if rm, ok := r.(map[string]any); ok && rm["name"] == "commerce" {
			found = true
		}
	}
	if !found {
		t.Error("restarted daemon: denied commerce must surface as a pending re-consent request")
	}
}

// --- T4 review regression, daemon-level ---

// TestDaemonVirginInstallBootsAndDefersPluginActivationUntilComposition is
// the regression test for the T4 review finding: on a virgin install (no
// composition stored) the daemon must boot successfully and defer first-
// party capability plugin activation until the setup flow commits the
// initial composition. Pre-fix, buildFullHandler activated unconditionally
// and forms/commerce/membership define content types through the host —
// DefineContentType requires the composition document, so activation died
// with "no composition stored" and the daemon never came up. The fixed
// contract: virgin boot succeeds with activation deferred (the content-type
// surface reports setup incomplete, not a crash), and once the composition
// is committed the rebuilt handler (the setup committer's gateway switch)
// activates the five first-party capabilities.
func TestDaemonVirginInstallBootsAndDefersPluginActivationUntilComposition(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// GIVEN a virgin install (no composition stored, no admin account)
	// WHEN the daemon boots THEN boot succeeds — bootDaemonParts fails the
	// test if buildFullHandler returns the pre-fix activation error
	// ("activate plugin forms: no composition stored").
	p := bootDaemonParts(t, cfg, dbPath, false)

	// AND plugin activation is deferred: the content-type surface reports
	// setup incomplete instead of serving a half-activated plugin set.
	rec := doJSON(t, p.h, http.MethodGet, "/api/v0/content-types", daemonSession{}, nil)
	if rec.Code != http.StatusConflict {
		t.Fatalf("virgin boot GET /content-types = %d, want 409 (setup not completed), body %s", rec.Code, rec.Body.String())
	}

	// GIVEN the setup wizard commits the initial composition (the same write
	// the bootstrap committer performs: composition + admin account) WHEN
	// the daemon rebuilds its handler, as the committer's gateway switch
	// does THEN activation completes and the five first-party content types
	// become available via GET /api/v0/content-types.
	if err := p.comps.Save(context.Background(), nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatalf("commit composition: %v", err)
	}
	if err := p.ids.CreateAdmin(context.Background(), "admin@example.com", "correct horse battery"); err != nil {
		t.Fatalf("commit admin: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	h2, err := buildFullHandler(cfg, log)(p.d, p.comps, p.ids, p.wizard)
	if err != nil {
		t.Fatalf("rebuild handler after composition commit: %v", err)
	}

	rec = doJSON(t, h2, http.MethodGet, "/api/v0/content-types", daemonSession{}, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("post-setup GET /content-types = %d, body %s", rec.Code, rec.Body.String())
	}
	types, _ := decodeBody(t, rec)["content_types"].(map[string]any)
	for _, want := range []string{"form_submission", "product", "order", "membership_tier", "membership_subscription"} {
		if _, ok := types[want]; !ok {
			t.Errorf("post-setup content_types missing %q (got %v)", want, keysOf(types))
		}
	}
}

// --- Ticket T6 (gap 1) daemon-level wiring ---

// TestDaemonBootsWithUnconsentedConfiguredWasmPluginRefusedAndContinues is
// the T6 wiring seam + default-policy proof at the daemon level: with a
// plugins dir and one tier-b entry configured via cfg.Plugins but no
// consent decision on file, the daemon must boot — the unconsented plugin
// is refused before a host is built and the daemon continues (the
// acceptance criterion's own "booted" reading of the no-decision policy) —
// and keep serving the first-party surface.
func TestDaemonBootsWithUnconsentedConfiguredWasmPluginRefusedAndContinues(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Plugins = config.PluginsConfig{
		Dir: filepath.Join("..", "..", "pkg", "runtime", "wasm", "testdata"),
		Plugins: []config.PluginConfig{
			{Name: "kv-plugin", Tier: "b", Source: "kv_guest.wasm"},
		},
	}
	h, _ := bootDaemon(t, cfg)
	sess := loginAdmin(t, h)

	rec := doJSON(t, h, http.MethodGet, "/api/v0/plugins", sess, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /plugins = %d, body %s", rec.Code, rec.Body.String())
	}
	plugins, _ := decodeBody(t, rec)["plugins"].([]any)
	found := false
	for _, p := range plugins {
		if pm, ok := p.(map[string]any); ok && pm["name"] == "forms" {
			found = true
		}
	}
	if !found {
		t.Error("first-party forms must remain registered when an unconsented wasm plugin is refused")
	}
}

// TestDaemonFailsFastOnMisconfiguredPluginEntry pins the T6 fatal-fast
// invariant at the daemon boundary: an unknown tier in cfg.Plugins is an
// operator misconfiguration buildFullHandler rejects — the daemon never
// starts (same fail-fast style as an unknown ai.provider, and the loader
// contract's tier validation).
func TestDaemonFailsFastOnMisconfiguredPluginEntry(t *testing.T) {
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	cfg.Plugins = config.PluginsConfig{
		Plugins: []config.PluginConfig{
			{Name: "mystery", Tier: "x", Source: "anything"},
		},
	}

	// Drive buildFullHandler the same way bootstrap would (real stores) and
	// require the error, exactly as TestDaemonUnknownAIProviderFailsFast.
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(ctx, composition.Migrations); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	if err := comps.Save(ctx, nil, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
	}); err != nil {
		t.Fatal(err)
	}
	ids := identity.NewService(d)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	wizard, err := setup.New(ctx, comps, ids, d, log, false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = buildFullHandler(cfg, log)(d, comps, ids, wizard)
	if err == nil {
		t.Fatal("expected boot to fail on a misconfigured plugin entry")
	}
	if !strings.Contains(err.Error(), "mystery") || !strings.Contains(err.Error(), "tier") {
		t.Errorf("fail-fast error %q must name the plugin and the tier", err)
	}
}

// ---- Ticket T7 (gap 4): daemon-level audit activation ----

// saveArticleType replaces the seeded composition with one declaring the
// article content type — the daemon-level content write path needs it
// (bootDaemonParts seeds a site-only composition). Saving over an existing
// composition requires content_types:manage, so the helper writes as an
// admin principal.
func saveArticleType(t *testing.T, p daemonParts) {
	t.Helper()
	if err := p.comps.Save(context.Background(), &permission.Principal{Role: permission.RoleAdmin}, &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
				"body":  {Type: contract.FieldRichText},
			}},
		},
	}); err != nil {
		t.Fatal(err)
	}
}

// auditRows reads audit records through a brand-new connection to dbPath —
// the restart-survival proof: rows written by one daemon boot must be
// visible to a fresh handle on the same SQLite file.
func auditRows(t *testing.T, dbPath, plugin string) []audit.Record {
	t.Helper()
	check, err := db.OpenSQLite(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer check.Close()
	rows, err := audit.NewLogger(check).ListByPlugin(context.Background(), plugin)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// loginAs logs in a non-default account (loginAdmin is pinned to the seeded
// admin) and returns its session+CSRF pair.
func loginAs(t *testing.T, h http.Handler, email string) daemonSession {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"email": email, "password": "correct horse battery"})
	req := httptest.NewRequest(http.MethodPost, "/api/v0/auth/login", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login %s = %d, body %s", email, rec.Code, rec.Body.String())
	}
	var sess daemonSession
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "glyphux_session":
			sess.session = c.Value
		case "glyphux_csrf":
			sess.csrf = c.Value
		}
	}
	return sess
}

// TestDaemonAuditsContentWriteOverHTTPAndSurvivesRestart: GIVEN a real
// daemon boot (buildFullHandler + real SQLite), WHEN an admin creates a
// content item over HTTP, THEN a content.created row lands in audit_records
// (stamped plugin "content", Detail with role+item_id+type) and is visible
// through a fresh connection to the same database file — the restart-AC
// proof.
func TestDaemonAuditsContentWriteOverHTTPAndSurvivesRestart(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	p := bootDaemonParts(t, cfg, dbPath, true)
	saveArticleType(t, p)
	sess := loginAdmin(t, p.h)

	rec := doJSON(t, p.h, http.MethodPost, "/api/v0/content/article", sess, map[string]any{"title": "Hello", "body": "World"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /content/article = %d, body %s", rec.Code, rec.Body.String())
	}
	id := decodeBody(t, rec)["id"].(string)

	rows := auditRows(t, dbPath, "content")
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1 (content.created)", len(rows))
	}
	r := rows[0]
	if r.Action != "content.created" || !r.Allowed || r.PluginName != "content" {
		t.Errorf("row = %+v, want action=content.created allowed plugin=content", r)
	}
	var detail map[string]string
	if err := json.Unmarshal([]byte(r.Detail), &detail); err != nil {
		t.Fatalf("parse detail %q: %v", r.Detail, err)
	}
	if detail["item_id"] != id || detail["type"] != "article" || detail["role"] != "admin" {
		t.Errorf("detail = %v, want item_id=%s type=article role=admin", detail, id)
	}
}

// TestDaemonAuditEndpointAuth: GIVEN a real daemon, WHEN an anonymous
// caller, an editor, and an admin GET /api/v0/audit, THEN the endpoint
// answers 401, 403 and 200 (with the audited rows) respectively.
func TestDaemonAuditEndpointAuth(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "glyphux.db")
	cfg := config.Default()
	cfg.DataDir = t.TempDir()
	p := bootDaemonParts(t, cfg, dbPath, true)
	saveArticleType(t, p)
	ctx := context.Background()
	if _, err := p.ids.CreateUser(ctx, "editor@example.com", "correct horse battery", "editor"); err != nil {
		t.Fatal(err)
	}

	// Anonymous: 401.
	rec := doJSON(t, p.h, http.MethodGet, "/api/v0/audit?plugin=content", daemonSession{}, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous = %d, want 401 (body %s)", rec.Code, rec.Body.String())
	}

	// Editor: 403.
	rec = doJSON(t, p.h, http.MethodGet, "/api/v0/audit?plugin=content", loginAs(t, p.h, "editor@example.com"), nil)
	if rec.Code != http.StatusForbidden {
		t.Errorf("editor = %d, want 403 (body %s)", rec.Code, rec.Body.String())
	}

	// Admin: 200 with the audited write's row.
	sess := loginAdmin(t, p.h)
	rec = doJSON(t, p.h, http.MethodPost, "/api/v0/content/article", sess, map[string]any{"title": "Hello", "body": "World"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /content/article = %d, body %s", rec.Code, rec.Body.String())
	}
	rec = doJSON(t, p.h, http.MethodGet, "/api/v0/audit?plugin=content", sess, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin audit = %d, want 200 (body %s)", rec.Code, rec.Body.String())
	}
	body := decodeBody(t, rec)
	records, ok := body["records"].([]any)
	if !ok || len(records) < 1 {
		t.Errorf("records = %v, want at least the content.created row", body["records"])
	}
}
