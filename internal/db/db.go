// Package db is the database abstraction behind the kernel (an adapter,
// §5.1). Nothing above the kernel may see a raw *sql.DB: the kernel calls
// this package, and this package alone knows which driver is underneath
// (Communication Law, §5.2).
package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib" // pure-Go Postgres driver
	_ "modernc.org/sqlite"             // pure-Go SQLite; preserves the single static binary
)

// DB wraps the underlying database handle. It is intentionally unexported
// beyond the kernel: domain APIs receive typed store interfaces, never DB.
type DB struct {
	sql    *sql.DB
	driver string
}

// OpenSQLite opens (creating if needed) the embedded SQLite database at path.
func OpenSQLite(path string) (*DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	// WAL + foreign keys + busy timeout: the sane embedded defaults.
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)", path)
	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// SQLite serializes writers; one connection avoids SQLITE_BUSY churn.
	sqlDB.SetMaxOpenConns(1)
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &DB{sql: sqlDB, driver: "sqlite"}, nil
}

// PoolConfig sizes and bounds a connection pool's lifecycle (§11.6). Zero
// values fall back to sane auto-sized defaults.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// withDefaults fills unset fields with production-sane defaults.
func (c PoolConfig) withDefaults() PoolConfig {
	if c.MaxOpenConns <= 0 {
		c.MaxOpenConns = 20
	}
	if c.MaxIdleConns <= 0 {
		c.MaxIdleConns = 5
	}
	if c.ConnMaxLifetime <= 0 {
		c.ConnMaxLifetime = 30 * time.Minute
	}
	if c.ConnMaxIdleTime <= 0 {
		c.ConnMaxIdleTime = 5 * time.Minute
	}
	return c
}

// OpenPostgres opens a pooled connection to a Postgres server. Behind the
// same DB abstraction as OpenSQLite: callers above the kernel never know
// which driver is underneath (§5.1, §11.6).
func OpenPostgres(dsn string, pool PoolConfig) (*DB, error) {
	sqlDB, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	pool = pool.withDefaults()
	sqlDB.SetMaxOpenConns(pool.MaxOpenConns)
	sqlDB.SetMaxIdleConns(pool.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(pool.ConnMaxLifetime)
	sqlDB.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping postgres: %w", err)
	}
	return &DB{sql: sqlDB, driver: "postgres"}, nil
}

// Close releases the underlying handle.
func (d *DB) Close() error { return d.sql.Close() }

// rebind rewrites `?` placeholders to Postgres's `$1, $2, ...` form. Every
// caller above this package writes SQLite-style `?` placeholders; this is the
// single choke point that makes them portable to Postgres (§5.1: nothing
// above the kernel knows which driver is underneath).
func (d *DB) rebind(query string) string {
	if d.driver != "postgres" || !strings.Contains(query, "?") {
		return query
	}
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			fmt.Fprintf(&b, "$%d", n)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Exec runs a statement inside the abstraction.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sql.ExecContext(ctx, d.rebind(query), args...)
}

// QueryRow runs a single-row query inside the abstraction.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sql.QueryRowContext(ctx, d.rebind(query), args...)
}

// Query runs a multi-row query inside the abstraction. The returned *sql.Rows
// streams results row by row as the caller advances it — large reads and
// exports never buffer the full result set in memory (§11.6).
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sql.QueryContext(ctx, d.rebind(query), args...)
}

// Tx runs fn inside a transaction, committing on nil and rolling back on error.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}
