// CORE-06 REVIEW (behavior-first): config precedence.
//
// Precedence contract (internal/config/config.go): defaults < config file
// < GLYPHUX_* environment variables.
//   - Given nothing set, When config loads, Then defaults are applied.
//   - Given a config file value AND a different GLYPHUX_<KEY> env value,
//     When config loads, Then env wins.
package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/glyphux/glyphux/internal/config"
)

// reviewEnvKeys are every GLYPHUX_* key Load() reads.
var reviewEnvKeys = []string{
	"GLYPHUX_ADDR", "GLYPHUX_DATA_DIR", "GLYPHUX_DB_DRIVER", "GLYPHUX_DB_DSN",
	"GLYPHUX_DB_MAX_OPEN_CONNS", "GLYPHUX_DB_MAX_IDLE_CONNS", "GLYPHUX_DB_CONN_MAX_LIFETIME",
	"GLYPHUX_OPEN_BROWSER", "GLYPHUX_TRUST_PROXY_HEADERS", "GLYPHUX_PUBLIC_URL",
	"GLYPHUX_OAUTH_GITHUB_CLIENT_ID", "GLYPHUX_OAUTH_GITHUB_CLIENT_SECRET",
	"GLYPHUX_ALLOWED_ORIGINS", "GLYPHUX_RPC_OUTBOUND_PROXY_URL",
	"GLYPHUX_AI_PROVIDER", "GLYPHUX_AI_MODEL", "GLYPHUX_AI_BASE_URL",
	"GLYPHUX_AI_RATE_LIMIT", "GLYPHUX_AI_API_KEY", "GLYPHUX_PLUGINS_DIR",
	"GLYPHUX_MARKETPLACE_TRUST_MODE", "GLYPHUX_MARKETPLACE_CATALOG_FILE",
}

// reviewClearEnv neutralizes every env key the loader reads.
func reviewClearEnv(t *testing.T) {
	t.Helper()
	for _, k := range reviewEnvKeys {
		t.Setenv(k, "") // empty means "unset" to Load()
	}
}

// TestReviewDefaultsApplied — nothing set -> defaults.
func TestReviewDefaultsApplied(t *testing.T) {
	t.Run("Given no config file and no GLYPHUX_* env, When config.Load runs, Then defaults are applied", func(t *testing.T) {
		reviewClearEnv(t)
		t.Logf("Given no config file (path \"\") and all GLYPHUX_* env vars empty")
		t.Logf("When  config.Load(\"\") runs")
		cfg, err := config.Load("")
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		want := config.Default()
		if cfg.Addr != want.Addr {
			t.Errorf("FAIL: Addr = %q, want default %q", cfg.Addr, want.Addr)
		} else {
			t.Logf("PASS: Addr default %q", cfg.Addr)
		}
		if cfg.DataDir != want.DataDir {
			t.Errorf("FAIL: DataDir = %q, want default %q", cfg.DataDir, want.DataDir)
		} else {
			t.Logf("PASS: DataDir default %q", cfg.DataDir)
		}
		if cfg.Database.Driver != want.Database.Driver {
			t.Errorf("FAIL: Database.Driver = %q, want default %q", cfg.Database.Driver, want.Database.Driver)
		} else {
			t.Logf("PASS: Database.Driver default %q", cfg.Database.Driver)
		}
		if cfg.ShutdownTimeout != want.ShutdownTimeout {
			t.Errorf("FAIL: ShutdownTimeout = %v, want default %v", cfg.ShutdownTimeout, want.ShutdownTimeout)
		} else {
			t.Logf("PASS: ShutdownTimeout default %v", cfg.ShutdownTimeout)
		}
		if len(cfg.Marketplace.TrustedKeys) == 0 {
			t.Errorf("FAIL: Marketplace.TrustedKeys empty — defaults must seed a trust root")
		} else {
			t.Logf("PASS: Marketplace.TrustedKeys seeded with %d key(s)", len(cfg.Marketplace.TrustedKeys))
		}
	})
}

// TestReviewEnvWinsOverFile — env beats config file beats defaults.
func TestReviewEnvWinsOverFile(t *testing.T) {
	t.Run("Given a config file with addr=:9999/data_dir=from-file AND GLYPHUX_ADDR=:1234/GLYPHUX_DATA_DIR=from-env, When config.Load runs, Then env wins over file", func(t *testing.T) {
		reviewClearEnv(t)
		dir := t.TempDir()
		path := filepath.Join(dir, "glyphux.json")
		if err := os.WriteFile(path, []byte(`{"addr":":9999","data_dir":"from-file","database":{"driver":"sqlite"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("GLYPHUX_ADDR", ":1234")
		t.Setenv("GLYPHUX_DATA_DIR", "from-env")
		t.Logf("Given config file %s declares addr=:9999, data_dir=from-file; env declares GLYPHUX_ADDR=:1234, GLYPHUX_DATA_DIR=from-env", path)
		t.Logf("When  config.Load(%q) runs", path)
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Addr != ":1234" {
			t.Errorf("FAIL: Addr = %q, want :1234 (env must win over file :9999)", cfg.Addr)
		} else {
			t.Logf("PASS: Addr = :1234 (env GLYPHUX_ADDR beats file :9999)")
		}
		if cfg.DataDir != "from-env" {
			t.Errorf("FAIL: DataDir = %q, want from-env (env must win over file from-file)", cfg.DataDir)
		} else {
			t.Logf("PASS: DataDir = from-env (env GLYPHUX_DATA_DIR beats file from-file)")
		}
	})
}

// TestReviewFileBeatsDefault — file value applied when env is unset.
func TestReviewFileBeatsDefault(t *testing.T) {
	t.Run("Given a config file with addr=:9999 and no GLYPHUX_ADDR, When config.Load runs, Then the file value beats the default", func(t *testing.T) {
		reviewClearEnv(t)
		dir := t.TempDir()
		path := filepath.Join(dir, "glyphux.json")
		if err := os.WriteFile(path, []byte(`{"addr":":9999","database":{"driver":"sqlite"}}`), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Logf("Given config file %s declares addr=:9999; no GLYPHUX_ADDR env", path)
		t.Logf("When  config.Load(%q) runs", path)
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if cfg.Addr != ":9999" {
			t.Errorf("FAIL: Addr = %q, want :9999 (file must beat default :8080)", cfg.Addr)
		} else {
			t.Logf("PASS: Addr = :9999 (file beats default :8080)")
		}
		_ = time.Second // keep the time import meaningful for future duration assertions
	})
}
