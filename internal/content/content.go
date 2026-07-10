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

// Item is a single piece of content: a typed, identified JSON document with
// lifecycle timestamps.
type Item struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// Create validates data against the declared content type and persists a new
// item, returning it with a generated id and timestamps.
func (a *API) Create(ctx context.Context, typeName string, data map[string]any) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	if err := validate(typeName, ct, data); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	item := &Item{ID: newID(), Type: typeName, Data: data, CreatedAt: now, UpdatedAt: now}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode content data: %w", err)
	}
	if err := a.items.insert(ctx, record{
		ID: item.ID, Type: typeName, Data: string(encoded),
		CreatedAt: now, UpdatedAt: now,
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

// Update validates data against the declared type and replaces the item's data,
// returning ErrNotFound if the item does not exist.
func (a *API) Update(ctx context.Context, typeName, id string, data map[string]any) (*Item, error) {
	ct, err := a.contentType(ctx, typeName)
	if err != nil {
		return nil, err
	}
	if err := validate(typeName, ct, data); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("encode content data: %w", err)
	}
	now := time.Now().UTC()
	if err := a.items.update(ctx, typeName, id, string(encoded), now); err != nil {
		return nil, err
	}
	return a.Get(ctx, typeName, id)
}

// Delete removes an item, returning ErrNotFound if it does not exist.
func (a *API) Delete(ctx context.Context, typeName, id string) error {
	if _, err := a.contentType(ctx, typeName); err != nil {
		return err
	}
	return a.items.delete(ctx, typeName, id)
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
	return &Item{ID: r.ID, Type: r.Type, Data: data, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}, nil
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
