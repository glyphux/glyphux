// Package headless is glyphux's built-in default theme (PRD §9.3: "the
// platform must be fully usable headlessly with the built-in headless
// 'theme' that emits JSON only"). It proves that claim for real: it emits
// a pkg/theme.CompositionView as JSON, nothing else.
//
// headless needs no per-block-type dispatch and no bespoke tree-walk of
// its own the way themes/starter's HTML rendering does — encoding/json's
// ordinary recursive marshaling already walks a contract.Layout's
// regions/blocks/slots tree for free, since every level (Region, Block,
// and Block.Slots' nested []Block) already carries its own `json:"..."`
// tags. Reimplementing that walk here would be pure duplication of what
// encoding/json already does correctly; headless's whole job is deciding
// what shape to hand to json.Marshal, not how to recurse.
package headless

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
)

// Theme is the headless theme.Theme implementation.
type Theme struct{}

// New returns a headless Theme.
func New() *Theme { return &Theme{} }

// Name identifies this theme as "headless".
func (t *Theme) Name() string { return "headless" }

// document is headless's wire shape: a plain, JSON-tagged projection of a
// CompositionView. It is a distinct type from CompositionView itself
// (rather than reusing CompositionView as-is) because CompositionView's
// fields are deliberately unexported — see pkg/theme's doc comment on why
// — so this is the one exported shape that turns a view's Items/Layout
// into JSON. Items is typed `any` (holding whatever []*content.Item
// CompositionView.Items() returns) rather than naming internal/content.Item
// directly: this package deliberately imports nothing but pkg/contract and
// pkg/theme — the same public surface a third-party theme is restricted to
// (internal/boundary's plugin-kernel-import invariant, PRD §17.2) — proving
// even the built-in default theme needs no special access to kernel
// internals to do its job. Items is never emitted as a Go nil slice (which
// encodes as JSON null); Render normalizes it to an empty slice so headless
// clients always see a JSON array, never null, for a field documented as a
// list.
type document struct {
	Items  any              `json:"items"`
	Layout *contract.Layout `json:"layout,omitempty"`
}

// Render emits view as JSON. contentType is always "application/json".
func (t *Theme) Render(ctx context.Context, view theme.CompositionView) ([]byte, string, error) {
	items := view.Items()
	var itemsJSON any = items
	if len(items) == 0 {
		itemsJSON = []any{}
	}
	doc := document{Items: itemsJSON, Layout: view.Layout()}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, "", fmt.Errorf("headless: encode composition view: %w", err)
	}
	return out, "application/json", nil
}
