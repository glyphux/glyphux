// Package bootstrap owns the daemon's boot-time decision of which database
// to use and, when it isn't known yet, defers opening the real database
// until the first-run wizard resolves it (§6.2, §16 Phase 0). This is what
// lets a Postgres choice made in the wizard actually take effect without an
// operator restart.
package bootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Config is the durable record of a completed setup's database choice.
// Deliberately minimal: it is never allowed to carry a secret. For
// Postgres, DSNEnvVar names the environment variable the connection string
// must be supplied through on every subsequent boot — the DSN itself is
// never written to disk.
type Config struct {
	Driver    string `json:"driver"`              // "sqlite" | "postgres"
	DSNEnvVar string `json:"dsn_env_var,omitempty"` // set only when Driver == "postgres"
}

func configPath(dataDir string) string { return filepath.Join(dataDir, "glyphux.json") }

// Load reads the persisted bootstrap config from dataDir. found is false
// (with a nil error) when no setup has completed yet — the ordinary,
// expected state for a fresh install.
func Load(dataDir string) (cfg *Config, found bool, err error) {
	raw, err := os.ReadFile(configPath(dataDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", configPath(dataDir), err)
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", configPath(dataDir), err)
	}
	return &c, true, nil
}

// Save persists cfg atomically: written to a temp file, then renamed over
// the target, so a crash mid-write never leaves a corrupt or partial
// glyphux.json for the next boot to trip over.
func Save(dataDir string, cfg *Config) error {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}
	raw, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("encode bootstrap config: %w", err)
	}
	tmp := configPath(dataDir) + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, configPath(dataDir)); err != nil {
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}
