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

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/config"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/internal/pluginstore"
	"github.com/glyphux/glyphux/internal/setup"
	"github.com/glyphux/glyphux/pkg/contract"
)

// bootDaemon runs the real buildFullHandler the daemon serves from: a real
// SQLite database migrated with the same migration list main.go appends,
// a seeded composition (setup complete), an admin account, and the real
// setup wizard. This is the daemon-level httptest the T5 acceptance
// criteria call for — no mocks anywhere except the fake AI provider each
// AI test stands up itself.
func bootDaemon(t *testing.T, cfg config.Config) http.Handler {
	t.Helper()
	ctx := context.Background()

	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
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
	if err := d.Migrate(ctx, migs); err != nil {
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
	if err := ids.CreateAdmin(ctx, "admin@example.com", "correct horse battery"); err != nil {
		t.Fatal(err)
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
	return h
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
	h := bootDaemon(t, cfg)

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
	h := bootDaemon(t, cfg)
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
	h := bootDaemon(t, cfg)
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
