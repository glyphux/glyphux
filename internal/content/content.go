// Package content is the kernel content engine. Phase 0 scope (slice 0.4):
// one trivial domain-API read — "ping" — exercised through the composition
// contract, proving the Communication Law plumbing end to end:
//
//	client → HTTP transport → domain API → kernel → adapter
//
// The full content engine (types, CRUD, relations, versioning) is Phase 1.
package content

import (
	"context"
	"fmt"
	"sort"

	"github.com/glyphux/glyphux/internal/composition"
)

// API is the content domain API — the only sanctioned way for clients to
// touch content state. It never exposes the database (Principle 4).
type API struct {
	compositions *composition.Store
}

// NewAPI wires the domain API to the kernel stores.
func NewAPI(store *composition.Store) *API {
	return &API{compositions: store}
}

// Ping is the Phase-0 contract-driven read: it resolves the current
// composition and reports what content types it declares. Trivial by design —
// its purpose is to prove that a client request flows through the domain-API
// boundary and the contract, not around them.
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
