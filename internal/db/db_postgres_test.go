package db

import (
	"context"
	"os"
	"testing"
)

// testPostgresDSN returns the DSN for a real Postgres instance to test
// against, or skips the test. Set GLYPHUX_TEST_POSTGRES_DSN to run these —
// they are integration tests against a real server, not mocks, because the
// placeholder rewriting and dialect-specific migration SQL are exactly the
// kind of thing that "looks right" against SQLite alone and breaks silently
// against a real Postgres wire protocol.
func testPostgresDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("GLYPHUX_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("GLYPHUX_TEST_POSTGRES_DSN not set; skipping Postgres integration test")
	}
	return dsn
}

func openTestPostgres(t *testing.T) *DB {
	t.Helper()
	d, err := OpenPostgres(testPostgresDSN(t), PoolConfig{})
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	t.Cleanup(func() { d.Close() })
	// Each test gets a clean slate: drop anything a previous run left behind.
	t.Cleanup(func() {
		_, _ = d.Exec(context.Background(), `DROP TABLE IF EXISTS schema_migrations, t1, t2, base, a, b, fine, users, composition CASCADE`)
	})
	_, err = d.Exec(context.Background(), `DROP TABLE IF EXISTS schema_migrations, t1, t2, base, a, b, fine, users, composition CASCADE`)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestPostgresPlaceholdersAreRewritten(t *testing.T) {
	d := openTestPostgres(t)
	ctx := context.Background()
	if _, err := d.Exec(ctx, `CREATE TABLE t1 (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO t1 (id, name) VALUES (?, ?)`, 1, "ada"); err != nil {
		t.Fatalf("insert with ? placeholders: %v", err)
	}
	var name string
	if err := d.QueryRow(ctx, `SELECT name FROM t1 WHERE id = ?`, 1).Scan(&name); err != nil {
		t.Fatalf("query with ? placeholder: %v", err)
	}
	if name != "ada" {
		t.Errorf("name = %q, want ada", name)
	}
}

func TestPostgresMigrateUsesDialectVariant(t *testing.T) {
	d := openTestPostgres(t)
	ctx := context.Background()
	migrations := []Migration{
		{
			Version:     1,
			Name:        "dialect table",
			SQL:         `CREATE TABLE base (id INTEGER PRIMARY KEY AUTOINCREMENT, name TEXT)`, // invalid on Postgres
			PostgresSQL: `CREATE TABLE base (id INTEGER GENERATED ALWAYS AS IDENTITY PRIMARY KEY, name TEXT)`,
		},
	}
	if err := d.Migrate(ctx, migrations); err != nil {
		t.Fatalf("Migrate with PostgresSQL variant: %v", err)
	}
	if _, err := d.Exec(ctx, `INSERT INTO base (name) VALUES (?)`, "x"); err != nil {
		t.Fatalf("insert into dialect-created table: %v", err)
	}
}

func TestPostgresMigrateIsIdempotent(t *testing.T) {
	d := openTestPostgres(t)
	ctx := context.Background()
	migrations := []Migration{
		{Version: 1, Name: "a", SQL: `CREATE TABLE a (id INTEGER PRIMARY KEY)`},
		{Version: 2, Name: "b", SQL: `CREATE TABLE b (id INTEGER PRIMARY KEY)`},
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

func TestPostgresPoolConfigIsApplied(t *testing.T) {
	dsn := testPostgresDSN(t)
	d, err := OpenPostgres(dsn, PoolConfig{MaxOpenConns: 7, MaxIdleConns: 3})
	if err != nil {
		t.Fatalf("OpenPostgres: %v", err)
	}
	defer d.Close()
	if got := d.sql.Stats().MaxOpenConnections; got != 7 {
		t.Errorf("MaxOpenConnections = %d, want 7", got)
	}
}
