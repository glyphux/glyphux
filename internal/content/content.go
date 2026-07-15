// Package content is the kernel content engine and the primary domain-API
// surface of the headless product (Phase 1). Clients touch content state only
// through this API — never the database (Principle 4, Communication Law §5.2).
//
// Content types and fields are declared in the composition contract (the single
// source of truth); items are stored as JSON documents validated against the
// declared type on every write.
package content

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/pkg/contract"
)

// API is the content domain API — the only sanctioned way for clients to touch
// content state. It never exposes the database (Principle 4).
type API struct {
	compositions *composition.Store
	items        *Store
}

// NewAPI wires the domain API to the kernel stores.
func NewAPI(comps *composition.Store, items *Store) *API {
	return &API{compositions: comps, items: items}
}

// Status values for an item's publication lifecycle (slice 1.5).
const (
	StatusDraft     = "draft"
	StatusPublished = "published"
)

// Item is a single piece of content: a typed, identified JSON document with a
// publication status, a version number, and lifecycle timestamps.
type Item struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Status    string         `json:"status"`
	Version   int            `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Version is one immutable historical snapshot of an item.
type Version struct {
	Version   int            `json:"version"`
	Data      map[string]any `json:"data"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

// Create validates data against the declared content type and persists a new
// item as a draft at version 1, returning it with a generated id and
// timestamps.
func (a *API) Create(ctx context.Context, typeName string, data map[string]any) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	if err := validate(typeName, ct, data); err != nil {
		return nil, err
	}
	if err := a.checkRelations(ctx, typeName, ct, data); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item := &Item{
		ID: newID(), Type: typeName, Data: data,
		Status: StatusDraft, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode content data: %w", err)
	}
	if err := a.items.insert(ctx, record{
		ID: item.ID, Type: typeName, Data: string(encoded),
		Status: StatusDraft, Version: 1,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return nil, err
	}
	if err := a.items.insertVersion(ctx, item.ID, versionRecord{
		Version: 1, Type: typeName, Data: string(encoded), Status: StatusDraft, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return item, nil
}

// Get returns the item of the given type and id, or ErrNotFound.
func (a *API) Get(ctx context.Context, typeName, id string) (*Item, error) {
	r, err := a.items.getByID(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	return recordToItem(r)
}

// List returns every item of the given type, oldest first. The type must be
// declared in the composition.
func (a *API) List(ctx context.Context, typeName string) ([]*Item, error) {
	if _, err := a.contentType(ctx, typeName); err != nil {
		return nil, err
	}
	records, err := a.items.listByType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	items := make([]*Item, 0, len(records))
	for _, r := range records {
		item, err := recordToItem(r)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

// GetPublished returns the item like Get, but reports ErrNotFound if it is
// not published — drafts are invisible to public/unprivileged reads (slice
// 1.5 fix: publish state must actually gate visibility, not just be a label).
func (a *API) GetPublished(ctx context.Context, typeName, id string) (*Item, error) {
	item, err := a.Get(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	if item.Status != StatusPublished {
		return nil, ErrNotFound
	}
	return item, nil
}

// ListPublished returns every published item of the given type, oldest
// first, excluding drafts.
func (a *API) ListPublished(ctx context.Context, typeName string) ([]*Item, error) {
	items, err := a.List(ctx, typeName)
	if err != nil {
		return nil, err
	}
	published := items[:0]
	for _, item := range items {
		if item.Status == StatusPublished {
			published = append(published, item)
		}
	}
	return published, nil
}

// GetLocalizedPublished composes GetPublished with locale resolution, like
// GetLocalized does for Get.
func (a *API) GetLocalizedPublished(ctx context.Context, typeName, id, locale string) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	item, err := a.GetPublished(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	resolveLocale(item, ct, locale)
	return item, nil
}

// ListLocalizedPublished composes ListPublished with locale resolution, like
// ListLocalized does for List.
func (a *API) ListLocalizedPublished(ctx context.Context, typeName, locale string) ([]*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	items, err := a.ListPublished(ctx, typeName)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		resolveLocale(item, ct, locale)
	}
	return items, nil
}

// GetLocalized returns the item like Get, but resolves every localized field
// to a single value for locale: the value at that locale if present, else an
// arbitrary available locale as a fallback rather than leaving the field
// empty. Non-localized fields pass through unchanged.
func (a *API) GetLocalized(ctx context.Context, typeName, id, locale string) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	item, err := a.Get(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	resolveLocale(item, ct, locale)
	return item, nil
}

// ListLocalized returns every item of the given type, resolved to locale like
// GetLocalized.
func (a *API) ListLocalized(ctx context.Context, typeName, locale string) ([]*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	items, err := a.List(ctx, typeName)
	if err != nil {
		return nil, err
	}
	for _, item := range items {
		resolveLocale(item, ct, locale)
	}
	return items, nil
}

// resolveLocale flattens every localized field on item in place: the value at
// locale if present, else an arbitrary available locale.
func resolveLocale(item *Item, ct contract.ContentType, locale string) {
	for name, f := range ct.Fields {
		if !f.Localized {
			continue
		}
		locales, ok := item.Data[name].(map[string]any)
		if !ok {
			continue
		}
		if v, ok := locales[locale]; ok {
			item.Data[name] = v
			continue
		}
		for _, v := range locales {
			item.Data[name] = v
			break
		}
	}
}

// Update validates data against the declared type, replaces the item's data,
// and records a new version snapshot. Status is left unchanged. Returns
// ErrNotFound if the item does not exist.
func (a *API) Update(ctx context.Context, typeName, id string, data map[string]any) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	if err := validate(typeName, ct, data); err != nil {
		return nil, err
	}
	if err := a.checkRelations(ctx, typeName, ct, data); err != nil {
		return nil, err
	}
	existing, err := a.items.getByID(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode content data: %w", err)
	}
	now := time.Now().UTC()
	nextVersion := existing.Version + 1
	if err := a.items.setData(ctx, typeName, id, string(encoded), nextVersion, now); err != nil {
		return nil, err
	}
	if err := a.items.insertVersion(ctx, id, versionRecord{
		Version: nextVersion, Type: typeName, Data: string(encoded), Status: existing.Status, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return a.Get(ctx, typeName, id)
}

// Delete removes an item, returning ErrNotFound if it does not exist.
// CountItems reports how many items of typeName exist, regardless of their
// publish status. It deliberately does not require typeName to be a
// currently-declared content type, so callers (e.g. the content-type
// deletion guard) can check for orphaned items even after a type has been
// removed from the composition.
func (a *API) CountItems(ctx context.Context, typeName string) (int, error) {
	return a.items.countByType(ctx, typeName)
}

func (a *API) Delete(ctx context.Context, typeName, id string) error {
	if _, err := a.contentType(ctx, typeName); err != nil {
		return err
	}
	return a.items.delete(ctx, typeName, id)
}

// Publish marks an item as published, making it the item's live status.
// Returns ErrNotFound if the item does not exist.
func (a *API) Publish(ctx context.Context, typeName, id string) (*Item, error) {
	return a.setStatus(ctx, typeName, id, StatusPublished)
}

// Unpublish reverts a published item to draft. Returns ErrNotFound if the
// item does not exist.
func (a *API) Unpublish(ctx context.Context, typeName, id string) (*Item, error) {
	return a.setStatus(ctx, typeName, id, StatusDraft)
}

func (a *API) setStatus(ctx context.Context, typeName, id, status string) (*Item, error) {
	if _, err := a.contentType(ctx, typeName); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := a.items.setStatus(ctx, typeName, id, status, now); err != nil {
		return nil, err
	}
	return a.Get(ctx, typeName, id)
}

// ListVersions returns an item's full version history, oldest first. Returns
// ErrNotFound if the item does not exist.
func (a *API) ListVersions(ctx context.Context, typeName, id string) ([]*Version, error) {
	if _, err := a.items.getByID(ctx, typeName, id); err != nil {
		return nil, err
	}
	records, err := a.items.listVersions(ctx, id)
	if err != nil {
		return nil, err
	}
	versions := make([]*Version, 0, len(records))
	for _, vr := range records {
		v, err := recordToVersion(vr)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// Rollback restores an item's data to an earlier version, validating it
// against the current content type and recording the restore as a new
// version. Status is left unchanged. Returns ErrNotFound if the item or the
// requested version does not exist.
func (a *API) Rollback(ctx context.Context, typeName, id string, version int) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	existing, err := a.items.getByID(ctx, typeName, id)
	if err != nil {
		return nil, err
	}
	target, err := a.items.getVersion(ctx, id, version)
	if err != nil {
		return nil, err
	}
	var data map[string]any
	if err := json.Unmarshal([]byte(target.Data), &data); err != nil {
		return nil, fmt.Errorf("decode content data: %w", err)
	}
	if err := validate(typeName, ct, data); err != nil {
		return nil, err
	}
	if err := a.checkRelations(ctx, typeName, ct, data); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	nextVersion := existing.Version + 1
	if err := a.items.setData(ctx, typeName, id, target.Data, nextVersion, now); err != nil {
		return nil, err
	}
	if err := a.items.insertVersion(ctx, id, versionRecord{
		Version: nextVersion, Type: typeName, Data: target.Data, Status: existing.Status, CreatedAt: now,
	}); err != nil {
		return nil, err
	}
	return a.Get(ctx, typeName, id)
}

// checkRelations verifies that every present relation field references an
// existing item of its declared target type. Referential integrity is a
// Layer-1 concern enforced at the domain boundary, not left to the storage
// adapter. Kind (string) is already checked structurally by validate.
func (a *API) checkRelations(ctx context.Context, typeName string, ct contract.ContentType, data map[string]any) error {
	var issues []string
	for name, f := range ct.Fields {
		if f.Type != contract.FieldRelation {
			continue
		}
		v, ok := data[name]
		if !ok || v == nil {
			continue
		}
		target, ok := v.(string)
		if !ok {
			continue // wrong kind already reported by validate
		}
		exists, err := a.items.exists(ctx, f.To, target)
		if err != nil {
			return err
		}
		if !exists {
			issues = append(issues, fmt.Sprintf("relation %q references missing %s %q", name, f.To, target))
		}
	}
	if len(issues) > 0 {
		sort.Strings(issues)
		return &ValidationError{Type: typeName, Issues: issues}
	}
	return nil
}

// contentType resolves a declared content type from the composition, or an
// error if it is not declared.
func (a *API) contentType(ctx context.Context, typeName string) (contract.ContentType, error) {
	comp, err := a.compositions.Load(ctx)
	if err != nil {
		return contract.ContentType{}, fmt.Errorf("resolve composition: %w", err)
	}
	ct, ok := comp.ContentTypes[typeName]
	if !ok {
		return contract.ContentType{}, fmt.Errorf("%w: %q", ErrUnknownType, typeName)
	}
	return ct, nil
}

func recordToItem(r record) (*Item, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(r.Data), &data); err != nil {
		return nil, fmt.Errorf("decode content data: %w", err)
	}
	return &Item{
		ID: r.ID, Type: r.Type, Data: data,
		Status: r.Status, Version: r.Version,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}, nil
}

func recordToVersion(vr versionRecord) (*Version, error) {
	var data map[string]any
	if err := json.Unmarshal([]byte(vr.Data), &data); err != nil {
		return nil, fmt.Errorf("decode content version data: %w", err)
	}
	return &Version{Version: vr.Version, Data: data, Status: vr.Status, CreatedAt: vr.CreatedAt}, nil
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// Ping is the Phase-0 contract-driven read: it resolves the current
// composition and reports what content types it declares.
type Ping struct {
	ContractVersion string   `json:"contract_version"`
	Site            string   `json:"site"`
	ContentTypes    []string `json:"content_types"`
}

// GetPing serves the ping read from the resolved composition.
func (a *API) GetPing(ctx context.Context) (*Ping, error) {
	comp, err := a.compositions.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve composition: %w", err)
	}
	types := make([]string, 0, len(comp.ContentTypes))
	for name := range comp.ContentTypes {
		types = append(types, name)
	}
	sort.Strings(types)
	return &Ping{
		ContractVersion: string(comp.ContractVersion),
		Site:            comp.Site.Name,
		ContentTypes:    types,
	}, nil
}
