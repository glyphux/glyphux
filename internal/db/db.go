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

// Ping verifies the connection is still reachable — the seam readiness
// checks use, since a liveness check (the process is running) is not the
// same claim as a readiness check (the process can serve real requests).
func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

// ErrNoRows is returned by Row.Scan when the query returned no rows. Callers
// above this package check errors.Is(err, db.ErrNoRows) instead of importing
// database/sql themselves (§5.2: nothing above the kernel knows the driver).
var ErrNoRows = sql.ErrNoRows

// Result reports the outcome of Exec. It is a narrow alias over
// database/sql's already-abstract result interface — no concrete sql type
// crosses this boundary.
type Result = sql.Result

// Row is the narrow interface QueryRow returns: scan-only, no driver detail.
type Row interface {
	Scan(dest ...any) error
}

// Rows is the narrow interface Query returns: iterate, scan, close.
type Rows interface {
	Next() bool
	Scan(dest ...any) error
	Close() error
	Err() error
}

// Queryer is the read/write surface domain stores depend on. *DB implements
// it directly; WithTx hands callers a transaction-scoped Queryer with the
// identical surface, so store code never needs to know whether it is
// running inside a transaction.
type Queryer interface {
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
	Query(ctx context.Context, query string, args ...any) (Rows, error)
}

// rebind rewrites `?` placeholders to Postgres's `$1, $2, ...` form. Every
// caller above this package writes SQLite-style `?` placeholders; this is the
// single choke point that makes them portable to Postgres (§5.1: nothing
// above the kernel knows which driver is underneath).
func rebind(driver, query string) string {
	if driver != "postgres" || !strings.Contains(query, "?") {
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

func (d *DB) rebind(query string) string { return rebind(d.driver, query) }

// Exec runs a statement inside the abstraction.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return d.sql.ExecContext(ctx, d.rebind(query), args...)
}

// QueryRow runs a single-row query inside the abstraction.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) Row {
	return d.sql.QueryRowContext(ctx, d.rebind(query), args...)
}

// Query runs a multi-row query inside the abstraction. The returned Rows
// streams results row by row as the caller advances it — large reads and
// exports never buffer the full result set in memory (§11.6).
func (d *DB) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return d.sql.QueryContext(ctx, d.rebind(query), args...)
}

// Tx runs fn inside a transaction, committing on nil and rolling back on
// error. Package-internal only (the migration runner uses it) — callers
// outside internal/db use WithTx, which never exposes *sql.Tx.
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

// WithTx runs fn inside a transaction, committing on nil and rolling back on
// error. fn receives a Queryer with the identical surface *DB exposes, so
// store methods written against Queryer work unchanged whether called
// directly or from inside a transaction — this is what lets bootstrap create
// the admin account and the initial composition atomically (§17.2).
func (d *DB) WithTx(ctx context.Context, fn func(q Queryer) error) error {
	tx, err := d.sql.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(&txQueryer{tx: tx, driver: d.driver}); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback failed: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}

// txQueryer adapts a *sql.Tx to Queryer, applying the same placeholder
// rebind as *DB so store code is identical inside and outside a transaction.
type txQueryer struct {
	tx     *sql.Tx
	driver string
}

func (q *txQueryer) Exec(ctx context.Context, query string, args ...any) (Result, error) {
	return q.tx.ExecContext(ctx, rebind(q.driver, query), args...)
}

func (q *txQueryer) QueryRow(ctx context.Context, query string, args ...any) Row {
	return q.tx.QueryRowContext(ctx, rebind(q.driver, query), args...)
}

func (q *txQueryer) Query(ctx context.Context, query string, args ...any) (Rows, error) {
	return q.tx.QueryContext(ctx, rebind(q.driver, query), args...)
}
