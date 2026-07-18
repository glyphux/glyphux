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

	// AllowedOrigins opts the daemon into CORS for exactly these origins —
	// e.g. an external developer's frontend calling the API cross-origin
	// (PRD's own headless-CMS positioning). Empty/unset by default,
	// preserving the "no CORS ever" posture every response has today
	// (slice 1.9): no wildcard support, on purpose — every allowed origin
	// must be named explicitly.
	AllowedOrigins []string `json:"allowed_origins"`
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
	if v := os.Getenv("GLYPHUX_ALLOWED_ORIGINS"); v != "" {
		var origins []string
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				origins = append(origins, o)
			}
		}
		cfg.AllowedOrigins = origins
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
	return nil
}

// SQLitePath is the on-disk location of the embedded database.
func (c Config) SQLitePath() string {
	return filepath.Join(c.DataDir, "glyphux.db")
}
