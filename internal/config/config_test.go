package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/glyphux/glyphux/internal/config"
)

// TestAllowedOriginsDefaultsEmpty proves CORS is opt-in (slice 1.9): with no
// GLYPHUX_ALLOWED_ORIGINS set, AllowedOrigins is empty, preserving the
// daemon's "no CORS ever" default posture.
func TestAllowedOriginsDefaultsEmpty(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AllowedOrigins) != 0 {
		t.Errorf("AllowedOrigins = %v, want empty by default", cfg.AllowedOrigins)
	}
}

// TestDatabaseDSNReadAsSecret proves GLYPHUX_DB_DSN — a real secret, a
// Postgres DSN with an embedded password — loads correctly through the
// package's single secret() seam (internal/config/secrets.go), not a bare
// os.Getenv call.
func TestDatabaseDSNReadAsSecret(t *testing.T) {
	t.Setenv("GLYPHUX_DB_DRIVER", "postgres")
	t.Setenv("GLYPHUX_DB_DSN", "postgres://user:hunter2@localhost/db")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Database.DSN != "postgres://user:hunter2@localhost/db" {
		t.Errorf("Database.DSN = %q", cfg.Database.DSN)
	}
}

// TestAllowedOriginsFromEnv proves GLYPHUX_ALLOWED_ORIGINS parses a
// comma-separated origin list, trimming whitespace and dropping empties.
func TestAllowedOriginsFromEnv(t *testing.T) {
	t.Setenv("GLYPHUX_ALLOWED_ORIGINS", "https://a.example, https://b.example ,")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"https://a.example", "https://b.example"}
	if len(cfg.AllowedOrigins) != len(want) {
		t.Fatalf("AllowedOrigins = %v, want %v", cfg.AllowedOrigins, want)
	}
	for i, o := range want {
		if cfg.AllowedOrigins[i] != o {
			t.Errorf("AllowedOrigins[%d] = %q, want %q", i, cfg.AllowedOrigins[i], o)
		}
	}
}

// TestRPCOutboundProxyURLDefaultsEmpty proves the Tier-C egress proxy is
// opt-in (Ticket T3): with no GLYPHUX_RPC_OUTBOUND_PROXY_URL set,
// RPCOutboundProxyURL is empty and the broker leaves subprocess proxy env
// untouched.
func TestRPCOutboundProxyURLDefaultsEmpty(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPCOutboundProxyURL != "" {
		t.Errorf("RPCOutboundProxyURL = %q, want empty by default", cfg.RPCOutboundProxyURL)
	}
}

// TestRPCOutboundProxyURLFromEnv proves GLYPHUX_RPC_OUTBOUND_PROXY_URL
// loads into Config.RPCOutboundProxyURL — the value the RPC broker injects
// as HTTP_PROXY/HTTPS_PROXY into every plugin subprocess it launches.
func TestRPCOutboundProxyURLFromEnv(t *testing.T) {
	t.Setenv("GLYPHUX_RPC_OUTBOUND_PROXY_URL", "http://proxy.internal:3128")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RPCOutboundProxyURL != "http://proxy.internal:3128" {
		t.Errorf("RPCOutboundProxyURL = %q, want %q", cfg.RPCOutboundProxyURL, "http://proxy.internal:3128")
	}
}

// --- AI configuration (Ticket T5 / gap 3) ---

// TestAIConfigDefaultsEmpty proves AI is opt-in: with no GLYPHUX_AI_* env,
// every AI config field is empty/zero, so the daemon leaves the compose
// endpoint 404ing.
func TestAIConfigDefaultsEmpty(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != "" || cfg.AI.Model != "" || cfg.AI.BaseURL != "" || cfg.AI.RateLimit != 0 || cfg.AI.APIKey != "" {
		t.Errorf("AI = %+v, want all fields empty by default", cfg.AI)
	}
}

// TestAIConfigFromEnv proves GLYPHUX_AI_PROVIDER/_MODEL/_BASE_URL/
// _RATE_LIMIT parse into Config.AI, and that the API key is resolved ONLY
// through the package's secrets seam (GLYPHUX_AI_API_KEY via secret(), the
// same single path GLYPHUX_DB_DSN uses) — never as a plain config value.
func TestAIConfigFromEnv(t *testing.T) {
	t.Setenv("GLYPHUX_AI_PROVIDER", "openai-compatible")
	t.Setenv("GLYPHUX_AI_MODEL", "llama3.2")
	t.Setenv("GLYPHUX_AI_BASE_URL", "http://localhost:11434")
	t.Setenv("GLYPHUX_AI_RATE_LIMIT", "42")
	t.Setenv("GLYPHUX_AI_API_KEY", "sk-test-secret")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.Provider != "openai-compatible" {
		t.Errorf("AI.Provider = %q", cfg.AI.Provider)
	}
	if cfg.AI.Model != "llama3.2" {
		t.Errorf("AI.Model = %q", cfg.AI.Model)
	}
	if cfg.AI.BaseURL != "http://localhost:11434" {
		t.Errorf("AI.BaseURL = %q", cfg.AI.BaseURL)
	}
	if cfg.AI.RateLimit != 42 {
		t.Errorf("AI.RateLimit = %d, want 42", cfg.AI.RateLimit)
	}
	if cfg.AI.APIKey != "sk-test-secret" {
		t.Errorf("AI.APIKey = %q (must resolve through the secrets seam)", cfg.AI.APIKey)
	}
}

// TestAIConfigAPIKeyNeverSerialized proves the api key cannot leak through
// plain config serialization: the field carries json:"-", so marshaling a
// fully-populated Config (or embedding it in any JSON document) never
// contains the key — the redaction guarantee Ticket T5's acceptance
// criteria require.
func TestAIConfigAPIKeyNeverSerialized(t *testing.T) {
	t.Setenv("GLYPHUX_AI_PROVIDER", "openai")
	t.Setenv("GLYPHUX_AI_API_KEY", "sk-hunter2-never-serialize")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AI.APIKey != "sk-hunter2-never-serialize" {
		t.Fatalf("precondition: APIKey must load, got %q", cfg.AI.APIKey)
	}

	encoded, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(encoded); strings.Contains(got, "hunter2") || strings.Contains(got, "sk-") {
		t.Fatalf("serialized config leaks the api key: %s", got)
	}
}

// --- Ticket T6 (gap 1): plugins section ---

// TestPluginsConfigDefaultsEmpty proves the plugins section is opt-in: with
// no plugins_dir and no plugins entries configured, Plugins is empty — the
// daemon's default posture is first-party-only (the T5 path).
func TestPluginsConfigDefaultsEmpty(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "" {
		t.Errorf("Plugins.Dir = %q, want empty by default", cfg.Plugins.Dir)
	}
	if len(cfg.Plugins.Plugins) != 0 {
		t.Errorf("Plugins.Plugins = %v, want empty by default", cfg.Plugins.Plugins)
	}
}

// TestPluginsDirFromEnv proves GLYPHUX_PLUGINS_DIR is the environment
// surface for the tier-B plugins directory — the same precedence the rest
// of Load applies (defaults, then JSON file, then GLYPHUX_* env).
func TestPluginsDirFromEnv(t *testing.T) {
	t.Setenv("GLYPHUX_PLUGINS_DIR", "/srv/glyphux/plugins")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "/srv/glyphux/plugins" {
		t.Errorf("Plugins.Dir = %q, want /srv/glyphux/plugins", cfg.Plugins.Dir)
	}
}

// TestPluginsSectionFromJSONFile proves the config-file surface for the
// plugins section: plugins_dir plus a plugins[]{name,tier,source} list (the
// Ticket T6 wire shape) parse from a JSON config file.
func TestPluginsSectionFromJSONFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glyphux.json")
	if err := os.WriteFile(p, []byte(`{
		"plugins_dir": "/srv/glyphux/plugins",
		"plugins": [
			{"name": "kv", "tier": "b", "source": "kv_guest.wasm"},
			{"name": "rpc-tool", "tier": "c", "source": "/usr/local/bin/rpc-tool"}
		]
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Plugins.Dir != "/srv/glyphux/plugins" {
		t.Errorf("Plugins.Dir = %q", cfg.Plugins.Dir)
	}
	if len(cfg.Plugins.Plugins) != 2 {
		t.Fatalf("Plugins.Plugins = %v, want 2 entries", cfg.Plugins.Plugins)
	}
	first := cfg.Plugins.Plugins[0]
	if first.Name != "kv" || first.Tier != "b" || first.Source != "kv_guest.wasm" {
		t.Errorf("entry[0] = %+v, want {kv b kv_guest.wasm}", first)
	}
	second := cfg.Plugins.Plugins[1]
	if second.Name != "rpc-tool" || second.Tier != "c" || second.Source != "/usr/local/bin/rpc-tool" {
		t.Errorf("entry[1] = %+v, want {rpc-tool c /usr/local/bin/rpc-tool}", second)
	}
}

// ---- Ticket T8 (gap 8): key-ID-aware marketplace trust config ----

// devRootPublicKey is the pinned embedded default trust anchor — the dev
// root the T8 package fixtures are signed with. The matching private key is
// held by the fixture-generation tooling only (never embedded, never in the
// daemon); the public half is the one thing every verifying host embeds.
const devRootPublicKey = "a0919864e1e100024db888a7a4e4f9f85113fa2457eba83fc070bdb239b59b67"

func findTrustedKey(t *testing.T, cfg config.Config, id string) *config.TrustedKey {
	t.Helper()
	for i := range cfg.Marketplace.TrustedKeys {
		if cfg.Marketplace.TrustedKeys[i].ID == id {
			return &cfg.Marketplace.TrustedKeys[i]
		}
	}
	t.Fatalf("trusted key %q not in %+v", id, cfg.Marketplace.TrustedKeys)
	return nil
}

// TestMarketplaceTrustDefaultsEmbedDevRoot pins the locked trust decision:
// the embedded default is the dev public key — one active package-signing
// record — with additive trust_mode by default (no "custom-only").
func TestMarketplaceTrustDefaultsEmbedDevRoot(t *testing.T) {
	cfg := config.Default()
	if cfg.Marketplace.TrustMode != "" {
		t.Errorf("TrustMode = %q, want default additive (\"\")", cfg.Marketplace.TrustMode)
	}
	if len(cfg.Marketplace.TrustedKeys) != 1 {
		t.Fatalf("TrustedKeys = %d, want exactly the embedded dev root", len(cfg.Marketplace.TrustedKeys))
	}
	k := cfg.Marketplace.TrustedKeys[0]
	if k.ID != "glyphux-dev-2026-01" || k.Algorithm != "ed25519" || k.Issuer != "glyphux" || k.Status != "active" {
		t.Errorf("dev root = %+v, want id=glyphux-dev-2026-01 algorithm=ed25519 issuer=glyphux status=active", k)
	}
	if k.PublicKey != devRootPublicKey {
		t.Errorf("dev root public key = %q, want the pinned dev key", k.PublicKey)
	}
	if len(k.Purpose) != 1 || k.Purpose[0] != "package-signing" {
		t.Errorf("purpose = %v, want [package-signing]", k.Purpose)
	}
}

// TestMarketplaceTrustedKeysParseFromJSON: GIVEN a config file with a
// structured trusted_keys record, WHEN Load runs, THEN every field lands
// (algorithm, public_key, purpose, issuer, status, validity window) and the
// catalog_file override parses — the operator key is ADDITIVE next to the
// embedded dev root (owner decision: operator keys never replace official
// roots unless trust_mode=custom-only).
func TestMarketplaceTrustedKeysParseFromJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glyphux.json")
	if err := os.WriteFile(p, []byte(`{
		"marketplace": {
			"catalog_file": "/srv/glyphux/marketplace-catalog.json",
			"trusted_keys": [
				{
					"id": "acme-prod-2026",
					"algorithm": "ed25519",
					"public_key": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
					"purpose": ["package-signing", "entitlement-signing"],
					"issuer": "acme",
					"status": "active",
					"not_before": "2026-01-01T00:00:00Z",
					"not_after": "2027-01-01T00:00:00Z"
				}
			]
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Marketplace.CatalogFile != "/srv/glyphux/marketplace-catalog.json" {
		t.Errorf("CatalogFile = %q", cfg.Marketplace.CatalogFile)
	}
	// Additive: the operator key joins the embedded dev root, it does not
	// replace it.
	if len(cfg.Marketplace.TrustedKeys) != 2 {
		t.Fatalf("TrustedKeys = %d, want dev root + operator key (additive)", len(cfg.Marketplace.TrustedKeys))
	}
	if findTrustedKey(t, cfg, "glyphux-dev-2026-01") == nil {
		t.Error("embedded dev root missing after operator key added")
	}
	k := findTrustedKey(t, cfg, "acme-prod-2026")
	if k.Algorithm != "ed25519" || k.Status != "active" || k.Issuer != "acme" {
		t.Errorf("operator key = %+v", k)
	}
	if len(k.Purpose) != 2 || k.Purpose[0] != "package-signing" || k.Purpose[1] != "entitlement-signing" {
		t.Errorf("operator purposes = %v", k.Purpose)
	}
	if k.NotBefore.IsZero() || !k.NotBefore.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("not_before = %v, want 2026-01-01T00:00:00Z", k.NotBefore)
	}
	if k.NotAfter == nil || !k.NotAfter.Equal(time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("not_after = %v, want 2027-01-01T00:00:00Z", k.NotAfter)
	}
}

// TestMarketplaceTrustModeCustomOnlyReplaces pins the escape hatch: an
// operator who sets trust_mode=custom-only gets a full replacement — the
// embedded dev root is dropped and only the operator keys remain.
func TestMarketplaceTrustModeCustomOnlyReplaces(t *testing.T) {
	p := filepath.Join(t.TempDir(), "glyphux.json")
	if err := os.WriteFile(p, []byte(`{
		"marketplace": {
			"trust_mode": "custom-only",
			"trusted_keys": [
				{"id": "acme-prod-2026", "algorithm": "ed25519",
				 "public_key": "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789",
				 "purpose": ["package-signing"], "issuer": "acme", "status": "active"}
			]
		}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Marketplace.TrustMode != "custom-only" {
		t.Errorf("TrustMode = %q, want custom-only", cfg.Marketplace.TrustMode)
	}
	if len(cfg.Marketplace.TrustedKeys) != 1 {
		t.Fatalf("TrustedKeys = %d, want only the operator key (custom-only replaces)", len(cfg.Marketplace.TrustedKeys))
	}
	if cfg.Marketplace.TrustedKeys[0].ID != "acme-prod-2026" {
		t.Errorf("only key = %+v, want acme-prod-2026", cfg.Marketplace.TrustedKeys[0])
	}
}
