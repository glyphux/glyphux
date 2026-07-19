// Package theme is the public rendering contract (PRD §9.3: "the typed
// interface between the resolved composition and a theme"). Rendering is
// just another client of the composition contract (Principle 3) — a theme
// receives a read-only, typed CompositionView and produces rendered output,
// nothing else. No theme system existed before this slice (Phase 1
// deliberately shipped "no renderer, no builder"); this package, plus the
// two built-in themes in themes/headless and themes/starter, is that
// system's foundation.
package theme

import (
	"context"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/contract"
)

// Theme is the interface every theme implements — the boundary PRD §9.1's
// hard law ("themes never mutate composition") is drawn around. A Theme:
//   - declares its own identity (Name) so a host can select/log/attribute
//     output to it;
//   - renders a CompositionView into output bytes plus the output's MIME
//     type (a theme decides its own output shape — JSON, HTML, or anything
//     else — so the content type travels with the bytes rather than being
//     assumed by the caller).
//
// Render takes a context (for cancellation/timeouts/tracing, the same
// reason every other domain-API method in this codebase takes one — see
// internal/content.API) and a CompositionView (read-only by construction,
// see this package's CompositionView doc comment). It returns an error for
// any rendering failure (e.g. a Layer-2 Layout referencing a block type the
// theme has no template for); Render must not partially write output and
// return an error — callers may treat a non-nil error as "output is
// unusable."
//
// There is deliberately no method on this interface (or on
// CompositionView) that writes anything back — no Save, no Update, no
// "on change" callback. PRD §9.1: "if a theme needs something changed, it
// emits an event and the platform decides" — that emission, if a theme
// ever needs it, is a plugin/event-bus concern (pkg/sdk), not part of the
// rendering contract itself. A theme that wants to mutate state is, per
// the PRD's own words, "a plugin wearing a costume," and this interface
// refuses to let it happen by simply not exposing any way to.
type Theme interface {
	// Name returns the theme's identity (e.g. "headless", "starter").
	Name() string

	// Render produces output for view. contentType is the MIME type of
	// output (e.g. "application/json", "text/html; charset=utf-8").
	Render(ctx context.Context, view CompositionView) (output []byte, contentType string, err error)
}

// CompositionView is a read-only, typed view of the resolved composition
// for one route: the Layer-1 content item(s) relevant to that route, plus
// an optional Layer-2 contract.Layout (nil if the route has no Layer-2
// layout at all — Layer 2 is additive to Layer 1, never mandatory, PRD
// §14, so a theme must be able to render Layer-1-only content with no
// layout).
//
// CompositionView carries NO mutation method anywhere — not a setter, not
// a "helper" that quietly writes through to storage. Its fields are
// unexported precisely so that a theme (or any other consumer of this
// package) cannot reach into a CompositionView and add one later without
// it being an obvious, reviewable change to this file: PRD §9.1's hard law
// ("themes never mutate composition") is enforced structurally, not just
// by convention. The only way to build one is NewCompositionView; the only
// way to read one is Items()/Item()/Layout().
//
// Reused type, not reinvented: Items holds *content.Item, the exact
// Layer-1 read-model type internal/content.API already returns from every
// read path (Get, GetPublished, List, ...) — a theme sees precisely what
// any other read-only consumer of content sees, no parallel "view" copy of
// the same fields.
type CompositionView struct {
	items  []*content.Item
	layout *contract.Layout
}

// NewCompositionView builds a CompositionView for one route. items is the
// resolved Layer-1 content item(s) relevant to the route — a single-item
// route (e.g. rendering one page or post) passes a one-element slice; a
// list route (e.g. a blog index) passes several, in the order the caller
// wants them rendered. layout is the route's Layer-2 contract.Layout, or
// nil if the route has no Layer-2 composition at all.
func NewCompositionView(items []*content.Item, layout *contract.Layout) CompositionView {
	return CompositionView{items: items, layout: layout}
}

// Items returns the view's resolved Layer-1 content items, in the order
// given to NewCompositionView. The returned slice and its *content.Item
// elements are the same read-only values any other content.API read
// returns — a theme must not mutate them to affect anything beyond its own
// local copy; nothing reads them back afterward.
func (v CompositionView) Items() []*content.Item { return v.items }

// Item returns the view's first content item, or nil if the view has no
// items — a convenience for the common single-item-route case so a theme
// need not slice-index Items() itself for the ordinary "one page, one
// item" route.
func (v CompositionView) Item() *content.Item {
	if len(v.items) == 0 {
		return nil
	}
	return v.items[0]
}

// Layout returns the view's Layer-2 contract.Layout, or nil if the route
// has no Layer-2 composition.
func (v CompositionView) Layout() *contract.Layout { return v.layout }
