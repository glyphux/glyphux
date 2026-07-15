// Package composition persists and serves the composition document — the
// single source of truth (Principle 1). The kernel owns this store; clients
// reach it only through domain APIs and the HTTP transport.
package composition

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrNotFound reports that no composition has been written yet (pre-setup).
var ErrNotFound = errors.New("no composition stored")

// ErrContentTypeNotFound reports that no content type by that name is
// declared in the composition.
var ErrContentTypeNotFound = errors.New("content type not found")

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
	if errors.Is(err, db.ErrNoRows) {
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
	return s.SaveWith(ctx, s.db, c)
}

// SaveWith validates and persists the composition using q instead of the
// store's own database handle — q is typically a transaction from
// db.WithTx, so bootstrap can save the initial composition and create the
// admin account atomically: both commit together, or neither does.
func (s *Store) SaveWith(ctx context.Context, q db.Queryer, c *contract.Composition) error {
	if err := c.Validate(); err != nil {
		return err
	}
	doc, err := c.Encode()
	if err != nil {
		return fmt.Errorf("encode composition: %w", err)
	}
	_, err = q.Exec(ctx, `
		INSERT INTO composition (id, document, updated_at) VALUES (1, ?, ?)
		ON CONFLICT (id) DO UPDATE SET document = excluded.document, updated_at = excluded.updated_at`,
		string(doc), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save composition: %w", err)
	}
	return nil
}

// DefineContentType creates a new content type or replaces an existing
// type's field set, validating the resulting document against the full
// contract schema before persisting — an invalid shape (unknown field type,
// a relation naming a target that doesn't exist, a non-identifier name)
// leaves the stored composition untouched.
func (s *Store) DefineContentType(ctx context.Context, name string, ct contract.ContentType) (*contract.Composition, error) {
	comp, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	if comp.ContentTypes == nil {
		comp.ContentTypes = map[string]contract.ContentType{}
	}
	comp.ContentTypes[name] = ct
	if err := s.Save(ctx, comp); err != nil {
		return nil, err
	}
	return comp, nil
}

// RemoveContentType deletes a content type from the composition, returning
// ErrContentTypeNotFound if no type by that name is declared. Callers are
// responsible for checking whether content items of that type still exist
// before calling this — the store itself has no notion of content items.
func (s *Store) RemoveContentType(ctx context.Context, name string) (*contract.Composition, error) {
	comp, err := s.Load(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := comp.ContentTypes[name]; !ok {
		return nil, ErrContentTypeNotFound
	}
	delete(comp.ContentTypes, name)
	if err := s.Save(ctx, comp); err != nil {
		return nil, err
	}
	return comp, nil
}

// Ping verifies the underlying database connection is reachable — the
// readiness check's seam into the kernel.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

// Exists reports whether a composition has been written (setup completed).
func (s *Store) Exists(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT COUNT(*) FROM composition WHERE id = 1`).Scan(&n); err != nil {
		return false, fmt.Errorf("check composition: %w", err)
	}
	return n > 0, nil
}
