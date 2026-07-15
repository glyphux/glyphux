package media

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
)

// ErrNotFound reports that no media item exists for the given id.
var ErrNotFound = errors.New("media item not found")

// Migrations is the schema baseline for media storage.
var Migrations = []db.Migration{
	{
		Version: 6,
		Name:    "media items baseline",
		SQL: `
			CREATE TABLE media_items (
				id           TEXT NOT NULL PRIMARY KEY,
				filename     TEXT NOT NULL,
				mime_type    TEXT NOT NULL,
				size_bytes   INTEGER NOT NULL,
				width        INTEGER NOT NULL DEFAULT 0,
				height       INTEGER NOT NULL DEFAULT 0,
				storage_path TEXT NOT NULL,
				alt_text     TEXT NOT NULL DEFAULT '',
				created_at   TEXT NOT NULL,
				updated_at   TEXT NOT NULL
			);
			CREATE INDEX idx_media_items_created ON media_items (created_at);
		`,
	},
}

// record is the stored form of a media item.
type record struct {
	ID          string
	Filename    string
	MimeType    string
	SizeBytes   int64
	Width       int
	Height      int
	StoragePath string
	AltText     string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Store persists media metadata behind the database abstraction. It holds no
// knowledge of the filesystem — that is the API's concern.
type Store struct {
	db *db.DB
}

// NewStore wires the media store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

func (s *Store) insert(ctx context.Context, r record) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO media_items (id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Filename, r.MimeType, r.SizeBytes, r.Width, r.Height, r.StoragePath, r.AltText,
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert media item: %w", err)
	}
	return nil
}

func (s *Store) getByID(ctx context.Context, id string) (record, error) {
	var r record
	var created, updated string
	err := s.db.QueryRow(ctx,
		`SELECT id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, created_at, updated_at
		 FROM media_items WHERE id = ?`, id).
		Scan(&r.ID, &r.Filename, &r.MimeType, &r.SizeBytes, &r.Width, &r.Height, &r.StoragePath, &r.AltText, &created, &updated)
	if errors.Is(err, db.ErrNoRows) {
		return record{}, ErrNotFound
	}
	if err != nil {
		return record{}, fmt.Errorf("get media item: %w", err)
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

func (s *Store) list(ctx context.Context) ([]record, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, created_at, updated_at
		 FROM media_items ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list media items: %w", err)
	}
	defer rows.Close()

	var out []record
	for rows.Next() {
		var r record
		var created, updated string
		if err := rows.Scan(&r.ID, &r.Filename, &r.MimeType, &r.SizeBytes, &r.Width, &r.Height, &r.StoragePath, &r.AltText, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan media item: %w", err)
		}
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) delete(ctx context.Context, id string) error {
	res, err := s.db.Exec(ctx, `DELETE FROM media_items WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete media item: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
