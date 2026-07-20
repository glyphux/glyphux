// Package bundle persists pkg/contract.CompositionBundle documents (PRD
// §13.2/§13.3, Ticket P4.6) — a preset collection at site scale: pages +
// presets + sample content + a theme reference, "import this starter site /
// demo" made real. It mirrors internal/preset's shape (and, one level
// further back, internal/layout's): migrations declared alongside the
// store, validation performed here (not trusted from the caller), and the
// same compatibility-contract-gated Import pattern — checked against the
// live block registry and a destination theme's declared regions before any
// page's Layout is merged or any sample content item is created.
package bundle

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/compat"
	"github.com/glyphux/glyphux/pkg/contract"
)

// ErrNotFound reports that no bundle exists with the given ID.
var ErrNotFound = errors.New("bundle not found")

// Migrations is the schema baseline for bundle storage.
//
// Migration version numbers are global across every package's combined
// migration slice (internal/db/migrate.go rejects duplicates) — 18 is the
// next unused number after presets (17), re-verified by grepping `Version:`
// across internal/*/*.go immediately before writing this file, per this
// project's own recurring-mistake note (other Phase-4 tickets may land
// migrations in parallel).
var Migrations = []db.Migration{
	{
		Version: 18,
		Name:    "bundles baseline",
		SQL: `
			CREATE TABLE bundles (
				id         TEXT NOT NULL PRIMARY KEY,
				document   TEXT NOT NULL,
				created_at TEXT NOT NULL,
				updated_at TEXT NOT NULL
			);
		`,
	},
}

// Record is a saved CompositionBundle plus the store-assigned identity and
// timestamps around it — see internal/preset.Record's identical doc comment
// for why contract.CompositionBundle itself carries no ID field.
type Record struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	contract.CompositionBundle
}

// ContentRef identifies one Layer-1 content item an Import created from a
// bundle's SampleContent.
type ContentRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ImportResult reports what Import did — or, if the bundle was
// incompatible, declined to do. Compat is always populated; ImportedPages
// and CreatedContent/ContentErrors are only populated when Compat.Compatible
// is true (Import merges nothing and creates nothing otherwise).
type ImportResult struct {
	Compat compat.Result `json:"compat"`
	// ImportedPages lists the routes whose Layout was merged, in the same
	// order as the bundle's own Pages (Go maps have no order of their own,
	// so this is deterministic, sorted by route).
	ImportedPages []string `json:"imported_pages,omitempty"`
	// CreatedContent lists every sample content item successfully created.
	CreatedContent []ContentRef `json:"created_content,omitempty"`
	// ContentErrors reports, for any SampleContent item that failed to
	// create (e.g. an unknown content type — a content-model concern, not
	// part of the block/slot compatibility contract itself), the index into
	// Bundle.SampleContent and the error message. Sample content creation is
	// deliberately best-effort: a page Layout merge already succeeded by
	// the time content creation runs, so aborting the whole Import over one
	// bad sample item would throw away real, already-valid page structure
	// for a problem that has nothing to do with page structure.
	ContentErrors []string `json:"content_errors,omitempty"`
}

// Store persists CompositionBundle documents, each keyed by a
// store-generated ID.
type Store struct {
	db *db.DB
}

// NewStore wires the store to the database abstraction.
func NewStore(database *db.DB) *Store {
	return &Store{db: database}
}

// Get returns the bundle saved under id, or ErrNotFound.
func (s *Store) Get(ctx context.Context, id string) (*Record, error) {
	var doc, created, updated string
	err := s.db.QueryRow(ctx, `SELECT document, created_at, updated_at FROM bundles WHERE id = ?`, id).
		Scan(&doc, &created, &updated)
	if errors.Is(err, db.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load bundle %q: %w", id, err)
	}
	return decodeRecord(id, doc, created, updated)
}

// List returns every saved bundle, ordered by creation time then ID.
func (s *Store) List(ctx context.Context) ([]*Record, error) {
	rows, err := s.db.Query(ctx, `SELECT id, document, created_at, updated_at FROM bundles ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("list bundles: %w", err)
	}
	defer rows.Close()

	out := []*Record{}
	for rows.Next() {
		var id, doc, created, updated string
		if err := rows.Scan(&id, &doc, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan bundle: %w", err)
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
	var b contract.CompositionBundle
	if err := json.Unmarshal([]byte(doc), &b); err != nil {
		return nil, fmt.Errorf("decode bundle %q: %w", id, err)
	}
	r := &Record{ID: id, CompositionBundle: b}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	r.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	return r, nil
}

// Save validates b — b.Validate()'s structural check, then
// compat.CheckBundle's live-registry check — and persists it under a new
// generated ID. principal must hold permission.PresetsManage. Mirrors
// internal/preset.Store.Save's identical "authored locally, against blocks
// the author actually has" reasoning for requiring full block compatibility
// at save time (no theme-slot gate here either — that's Import's job).
func (s *Store) Save(ctx context.Context, principal *permission.Principal, registry *blocks.Registry, b *contract.CompositionBundle) (*Record, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return nil, permission.ErrDenied
	}
	if err := b.Validate(); err != nil {
		return nil, err
	}
	result := compat.CheckBundle(b, registry, nil)
	if !result.Compatible {
		return nil, incompatibleError(result)
	}

	id := newID()
	doc, err := json.Marshal(b)
	if err != nil {
		return nil, fmt.Errorf("encode bundle %q: %w", id, err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := s.db.Exec(ctx,
		`INSERT INTO bundles (id, document, created_at, updated_at) VALUES (?, ?, ?, ?)`,
		id, string(doc), now, now); err != nil {
		return nil, fmt.Errorf("save bundle %q: %w", id, err)
	}
	return s.Get(ctx, id)
}

// Check runs the compatibility contract for the bundle saved under id
// against registry and themeRegions, without mutating anything. No
// capability gate, mirroring internal/preset.Store.Check.
func (s *Store) Check(ctx context.Context, registry *blocks.Registry, themeRegions []string, id string) (compat.Result, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return compat.Result{}, err
	}
	return compat.CheckBundle(&r.CompositionBundle, registry, themeRegions), nil
}

// Import checks the bundle saved under id against registry and
// themeRegions, and — only if compatible — merges every page's Layout into
// layoutStore (one route at a time, region-by-region, exactly like
// internal/preset.Store.Import's merge) and creates every SampleContent item
// via contentAPI.Create. Sample content creation is best-effort (see
// ImportResult.ContentErrors' doc comment); page-Layout merging is not — an
// incompatible bundle merges no pages and creates no content at all.
//
// principal must hold permission.PresetsManage for the compatibility check
// and page merges (itself re-checked by layoutStore.Save) and, for sample
// content, permission.ContentWrite (re-checked by contentAPI.Create) — an
// admin holds both in this role matrix.
func (s *Store) Import(ctx context.Context, principal *permission.Principal, registry *blocks.Registry, layoutStore *layout.Store, contentAPI *content.API, themeRegions []string, id string) (ImportResult, error) {
	if !permission.AllowsPrincipal(principal, permission.PresetsManage) {
		return ImportResult{}, permission.ErrDenied
	}
	r, err := s.Get(ctx, id)
	if err != nil {
		return ImportResult{}, err
	}
	result := compat.CheckBundle(&r.CompositionBundle, registry, themeRegions)
	if !result.Compatible {
		return ImportResult{Compat: result}, nil
	}

	routes := make([]string, 0, len(r.Pages))
	for route := range r.Pages {
		routes = append(routes, route)
	}
	sort.Strings(routes)

	out := ImportResult{Compat: result}
	for _, route := range routes {
		page := r.Pages[route]
		target, err := layoutStore.Load(ctx, route)
		if errors.Is(err, layout.ErrNotFound) {
			target = &contract.Layout{ContractVersion: contract.LayoutCompositionV1, Regions: map[string]contract.Region{}}
		} else if err != nil {
			return out, fmt.Errorf("load target layout %q: %w", route, err)
		}
		if target.Regions == nil {
			target.Regions = map[string]contract.Region{}
		}
		for name, region := range page.Regions {
			target.Regions[name] = region
		}
		if err := layoutStore.Save(ctx, principal, registry, route, target); err != nil {
			return out, fmt.Errorf("merge bundle %q page %q: %w", id, route, err)
		}
		out.ImportedPages = append(out.ImportedPages, route)
	}

	for i, item := range r.SampleContent {
		created, err := contentAPI.Create(ctx, principal, item.Type, item.Data)
		if err != nil {
			out.ContentErrors = append(out.ContentErrors, fmt.Sprintf("sample_content[%d] (%s): %s", i, item.Type, err))
			continue
		}
		out.CreatedContent = append(out.CreatedContent, ContentRef{Type: item.Type, ID: created.ID})
	}

	return out, nil
}

// incompatibleError mirrors internal/preset's identical helper — see that
// file's doc comment.
func incompatibleError(result compat.Result) error {
	var errs contract.ValidationErrors
	for _, b := range result.MissingBlocks {
		errs = append(errs, contract.ValidationError{Path: "manifest.blocks", Message: fmt.Sprintf("block type %q is not registered", b)})
	}
	for _, s := range result.MissingSlots {
		errs = append(errs, contract.ValidationError{Path: "manifest.slots", Message: fmt.Sprintf("region %q is not declared by the destination theme", s)})
	}
	if result.UnsupportedContract != "" {
		errs = append(errs, contract.ValidationError{Path: "manifest.requires_contract", Message: fmt.Sprintf("unsupported contract version %q", result.UnsupportedContract)})
	}
	if len(errs) == 0 {
		errs = append(errs, contract.ValidationError{Path: "", Message: "incompatible"})
	}
	return errs
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
