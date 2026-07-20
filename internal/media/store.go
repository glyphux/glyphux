package media

import (
	"context"
	"encoding/json"
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
	{
		// Migration version numbers are global across every package's
		// combined migration slice (internal/db/migrate.go rejects
		// duplicates) — 8 is the next unused number after identity (2, 4),
		// content (3, 5), media (6), and composition (7).
		Version: 8,
		Name:    "media items tags and source/attribution",
		SQL: `
			ALTER TABLE media_items ADD COLUMN tags TEXT NOT NULL DEFAULT '[]';
			ALTER TABLE media_items ADD COLUMN source TEXT NOT NULL DEFAULT '';
			ALTER TABLE media_items ADD COLUMN attribution TEXT NOT NULL DEFAULT '';
		`,
	},
}

// record is the stored form of a media item. Tags is stored as a JSON array
// in a single TEXT column — matching how similarly small, not-independently-
// queried array fields are modeled elsewhere in this schema (e.g.
// composition's ContentTypes document), rather than a join table, since
// nothing here needs to query "all items with tag X" at the SQL level yet.
type record struct {
	ID          string
	Filename    string
	MimeType    string
	SizeBytes   int64
	Width       int
	Height      int
	StoragePath string
	AltText     string
	Tags        []string
	Source      string
	Attribution string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// encodeTags renders tags as a JSON array, normalizing nil to "[]" so a
// never-tagged item's stored value (and anything re-serialized from it) is
// never JSON null.
func encodeTags(tags []string) (string, error) {
	if tags == nil {
		tags = []string{}
	}
	encoded, err := json.Marshal(tags)
	if err != nil {
		return "", fmt.Errorf("encode tags: %w", err)
	}
	return string(encoded), nil
}

// decodeTags parses a stored tags column back into a slice, normalizing an
// empty or unparseable value to an empty (non-nil) slice.
func decodeTags(raw string) []string {
	if raw == "" {
		return []string{}
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil || tags == nil {
		return []string{}
	}
	return tags
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
	encodedTags, err := encodeTags(r.Tags)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx,
		`INSERT INTO media_items (id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, tags, source, attribution, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.Filename, r.MimeType, r.SizeBytes, r.Width, r.Height, r.StoragePath, r.AltText, encodedTags, r.Source, r.Attribution,
		r.CreatedAt.UTC().Format(time.RFC3339Nano), r.UpdatedAt.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("insert media item: %w", err)
	}
	return nil
}

func (s *Store) getByID(ctx context.Context, id string) (record, error) {
	var r record
	var created, updated, tags string
	err := s.db.QueryRow(ctx,
		`SELECT id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, tags, source, attribution, created_at, updated_at
		 FROM media_items WHERE id = ?`, id).
		Scan(&r.ID, &r.Filename, &r.MimeType, &r.SizeBytes, &r.Width, &r.Height, &r.StoragePath, &r.AltText, &tags, &r.Source, &r.Attribution, &created, &updated)
	if errors.Is(err, db.ErrNoRows) {
		return record{}, ErrNotFound
	}
	if err != nil {
		return record{}, fmt.Errorf("get media item: %w", err)
	}
	r.Tags = decodeTags(tags)
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

func (s *Store) list(ctx context.Context) ([]record, error) {
	rows, err := s.db.Query(ctx,
		`SELECT id, filename, mime_type, size_bytes, width, height, storage_path, alt_text, tags, source, attribution, created_at, updated_at
		 FROM media_items ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list media items: %w", err)
	}
	defer rows.Close()

	var out []record
	for rows.Next() {
		var r record
		var created, updated, tags string
		if err := rows.Scan(&r.ID, &r.Filename, &r.MimeType, &r.SizeBytes, &r.Width, &r.Height, &r.StoragePath, &r.AltText, &tags, &r.Source, &r.Attribution, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan media item: %w", err)
		}
		r.Tags = decodeTags(tags)
		r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

// updateMetadata replaces id's editable metadata — alt text, tags, and
// source/attribution — leaving every upload-time field (filename,
// dimensions, mime type, storage path) untouched. Returns ErrNotFound if id
// does not exist.
func (s *Store) updateMetadata(ctx context.Context, id, altText string, tags []string, source, attribution string, updatedAt time.Time) error {
	encodedTags, err := encodeTags(tags)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(ctx,
		`UPDATE media_items SET alt_text = ?, tags = ?, source = ?, attribution = ?, updated_at = ? WHERE id = ?`,
		altText, encodedTags, source, attribution, updatedAt.UTC().Format(time.RFC3339Nano), id)
	if err != nil {
		return fmt.Errorf("update media metadata: %w", err)
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
