package config_test

import (
	"testing"

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
