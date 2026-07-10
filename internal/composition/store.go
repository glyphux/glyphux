// Package composition persists and serves the composition document — the
// single source of truth (Principle 1). The kernel owns this store; clients
// reach it only through domain APIs and the HTTP transport.
package composition

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrNotFound reports that no composition has been written yet (pre-setup).
var ErrNotFound = errors.New("no composition stored")

// Migrations is the Phase-0 schema baseline for composition storage.
var Migrations = []db.Migration{
	{
		Version: 1,
		Name:    "composition baseline",
		SQL: `
			CREATE TABLE composition (
				id         INTEGER PRIMARY KEY CHECK (id = 1),
				document   TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
		`,
	},
}

// Store reads and writes the composition document.
type Store struct {
	db *db.DB
}

// NewStore wires the store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Load returns the current composition, or ErrNotFound before first setup.
func (s *Store) Load(ctx context.Context) (*contract.Composition, error) {
	var doc string
	err := s.db.QueryRow(ctx, `SELECT document FROM composition WHERE id = 1`).Scan(&doc)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load composition: %w", err)
	}
	return contract.Parse([]byte(doc))
}

// Save validates and persists the composition. Invalid compositions are
// rejected before touching storage — the store never holds a contract-invalid
// document.
func (s *Store) Save(ctx context.Context, c *contract.Composition) error {
	if err := c.Validate(); err != nil {
		return err
	}
	doc, err := c.Encode()
	if err != nil {
		return fmt.Errorf("encode composition: %w", err)
	}
	_, err = s.db.Exec(ctx, `
		INSERT INTO composition (id, document, updated_at) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET document = excluded.document, updated_at = excluded.updated_at`,
		string(doc), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save composition: %w", err)
	}
	return nil
}

// Exists reports whether a composition has been written (setup completed).
func (s *Store) Exists(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM composition WHERE id = 1`).Scan(&n); err != nil {
		return false, fmt.Errorf("check composition: %w", err)
	}
	return n > 0, nil
}
