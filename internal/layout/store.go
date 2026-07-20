// Package layout persists Layer-2 pkg/contract.Layout documents, one per
// named route (PRD §14 slice 4.4a). It follows the same DB-backed store
// shape internal/composition and internal/content already established:
// migrations declared alongside the store, a thin record/JSON-document
// column, and validation performed here (not trusted from the caller) so the
// store never holds a contract-invalid document — see internal/composition's
// Store.SaveWith doc comment for the precedent this mirrors.
package layout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrNotFound reports that no Layout has been saved for the given route.
var ErrNotFound = errors.New("layout not found")

// Migrations is the schema baseline for layout storage.
//
// Migration version numbers are global across every package's combined
// migration slice (internal/db/migrate.go rejects duplicates) — 16 is the
// next unused number after audit (15), consent (14), MFA (12, 13), OAuth
// (11), users_manage (10), identity (2, 4), sessions (4)... re-verified by
// grepping `Version:` across internal/*/*.go immediately before writing this
// file, per this project's own recurring-mistake note.
var Migrations = []db.Migration{
	{
		Version: 16,
		Name:    "layouts baseline",
		SQL: `
			CREATE TABLE layouts (
				route      TEXT NOT NULL PRIMARY KEY,
				document   TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
		`,
	},
}

// routeSegment matches one "/"-delimited path segment of a route: a
// lowercase identifier (a-z, 0-9, _, -), mirroring pkg/contract's own
// validIdent convention for region/slot names but additionally allowing
// hyphens, which are common in URL path segments (content type/field names
// never appear directly in a URL, but a route is a URL path).
var routeSegment = regexp.MustCompile(`^[a-z0-9_-]+$`)

// validRoute reports whether route is an acceptable Layout key: one or more
// non-empty "/"-delimited lowercase segments, with no leading, trailing, or
// doubled slash (so "home" and "blog/index" are valid; "", "/home",
// "home/", and "blog//index" are not). This is a deliberately conservative
// subset of what a URL path segment could contain — free-form enough to
// name any route a theme's routing might use, restrictive enough to rule
// out the ambiguous edge cases (empty segments, mixed case) before they ever
// reach storage.
func validRoute(route string) bool {
	if route == "" {
		return false
	}
	for _, seg := range strings.Split(route, "/") {
		if !routeSegment.MatchString(seg) {
			return false
		}
	}
	return true
}

// Store persists Layout documents keyed by route.
type Store struct {
	db *db.DB
}

// NewStore wires the store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Load returns the Layout saved for route, or ErrNotFound if none has been
// saved yet.
func (s *Store) Load(ctx context.Context, route string) (*contract.Layout, error) {
	var doc string
	err := s.db.QueryRow(ctx, `SELECT document FROM layouts WHERE route = ?`, route).Scan(&doc)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load layout %q: %w", route, err)
	}
	var l contract.Layout
	if err := json.Unmarshal([]byte(doc), &l); err != nil {
		return nil, fmt.Errorf("decode layout %q: %w", route, err)
	}
	return &l, nil
}

// ValidateDraft checks l structurally (l.Validate()) and then against
// registry's registered block types (blocks.ValidateLayout), in that order
// — the exact validation sequence a Layout must pass before it is trusted
// for anything, whether that's Save persisting it or a caller (e.g.
// internal/api's live-preview endpoint, Ticket P4.5) rendering it without
// persisting it at all. Extracted into one shared function specifically so
// every caller validating a Layout draft picks up the identical sequence:
// if this ever gains a third check, Save and every other caller gain it too
// without each needing its own matching edit.
func ValidateDraft(l *contract.Layout, registry *blocks.Registry) error {
	if err := l.Validate(); err != nil {
		return err
	}
	if err := blocks.ValidateLayout(l, registry); err != nil {
		return err
	}
	return nil
}

// Save validates l — route format, then ValidateDraft's structural +
// registry-existence checks, in that order — and persists it for route only
// if every check passes; an invalid Layout never reaches storage. principal
// must hold permission.LayoutsManage.
//
// registry is passed in per call (rather than captured at NewStore time)
// because it is the live, shared, running daemon's block registry — the
// same *blocks.Registry threaded through internal/api.Server — not state
// this store owns.
func (s *Store) Save(ctx context.Context, principal *permission.Principal, registry *blocks.Registry, route string, l *contract.Layout) error {
	if !permission.AllowsPrincipal(principal, permission.LayoutsManage) {
		return permission.ErrDenied
	}
	if !validRoute(route) {
		return contract.ValidationErrors{{
			Path:    "route",
			Message: "must be one or more lowercase, non-empty, \"/\"-delimited segments (a-z, 0-9, _, -)",
		}}
	}
	if err := ValidateDraft(l, registry); err != nil {
		return err
	}
	doc, err := json.Marshal(l)
	if err != nil {
		return fmt.Errorf("encode layout %q: %w", route, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = s.db.Exec(ctx, `
		INSERT INTO layouts (route, document, created_at, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (route) DO UPDATE SET document = excluded.document, updated_at = excluded.updated_at`,
		route, string(doc), now, now)
	if err != nil {
		return fmt.Errorf("save layout %q: %w", route, err)
	}
	return nil
}
