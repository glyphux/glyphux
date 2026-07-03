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

	_ "modernc.org/sqlite" // pure-Go SQLite; preserves the single static binary
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

// Close releases the underlying handle.
func (d *DB) Close() error { return d.sql.Close() }

// Exec runs a statement inside the abstraction.
func (d *DB) Exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return d.sql.ExecContext(ctx, query, args...)
}

// QueryRow runs a single-row query inside the abstraction.
func (d *DB) QueryRow(ctx context.Context, query string, args ...any) *sql.Row {
	return d.sql.QueryRowContext(ctx, query, args...)
}

// Query runs a multi-row query inside the abstraction.
func (d *DB) Query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return d.sql.QueryContext(ctx, query, args...)
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
