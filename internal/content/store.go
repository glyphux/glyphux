package content

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// ErrNotFound reports that no content item exists for the given type and id.
var ErrNotFound = errors.New("content item not found")

// Migrations is the Phase-1 schema baseline for content storage. Content items
// are stored as validated JSON documents keyed by (type, id): the composition
// is the schema, so per-type DDL is unnecessary and the substrate stays stable
// as content types are added or changed.
var Migrations = []db.Migration{
	{
		Version: 3,
		Name:    "content items baseline",
		SQL: `
			CREATE TABLE content_items (
				id         TEXT NOT NULL PRIMARY KEY,
				type       TEXT NOT NULL,
				data       TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
			CREATE INDEX idx_content_items_type ON content_items (type, created_at);
		`,
	},
}

// record is the stored form of a content item — JSON data plus timestamps.
type record struct {
	ID        string
	Type      string
	Data      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Store persists content items behind the database abstraction. It holds no
// knowledge of the contract — validation is the API's concern.
type Store struct {
	db *db.DB
}

// NewStore wires the item store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) insert(ctx context.Context, r record) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO content_items (id, type, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		r.ID, r.Type, r.Data, r.CreatedAt.UTC().Format(time.RFC3339Nano), r.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert content item: %w", err)
	}
	return nil
}

func (s *Store) getByID(ctx context.Context, typeName, id string) (record, error) {
	var r record
	var created, updated string
	err := s.db.QueryRow(ctx,
		`SELECT id, type, data, created_at, updated_at FROM content_items WHERE type = ? AND id = ?`,
		typeName, id).Scan(&r.ID, &r.Type, &r.Data, &created, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return record{}, ErrNotFound
	}
	if err != nil {
		return record{}, fmt.Errorf("get content item: %w", err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

func (s *Store) exists(ctx context.Context, typeName, id string) (bool, error) {
	var n int
	err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FROM content_items WHERE type = ? AND id = ?`, typeName, id).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check content item: %w", err)
	}
	return n > 0, nil
}

func (s *Store) listByType(ctx context.Context, typeName string) ([]record, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, type, data, created_at, updated_at FROM content_items WHERE type = ? ORDER BY created_at, id`,
		typeName)
	if err != nil {
		return nil, fmt.Errorf("list content items: %w", err)
	}
	defer rows.Close()

	var out []record
	for rows.Next() {
		var r record
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Type, &r.Data, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan content item: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// update rewrites an existing item's data and updated_at, returning ErrNotFound
// if no row of the given type and id exists.
func (s *Store) update(ctx context.Context, typeName, id, data string, updatedAt time.Time) error {
	res, err := s.db.Exec(ctx,
		`UPDATE content_items SET data = ?, updated_at = ? WHERE type = ? AND id = ?`,
		data, updatedAt.UTC().Format(time.RFC3339Nano), typeName, id)
	if err != nil {
		return fmt.Errorf("update content item: %w", err)
	}
	return affectedOrNotFound(res)
}

// delete removes an item, returning ErrNotFound if none matched.
func (s *Store) delete(ctx context.Context, typeName, id string) error {
	res, err := s.db.Exec(ctx,
		`DELETE FROM content_items WHERE type = ? AND id = ?`, typeName, id)
	if err != nil {
		return fmt.Errorf("delete content item: %w", err)
	}
	return affectedOrNotFound(res)
}

func affectedOrNotFound(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
