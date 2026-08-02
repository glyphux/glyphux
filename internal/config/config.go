// Package config loads the daemon configuration.
//
// Precedence: defaults < config file (glyphux.yaml / glyphux.json) < environment.
// Convention over configuration (Principle 10): every value has a sensible
// default so `glyphuxd` boots with no config at all.
//
// Secret values (currently just the Postgres DSN) are read through the
// single secret() function in secrets.go (slice 1.9) rather than a bare
// os.Getenv call, naming the one seam a future real secrets manager would
// need to change.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/glyphux/glyphux/pkg/sdk"
)

// Config is the fully-resolved daemon configuration.
type Config struct {
	// Addr is the listen address for the HTTP server, e.g. ":8080".
	Addr string `json:"addr"`

	// DataDir holds all persistent state (SQLite database, media, secrets).
	DataDir string `json:"data_dir"`

	// Database selects the storage adapter: "sqlite" (default) or "postgres".
	Database DatabaseConfig `json:"database"`

	// ShutdownTimeout bounds graceful shutdown.
	ShutdownTimeout time.Duration `json:"-"`

	// OpenBrowser controls whether first-run opens a local browser (Scenario 1).
	OpenBrowser bool `json:"open_browser"`

	// TrustProxyHeaders controls whether the wizard honors X-Forwarded-Proto
	// when deciding if a remote request arrived over HTTPS (§6.4). Only
	// enable this behind a reverse proxy known to set that header correctly
	// and strip any client-supplied copy of it — otherwise a client can
	// simply claim to be HTTPS.
	TrustProxyHeaders bool `json:"trust_proxy_headers"`

	// OAuth configures social login providers. Empty ClientID/ClientSecret
	// means the provider is not offered — OAuth login is entirely optional,
	// same as MFA (§ identity's Current Decisions doc).
	OAuth OAuthConfig `json:"oauth"`

	// PublicURL is this instance's externally-reachable base URL (e.g.
	// "https://cms.example.com"), used to build the OAuth redirect_uri
	// (must match what's registered with the provider). Defaults to
	// "http://" + Addr, which only works for local/loopback testing.
	PublicURL string `json:"public_url"`

	// AllowedOrigins opts the daemon into CORS for exactly these origins —
	// e.g. an external developer's frontend calling the API cross-origin
	// (PRD's own headless-CMS positioning). Empty/unset by default,
	// preserving the "no CORS ever" posture every response has today
	// (slice 1.9): no wildcard support, on purpose — every allowed origin
	// must be named explicitly.
	AllowedOrigins []string `json:"allowed_origins"`

	// RPCOutboundProxyURL, when set, is injected as HTTP_PROXY and
	// HTTPS_PROXY into every Tier-C plugin subprocess the RPC broker
	// launches — the operator egress choke point for out-of-process
	// plugins' own outbound connections, which the host cannot intercept
	// for them (Ticket T3 / gap 5). Empty (default) leaves the
	// subprocess's proxy environment inherited from the daemon's own
	// environment.
	RPCOutboundProxyURL string `json:"rpc_outbound_proxy_url"`

	// AI configures the AI authoring transport (POST /api/v0/ai/compose,
	// PRD §14.1 Surface 2): which provider adapter to use, the operator's
	// declared default model, an optional base URL for self-hosted
	// openai-compatible endpoints, and an optional calls/minute rate cap.
	// AI is opt-in: an empty Provider leaves the compose endpoint 404ing
	// and requires no API key. The API key itself is never part of this
	// struct's serialized form — see AIConfig.APIKey.
	AI AIConfig `json:"ai"`

	// Plugins configures Tier B/C plugin loading (Ticket T6 / gap 1): the
	// tier-b plugins directory (GLYPHUX_PLUGINS_DIR) plus the
	// plugins[]{name,tier,source} entries the loader materializes. Empty by
	// default — first-party-only (the T5 path). The config-file shape is
	// flat top-level "plugins_dir"/"plugins" keys, parsed in Load() (the
	// struct tag here is json:"-" so the plain field pass never collides
	// with the plugins array).
	Plugins PluginsConfig `json:"-"`

	// Marketplace configures the marketplace surface (Ticket T8 / gap 8):
	// the key-ID-aware package trust set (trusted_keys[] — the replacement
	// for the old scalar marketplace.public_key), the trust mode, and an
	// optional operator catalog file that EXTENDS the embedded sample
	// catalog (never replaces it).
	Marketplace MarketplaceConfig `json:"marketplace"`
}

// MarketplaceConfig is the operator-facing marketplace section. It is
// nested under "marketplace" in the config file (the natural home for a
// record-shaped section; the old scalar marketplace.public_key is gone).
//
// Trust semantics (locked T8 decisions): trust is key-ID-aware and
// ADDITIVE by default — operator keys join the embedded dev root, they do
// not replace it. Setting TrustMode to "custom-only" opts into full
// replacement: only the operator's declared keys are trusted. (Edge case,
// documented: json merge semantics keep the embedded seed when a
// custom-only file omits trusted_keys entirely — a custom-only operator
// should always list their keys explicitly.)
type MarketplaceConfig struct {
	// TrustedKeys is the trust set package signatures resolve against.
	TrustedKeys []TrustedKey `json:"trusted_keys"`
	// TrustMode is "" (additive — embedded dev root + operator keys) or
	// "custom-only" (full replacement — operator keys only).
	TrustMode string `json:"trust_mode"`
	// CatalogFile is an optional operator JSON file of catalog entries that
	// EXTENDS the embedded sample catalog at boot (never replaces it).
	CatalogFile string `json:"catalog_file"`
}

// TrustModeCustomOnly opts the trust set into full replacement: only the
// operator-declared trusted_keys are trusted — the embedded dev root is
// dropped.
const TrustModeCustomOnly = "custom-only"

// TrustedKey is one package-signing trust record: a named public key with
// its declared purpose(s), issuer, and lifecycle status. The status field
// (active|deprecated|revoked|expired) is descriptive today — T8 does not
// branch on it, but it is the documented hook the future era/prod-root
// split (T10) and key rotation hang off.
type TrustedKey struct {
	ID        string     `json:"id"`
	Algorithm string     `json:"algorithm"`
	PublicKey string     `json:"public_key"`
	Purpose   []string   `json:"purpose"`
	Issuer    string     `json:"issuer"`
	Status    string     `json:"status"`
	NotBefore time.Time  `json:"not_before"`
	NotAfter  *time.Time `json:"not_after"`
}

// DevRootKeyID is the id of the dev-era trust anchor — the embedded
// DEFAULT seed only in `-tags dev` builds (the T10 era/prod split); normal
// builds seed the prod root (ProdRootKeyID). Its public key is
// DevRootPublicKey below; the matching PRIVATE key exists only in the
// fixture-generation tooling (it is never embedded, and never in the
// daemon).
const DevRootKeyID = "glyphux-dev-2026-01"

// DevRootPublicKey is the pinned dev root public key (hex). Since the T10
// era/prod split it is the seed of dev-tagged builds only; a normal build
// does NOT trust it unless an operator explicitly lists it in
// trusted_keys[] (additive) or opts into custom-only.
const DevRootPublicKey = "a0919864e1e100024db888a7a4e4f9f85113fa2457eba83fc070bdb239b59b67"

// ProdRootKeyID is the id of the embedded production-era default trust
// anchor — the seed of every normal (non-dev-tagged) build since the T10
// era/prod split. Its public key is ProdRootPublicKey below; the matching
// PRIVATE key exists only in the fixture-generation tooling (it is never
// embedded, and never in the daemon) — the mirror of the dev-root pattern.
const ProdRootKeyID = "glyphux-packages-prod-2026-01"

// ProdRootPublicKey is the pinned prod-era root public key (hex) — the
// embedded default trust anchor of normal builds.
const ProdRootPublicKey = "35aca05ea0ce70a7fe71c5fe11ff8d52e812ef1d7b98b926d7ab52174b3c7736"

// embeddedDevRoot is the dev-root seed record — returned by Default() only
// in `-tags dev` builds (see the build-tagged seed selectors).
func embeddedDevRoot() TrustedKey {
	return TrustedKey{
		ID:        DevRootKeyID,
		Algorithm: "ed25519",
		PublicKey: DevRootPublicKey,
		Purpose:   []string{"package-signing"},
		Issuer:    "glyphux",
		Status:    "active",
	}
}

// embeddedProdRoot is the prod-era Default() seed record — the one key
// every normal host trusts unless the operator opts into custom-only.
func embeddedProdRoot() TrustedKey {
	return TrustedKey{
		ID:        ProdRootKeyID,
		Algorithm: "ed25519",
		PublicKey: ProdRootPublicKey,
		Purpose:   []string{"package-signing"},
		Issuer:    "glyphux",
		Status:    "active",
	}
}

// PluginsConfig is the operator-facing plugin section: a directory to read
// tier-b .wasm files from and the list of configured plugins.
type PluginsConfig struct {
	// Dir is the tier-b plugins directory (GLYPHUX_PLUGINS_DIR).
	Dir string `json:"plugins_dir"`
	// Plugins is the configured plugin list, each naming a tier and source.
	Plugins []PluginConfig `json:"plugins"`
}

// PluginConfig names one configured plugin. Tier "a" is first-party-only
// (registered in-process via internal/plugin's RegisterPlugin — never via
// config); config tiers are "b" (wasm) and "c" (rpc subprocess). Manifest
// is the declared trust surface the loader consents and filters against —
// the interim carrier until T8's package containers ship it; it is optional
// at parse time, and a plugin without one is refused at load
// (deny-by-default) while the daemon continues.
type PluginConfig struct {
	Name     string       `json:"name"`
	Tier     string       `json:"tier"`
	Source   string       `json:"source"`
	Manifest sdk.Manifest `json:"manifest"`
}

// AIConfig holds the operator's AI settings. APIKey is resolved at load
// time exclusively through the package's secret() seam (GLYPHUX_AI_API_KEY)
// and carries json:"-" so no serialized form of Config — config-file dumps,
// diagnostics, anything embedding Config — can ever contain it.
//
// Valid Provider values are the adapter set in capabilities/ai:
// claude | openai | gemini | openai-compatible (unknown providers are a
// daemon-side fail-fast, reported by cmd/glyphuxd at boot).
type AIConfig struct {
	Provider string `json:"provider"`
	Model    string `json:"model"`

	// BaseURL overrides the provider's default endpoint; required by the
	// claude, gemini and openai-compatible adapters, ignored by openai
	// (which always talks to https://api.openai.com).
	BaseURL string `json:"base_url"`

	// RateLimit caps every AI operation (generate/embed/classify) at this
	// many calls per minute. Zero (default) leaves the service's
	// DefaultLimits in place.
	RateLimit int `json:"rate_limit"`

	// APIKey is the provider credential, loaded only via secret(); it is
	// never serialized (json:"-") and never written to any config file.
	APIKey string `json:"-"`
}

// OAuthConfig holds one provider's registered app credentials. Only GitHub
// is wired into cmd/glyphuxd today (see internal/identity/oauth.go's doc
// comment for why); the shape here is provider-specific on purpose so
// adding a second provider is additive, not a rewrite of this struct.
type OAuthConfig struct {
	GitHubClientID     string `json:"github_client_id"`
	GitHubClientSecret string `json:"github_client_secret"`
}

// DatabaseConfig selects and configures the database adapter.
type DatabaseConfig struct {
	Driver string `json:"driver"` // "sqlite" | "postgres"
	DSN    string `json:"dsn"`    // for postgres; ignored for sqlite

	// Pool sizes and bounds the Postgres connection pool (§11.6); ignored for
	// sqlite, which always runs single-connection. Zero fields auto-size.
	MaxOpenConns    int           `json:"max_open_conns"`
	MaxIdleConns    int           `json:"max_idle_conns"`
	ConnMaxLifetime time.Duration `json:"-"`
}

// Default returns the zero-config defaults.
func Default() Config {
	return Config{
		Addr:            ":8080",
		DataDir:         "data",
		Database:        DatabaseConfig{Driver: "sqlite"},
		ShutdownTimeout: 10 * time.Second,
		OpenBrowser:     false,
		Marketplace: MarketplaceConfig{
			TrustedKeys: []TrustedKey{embeddedDefaultRoot()},
		},
	}
}

// Load resolves the configuration: defaults, then an optional JSON config
// file, then GLYPHUX_* environment variables.
func Load(path string) (Config, error) {
	cfg := Default()

	if path != "" {
		raw, err := os.ReadFile(path)
		if err != nil {
			return cfg, fmt.Errorf("read config %s: %w", path, err)
		}
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return cfg, fmt.Errorf("parse config %s: %w", path, err)
		}
		// The plugins section has a flat config-file shape — top-level
		// "plugins_dir" and "plugins" (array) keys (pinned by the T6 config
		// tests) — so it is parsed here rather than via a Config-level
		// UnmarshalJSON (which would clobber the Default() seed for every
		// field the document omits). The Plugins field itself carries
		// json:"-", keeping the plain pass above from colliding with the
		// plugins array.
		var flat struct {
			Dir     string         `json:"plugins_dir"`
			Plugins []PluginConfig `json:"plugins"`
		}
		if err := json.Unmarshal(raw, &flat); err != nil {
			return cfg, fmt.Errorf("parse config %s: plugins section: %w", path, err)
		}
		cfg.Plugins = PluginsConfig{Dir: flat.Dir, Plugins: flat.Plugins}

		// Marketplace trust (Ticket T8 / gap 8): trust is additive by
		// default — operator trusted_keys[] join the embedded dev root.
		// trust_mode="custom-only" opts into full replacement. The merge
		// runs AFTER the plain unmarshal because that pass replaces
		// cfg.Marketplace.TrustedKeys wholesale when the file declares the
		// key; the seed must then be re-appended (unless custom-only).
		if cfg.Marketplace.TrustMode != TrustModeCustomOnly {
			for _, def := range Default().Marketplace.TrustedKeys {
				present := false
				for _, k := range cfg.Marketplace.TrustedKeys {
					if k.ID == def.ID {
						present = true
						break
					}
				}
				if !present {
					cfg.Marketplace.TrustedKeys = append(cfg.Marketplace.TrustedKeys, def)
				}
			}
		}
	}

	if v := os.Getenv("GLYPHUX_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := os.Getenv("GLYPHUX_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	if v := os.Getenv("GLYPHUX_DB_DRIVER"); v != "" {
		cfg.Database.Driver = v
	}
	if v, ok := secret("GLYPHUX_DB_DSN"); ok && v != "" {
		cfg.Database.DSN = v
	}
	if v := os.Getenv("GLYPHUX_DB_MAX_OPEN_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_DB_MAX_OPEN_CONNS: %w", err)
		}
		cfg.Database.MaxOpenConns = n
	}
	if v := os.Getenv("GLYPHUX_DB_MAX_IDLE_CONNS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_DB_MAX_IDLE_CONNS: %w", err)
		}
		cfg.Database.MaxIdleConns = n
	}
	if v := os.Getenv("GLYPHUX_DB_CONN_MAX_LIFETIME"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_DB_CONN_MAX_LIFETIME: %w", err)
		}
		cfg.Database.ConnMaxLifetime = d
	}
	if v := os.Getenv("GLYPHUX_OPEN_BROWSER"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_OPEN_BROWSER: %w", err)
		}
		cfg.OpenBrowser = b
	}
	if v := os.Getenv("GLYPHUX_TRUST_PROXY_HEADERS"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_TRUST_PROXY_HEADERS: %w", err)
		}
		cfg.TrustProxyHeaders = b
	}
	if v := os.Getenv("GLYPHUX_PUBLIC_URL"); v != "" {
		cfg.PublicURL = v
	}
	if v := os.Getenv("GLYPHUX_OAUTH_GITHUB_CLIENT_ID"); v != "" {
		cfg.OAuth.GitHubClientID = v
	}
	if v := os.Getenv("GLYPHUX_OAUTH_GITHUB_CLIENT_SECRET"); v != "" {
		cfg.OAuth.GitHubClientSecret = v
	}
	if v := os.Getenv("GLYPHUX_ALLOWED_ORIGINS"); v != "" {
		var origins []string
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
		cfg.AllowedOrigins = origins
	}
	if v := os.Getenv("GLYPHUX_RPC_OUTBOUND_PROXY_URL"); v != "" {
		cfg.RPCOutboundProxyURL = v
	}
	// AI (Ticket T5 / gap 3). The API key is the one secret field and goes
	// through the package's single secrets seam — never a bare Getenv — so
	// the redaction guarantee (json:"-") and the "this is where secrets
	// come from" documentation hold in the same place.
	if v := os.Getenv("GLYPHUX_AI_PROVIDER"); v != "" {
		cfg.AI.Provider = v
	}
	if v := os.Getenv("GLYPHUX_AI_MODEL"); v != "" {
		cfg.AI.Model = v
	}
	if v := os.Getenv("GLYPHUX_AI_BASE_URL"); v != "" {
		cfg.AI.BaseURL = v
	}
	if v := os.Getenv("GLYPHUX_AI_RATE_LIMIT"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return cfg, fmt.Errorf("GLYPHUX_AI_RATE_LIMIT: %w", err)
		}
		cfg.AI.RateLimit = n
	}
	if v, ok := secret("GLYPHUX_AI_API_KEY"); ok && v != "" {
		cfg.AI.APIKey = v
	}
	// Plugins (Ticket T6 / gap 1): the tier-b plugins directory. The
	// plugins[] list itself is config-file-only (no env carrier for JSON).
	if v := os.Getenv("GLYPHUX_PLUGINS_DIR"); v != "" {
		cfg.Plugins.Dir = v
	}
	// Marketplace (Ticket T8 / gap 8): the scalar knobs have env carriers;
	// the trusted_keys[] records are JSON-shaped and therefore
	// config-file-only (same reasoning as the plugins[] list).
	if v := os.Getenv("GLYPHUX_MARKETPLACE_TRUST_MODE"); v != "" {
		cfg.Marketplace.TrustMode = v
	}
	if v := os.Getenv("GLYPHUX_MARKETPLACE_CATALOG_FILE"); v != "" {
		cfg.Marketplace.CatalogFile = v
	}

	if err := cfg.validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) validate() error {
	switch strings.ToLower(c.Database.Driver) {
	case "sqlite":
		c.Database.Driver = "sqlite"
	case "postgres":
		c.Database.Driver = "postgres"
		if c.Database.DSN == "" {
			return fmt.Errorf("database.driver=postgres requires a DSN")
		}
	default:
		return fmt.Errorf("unknown database driver %q (want sqlite or postgres)", c.Database.Driver)
	}
	if c.Addr == "" {
		return fmt.Errorf("addr must not be empty")
	}
	if c.AI.RateLimit < 0 {
		return fmt.Errorf("ai.rate_limit must not be negative")
	}
	switch c.Marketplace.TrustMode {
	case "", TrustModeCustomOnly:
	default:
		return fmt.Errorf("unknown marketplace.trust_mode %q (want %q for full replacement, or unset for additive)", c.Marketplace.TrustMode, TrustModeCustomOnly)
	}
	// T10a.1: custom-only means FULL replacement — only the operator's
	// declared keys are trusted. When it yields zero operator-declared
	// keys (file omits trusted_keys entirely, or declares an empty
	// array), boot must FAIL instead of silently keeping a default seed:
	// the embedded seed is not an operator key, so it cannot satisfy the
	// replacement contract — an operator who opts out of the embedded
	// root must say which keys replace it.
	if c.Marketplace.TrustMode == TrustModeCustomOnly && !operatorDeclared(c.Marketplace.TrustedKeys) {
		return fmt.Errorf("marketplace.trust_mode=custom-only requires at least one operator-declared trusted_keys entry (got zero)")
	}
	return nil
}

// operatorDeclared reports whether the trust set contains at least one key
// that is not an embedded root — i.e. one the operator actually declared
// (in custom-only mode the embedded dev/prod roots do not count; the
// operator's own keys do).

// operatorDeclared reports whether the trust set contains at least one key
// that is not an embedded root — i.e. one the operator actually declared
// (in custom-only mode the embedded dev/prod roots do not count; the
// operator's own keys do).
func operatorDeclared(keys []TrustedKey) bool {
	for _, k := range keys {
		if k.ID != DevRootKeyID && k.ID != ProdRootKeyID {
			return true
		}
	}
	return false
}

// SQLitePath is the on-disk location of the embedded database.
func (c Config) SQLitePath() string {
	return filepath.Join(c.DataDir, "glyphux.db")
}
