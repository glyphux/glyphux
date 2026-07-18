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
	{
		// Migration version numbers are global across every package's
		// combined migration slice (internal/db/migrate.go rejects
		// duplicates), not scoped to this package — 7 is the next unused
		// number after identity (2, 4), content (3, 5), and media (6).
		Version: 7,
		Name:    "composition optimistic-concurrency version",
		SQL: `
			ALTER TABLE composition ADD COLUMN version INTEGER NOT NULL DEFAULT 1;
		`,
	},
}

// maxCASAttempts bounds the load-modify-compare-and-swap retry loop
// DefineContentType/RemoveContentType use to avoid a lost update: two
// concurrent read-modify-write cycles on the single composition row would
// otherwise silently drop whichever one committed last, with the survivor's
// write based on a document the other's edit never touched.
const maxCASAttempts = 10

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
	comp, _, err := s.loadVersioned(ctx)
	return comp, err
}

// loadVersioned is Load plus the row's optimistic-concurrency version, for
// callers that need to compare-and-swap on write (DefineContentType,
// RemoveContentType).
func (s *Store) loadVersioned(ctx context.Context) (*contract.Composition, int64, error) {
	var doc string
	var version int64
	err := s.db.QueryRow(ctx, `SELECT document, version FROM composition WHERE id = 1`).Scan(&doc, &version)
	if errors.Is(err, db.ErrNoRows) {
		return nil, 0, ErrNotFound
	}
	if err != nil {
		return nil, 0, fmt.Errorf("load composition: %w", err)
	}
	comp, err := contract.Parse([]byte(doc))
	if err != nil {
		return nil, 0, err
	}
	return comp, version, nil
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
		INSERT INTO composition (id, document, updated_at, version) VALUES (1, ?, ?, 1)
		ON CONFLICT (id) DO UPDATE SET document = excluded.document, updated_at = excluded.updated_at,
			version = composition.version + 1`,
		string(doc), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save composition: %w", err)
	}
	return nil
}

// casSave persists c only if the row's version still equals expectVersion,
// bumping it atomically as part of the same UPDATE. Returns ok=false if
// another writer committed first (expectVersion is now stale) — the caller
// is expected to reload and retry.
func (s *Store) casSave(ctx context.Context, c *contract.Composition, expectVersion int64) (ok bool, err error) {
	if err := c.Validate(); err != nil {
		return false, err
	}
	doc, err := c.Encode()
	if err != nil {
		return false, fmt.Errorf("encode composition: %w", err)
	}
	res, err := s.db.Exec(ctx, `
		UPDATE composition SET document = ?, updated_at = ?, version = version + 1
		WHERE id = 1 AND version = ?`,
		string(doc), time.Now().UTC().Format(time.RFC3339Nano), expectVersion)
	if err != nil {
		return false, fmt.Errorf("save composition: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("save composition: %w", err)
	}
	return n == 1, nil
}

// DefineContentType creates a new content type or replaces an existing
// type's field set, validating the resulting document against the full
// contract schema before persisting — an invalid shape (unknown field type,
// a relation naming a target that doesn't exist, a non-identifier name)
// leaves the stored composition untouched.
func (s *Store) DefineContentType(ctx context.Context, name string, ct contract.ContentType) (*contract.Composition, error) {
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		comp, version, err := s.loadVersioned(ctx)
		if err != nil {
			return nil, err
		}
		if comp.ContentTypes == nil {
			comp.ContentTypes = map[string]contract.ContentType{}
		}
		comp.ContentTypes[name] = ct
		ok, err := s.casSave(ctx, comp, version)
		if err != nil {
			return nil, err
		}
		if ok {
			return comp, nil
		}
		// Another writer committed between our load and save — reload the
		// now-current document and reapply this edit on top of it, rather
		// than overwriting whatever that writer just did.
	}
	return nil, fmt.Errorf("define content type %q: too much write contention on the composition document", name)
}

// RemoveContentType deletes a content type from the composition, returning
// ErrContentTypeNotFound if no type by that name is declared. Callers are
// responsible for checking whether content items of that type still exist
// before calling this — the store itself has no notion of content items.
func (s *Store) RemoveContentType(ctx context.Context, name string) (*contract.Composition, error) {
	return s.RemoveContentTypeGuarded(ctx, name, nil)
}

// RemoveContentTypeGuarded is RemoveContentType, but calls guard (if
// non-nil) immediately before the final write on every attempt and aborts
// without writing if it returns an error. Callers that must ensure no
// content items of this type exist (the delete-guard in internal/api and
// internal/graphql) pass a guard that re-checks the item count here — this
// is the narrowest window the count check can be moved to without a
// transaction spanning both the composition and content stores, since an
// item could otherwise be created in the gap between an earlier count check
// and this write.
func (s *Store) RemoveContentTypeGuarded(ctx context.Context, name string, guard func(context.Context) error) (*contract.Composition, error) {
	for attempt := 0; attempt < maxCASAttempts; attempt++ {
		comp, version, err := s.loadVersioned(ctx)
		if err != nil {
			return nil, err
		}
		if _, ok := comp.ContentTypes[name]; !ok {
			return nil, ErrContentTypeNotFound
		}
		if guard != nil {
			if err := guard(ctx); err != nil {
				return nil, err
			}
		}
		delete(comp.ContentTypes, name)
		ok, err := s.casSave(ctx, comp, version)
		if err != nil {
			return nil, err
		}
		if ok {
			return comp, nil
		}
	}
	return nil, fmt.Errorf("remove content type %q: too much write contention on the composition document", name)
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
