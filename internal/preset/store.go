// Package preset persists pkg/contract.CompositionPreset documents (PRD
// §13.2/§13.3, Ticket P4.6) and implements the import half of the
// compatibility contract: checking a saved preset against the live block
// registry and a destination theme's declared regions, then — only if
// compatible — merging its Layout fragment into a target route's Layout via
// internal/layout.Store. It follows the same DB-backed store shape
// internal/layout already established: migrations declared alongside the
// store, a thin record/JSON-document column, and validation performed here
// (not trusted from the caller) so the store never holds a contract-invalid
// document.
package preset

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/compat"
	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrNotFound reports that no preset exists with the given ID.
var ErrNotFound = errors.New("preset not found")

// Migrations is the schema baseline for preset storage.
//
// Migration version numbers are global across every package's combined
// migration slice (internal/db/migrate.go rejects duplicates) — 17 is the
// next unused number after layouts (16), re-verified by grepping `Version:`
// across internal/*/*.go immediately before writing this file, per this
// project's own recurring-mistake note (other Phase-4 tickets may land
// migrations in parallel).
var Migrations = []db.Migration{
	{
		Version: 17,
		Name:    "presets baseline",
		SQL: `
			CREATE TABLE presets (
				id         TEXT NOT NULL PRIMARY KEY,
				document   TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
		`,
	},
}

// Record is a saved CompositionPreset plus the store-assigned identity and
// timestamps around it — the same split internal/content.Item already draws
// between a content item's own portable Data and its ID/timestamps.
// contract.CompositionPreset itself stays free of any storage-assigned ID:
// it is the artifact distributed through the marketplace too (PRD §13.4), a
// plain document with no notion of "which row in which host's DB it lives
// in"; Record is what this host's store hands back once one has been saved
// here.
type Record struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	contract.CompositionPreset
}

// Store persists CompositionPreset documents, each keyed by a
// store-generated ID (distinct from the human-chosen Name — two presets may
// share a Name; IDs never collide).
type Store struct {
	db *db.DB
}

// NewStore wires the store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Get returns the preset saved under id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (*Record, error) {
	var doc, created, updated string
	err := s.db.QueryRow(ctx, `SELECT document, created_at, updated_at FROM presets WHERE id = ?`, id).
		Scan(&doc, &created, &updated)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load preset %q: %w", id, err)
	}
	return decodeRecord(id, doc, created, updated)
}

// List returns every saved preset, ordered by creation time then ID.
func (s *Store) List(ctx context.Context) ([]*Record, error) {
	rows, err := s.db.Query(ctx, `SELECT id, document, created_at, updated_at FROM presets ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list presets: %w", err)
	}
	defer rows.Close()

	out := []*Record{}
	for rows.Next() {
		var id, doc, created, updated string
		if err := rows.Scan(&id, &doc, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan preset: %w", err)
		}
		r, err := decodeRecord(id, doc, created, updated)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func decodeRecord(id, doc, created, updated string) (*Record, error) {
	var p contract.CompositionPreset
	if err := json.Unmarshal([]byte(doc), &p); err != nil {
		return nil, fmt.Errorf("decode preset %q: %w", id, err)
	}
	r := &Record{ID: id, CompositionPreset: p}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

// Save validates p — p.Validate()'s structural check, then
// compat.CheckPreset's live-registry check — and persists it under a new
// generated ID, returned alongside the saved preset. principal must hold
// permission.PresetsManage.
//
// Save requires every block type p's manifest declares or its composition
// tree actually uses to already be registered in registry: a preset saved
// this way is authored locally, from blocks the author actually has, so
// there is no legitimate reason for it to reference something unregistered
// — unlike Import, which must tolerate (and explain, not silently drop) a
// preset built somewhere else against a block this host lacks.
// registry is passed in per call, not captured at NewStore time, mirroring
// internal/layout.Store.Save's identical reasoning: it is the live, shared,
// running daemon's registry, not state this store owns.
func (s *Store) Save(ctx context.Context, principal *permission.Principal, registry *blocks.Registry, p *contract.CompositionPreset) (*Record, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return nil, permission.ErrDenied
	}
	if err := p.Validate(); err != nil {
		return nil, err
	}
	result := compat.CheckPreset(p, registry, nil) // nil: no theme-slot gate at author-time save
	if !result.Compatible {
		return nil, result.AsValidationErrors()
	}

	return s.insert(ctx, p)
}

// insert persists p under a newly generated ID and returns the saved
// Record. Shared by Save (after its own compat-gated validation, above) and
// InstallFromPackage (marketplace.go — after its own, deliberately
// different, validation), so both write through the identical
// encode/INSERT/Get path rather than duplicating it.
func (s *Store) insert(ctx context.Context, p *contract.CompositionPreset) (*Record, error) {
	id := newID()
	doc, err := json.Marshal(p)
	if err != nil {
		return nil, fmt.Errorf("encode preset %q: %w", id, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(ctx,
		`INSERT INTO presets (id, document, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, string(doc), now, now); err != nil {
		return nil, fmt.Errorf("save preset %q: %w", id, err)
	}
	return s.Get(ctx, id)
}

// Check runs the compatibility contract for the preset saved under id
// against registry and themeRegions (a destination theme's declared
// Theme.Regions(), or nil for "no declared restriction"), without mutating
// anything — the read-only "would importing this work?" query a future UI
// calls before committing to Import. No capability gate: like layout reads
// and block-registry reads, checking compatibility discloses nothing
// sensitive.
func (s *Store) Check(ctx context.Context, registry *blocks.Registry, themeRegions []string, id string) (compat.Result, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return compat.Result{}, err
	}
	return compat.CheckPreset(&r.CompositionPreset, registry, themeRegions), nil
}

// Import checks the preset saved under id against registry and
// themeRegions, and — only if compat.Result.Compatible — merges its Layout
// fragment into the Layout saved for targetRoute via layoutStore
// (overwriting a same-named region if targetRoute already has one, exactly
// as re-PUTting a whole Layout already does for any other region conflict;
// per-block/section merging within a shared region is not attempted, since
// pkg/contract.Layout has no notion of "insert at position N" to merge
// against). Import always returns the compat.Result, whether or not the
// merge happened, so a caller can render "this preset needs the
// pricing-table block — install it?" instead of a bare error even when
// Import declines to merge (PRD §13.3's explicit "explainable" requirement).
// principal must hold permission.PresetsManage for the compatibility check
// and, if the merge proceeds, is passed through to layoutStore.Save, which
// itself re-checks permission.LayoutsManage (defense-in-depth, PRD §10.5;
// both capabilities are admin-only in this role matrix, so an admin calling
// Import always holds both).
func (s *Store) Import(ctx context.Context, principal *permission.Principal, registry *blocks.Registry, layoutStore *layout.Store, themeRegions []string, id, targetRoute string) (compat.Result, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return compat.Result{}, permission.ErrDenied
	}
	r, err := s.Get(ctx, id)
	if err != nil {
		return compat.Result{}, err
	}
	result := compat.CheckPreset(&r.CompositionPreset, registry, themeRegions)
	if !result.Compatible {
		return result, nil
	}

	target, err := layoutStore.Load(ctx, targetRoute)
	if errors.Is(err, layout.ErrNotFound) {
		target = &contract.Layout{ContractVersion: contract.LayoutCompositionV1, Regions: map[string]contract.Region{}}
	} else if err != nil {
		return result, fmt.Errorf("load target layout %q: %w", targetRoute, err)
	}
	if target.Regions == nil {
		target.Regions = map[string]contract.Region{}
	}
	for name, region := range r.Layout.Regions {
		target.Regions[name] = region
	}
	if err := layoutStore.Save(ctx, principal, registry, targetRoute, target); err != nil {
		return result, fmt.Errorf("merge preset %q into %q: %w", id, targetRoute, err)
	}
	return result, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
