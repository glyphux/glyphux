package bootstrap_test

// The bootstrap config file (<data_dir>/glyphux.json) records which database
// driver a completed setup chose, so a restart doesn't need the wizard
// again. It must never carry a secret — for Postgres it records only the
// name of the environment variable the DSN lives in, never the DSN itself.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/internal/bootstrap"
)

func TestLoadReportsNotFoundWhenNoConfigWritten(t *testing.T) {
	_, found, err := bootstrap.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("Load reported found on an empty data dir")
	}
}

func TestSaveThenLoadRoundTrips(t *testing.T) {
	dir := t.TempDir()
	want := &bootstrap.Config{Driver: "postgres", DSNEnvVar: "GLYPHUX_DB_DSN"}
	if err := bootstrap.Save(dir, want); err != nil {
		t.Fatal(err)
	}
	got, found, err := bootstrap.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("Load reported not found after Save")
	}
	if *got != *want {
		t.Errorf("Load() = %+v, want %+v", got, want)
	}
}

func TestSaveNeverWritesADSNValue(t *testing.T) {
	dir := t.TempDir()
	cfg := &bootstrap.Config{Driver: "postgres", DSNEnvVar: "GLYPHUX_DB_DSN"}
	if err := bootstrap.Save(dir, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "glyphux.json"))
	if err != nil {
		t.Fatal(err)
	}
	// The Config type has no field for a raw connection string at all, but
	// this guards the on-disk contract directly: nothing that looks like a
	// DSN (scheme://user:pass@host) ever lands in the persisted file.
	if strings.Contains(string(raw), "://") {
		t.Errorf("persisted bootstrap config looks like it contains a connection string: %s", raw)
	}
}

func TestSaveIsAtomicAgainstAPriorPartialWrite(t *testing.T) {
	dir := t.TempDir()
	// Simulate a crash mid-write on a previous attempt: a stray .tmp file.
	if err := os.WriteFile(filepath.Join(dir, "glyphux.json.tmp"), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap.Save(dir, &bootstrap.Config{Driver: "sqlite"}); err != nil {
		t.Fatal(err)
	}
	got, found, err := bootstrap.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !found || got.Driver != "sqlite" {
		t.Fatalf("Load() = %+v, found=%v, want driver=sqlite", got, found)
	}
}
