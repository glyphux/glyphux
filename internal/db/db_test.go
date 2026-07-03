package db

import (
	"context"
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	d, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestMigrateIsIdempotent(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	migrations := []Migration{
		{Version: 1, Name: "t1", SQL: `CREATE TABLE t1 (id INTEGER PRIMARY KEY)`},
		{Version: 2, Name: "t2", SQL: `CREATE TABLE t2 (id INTEGER PRIMARY KEY)`},
	}
	for range 3 {
		if err := d.Migrate(ctx, migrations); err != nil {
			t.Fatalf("Migrate: %v", err)
		}
	}
	var n int
	if err := d.QueryRow(ctx, `SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("schema_migrations rows = %d, want 2", n)
	}
}

func TestMigrateAppliesInOrder(t *testing.T) {
	d := openTestDB(t)
	// Version 2 depends on version 1's table; passing them out of order must
	// still apply 1 first.
	migrations := []Migration{
		{Version: 2, Name: "add column", SQL: `ALTER TABLE base ADD COLUMN extra TEXT`},
		{Version: 1, Name: "base", SQL: `CREATE TABLE base (id INTEGER PRIMARY KEY)`},
	}
	if err := d.Migrate(context.Background(), migrations); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
}

func TestMigrateRejectsDuplicateVersions(t *testing.T) {
	d := openTestDB(t)
	migrations := []Migration{
		{Version: 1, Name: "a", SQL: `CREATE TABLE a (id INTEGER)`},
		{Version: 1, Name: "b", SQL: `CREATE TABLE b (id INTEGER)`},
	}
	if err := d.Migrate(context.Background(), migrations); err == nil {
		t.Fatal("Migrate accepted duplicate versions")
	}
}

func TestMigrateFailedStepIsNotRecorded(t *testing.T) {
	d := openTestDB(t)
	ctx := context.Background()
	bad := []Migration{{Version: 1, Name: "bad", SQL: `THIS IS NOT SQL`}}
	if err := d.Migrate(ctx, bad); err == nil {
		t.Fatal("Migrate accepted invalid SQL")
	}
	good := []Migration{{Version: 1, Name: "good", SQL: `CREATE TABLE fine (id INTEGER)`}}
	if err := d.Migrate(ctx, good); err != nil {
		t.Fatalf("retry after failure: %v", err)
	}
}
