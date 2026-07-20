// Package starter is glyphux's reference server-side HTML theme (PRD §9.3:
// "V1 target is server-side templating... plus static asset delivery").
// Unlike themes/headless (which just hands a CompositionView to
// encoding/json and lets its recursive marshaling do the work), starter
// must actually turn a Layer-2 contract.Layout's blocks/slots tree into
// nested HTML — the "server-side layout rendering" this ticket is named
// for — so it owns its own recursive block-to-HTML walk (renderBlock)
// rather than reusing pkg/contract.WalkBlocks: WalkBlocks's visit
// signature returns a flat contract.ValidationErrors accumulator (built
// for Layout.Validate/blocks.ValidateLayout's "collect every violation"
// shape), not a per-block rendered value a parent block can embed inside
// its own output — a container's HTML must contain its children's
// already-rendered HTML in order, which needs a walk that returns a value,
// not one that accumulates errors. Reimplementing that shape on top of
// WalkBlocks would need a closure capturing a mutable per-node output map
// keyed by path, which is more machinery than the plain recursive function
// below and no more correct.
//
// starter uses html/template, not text/template — deliberately: a Layout's
// block Props come from a Layer-2 contract.Layout document (hand-authored
// JSON, a future builder UI, or a plugin), and pkg/contract.Layout.Validate
// performs only structural checks (non-empty Type, valid slot names) —
// there is currently no sanitization pipeline anywhere for Layer-2 block
// props (internal/content's sanitizeRichText runs only on Layer-1 content
// items, inside content.API.Create/Update/Rollback; it never touches a
// Layout document). Every prop must therefore be treated as untrusted when
// rendered into a real HTML document a browser executes. text/template
// would emit whatever a prop contains byte-for-byte, including
// "<script>"; html/template auto-escapes every interpolated value by
// default (contextually, per HTML/attribute/URL position), so a block's
// text/src/alt prop can never break out of its element and inject markup.
// There is NO exception to this — every block type's every prop,
// including "paragraph"'s "text", is auto-escaped. Rendering a prop as
// trusted HTML (e.g. treating rich text as pre-sanitized markup) is
// explicitly deferred until a real sanitization pipeline exists for
// Layer-2 block props (see this slice's tracking doc) — a natural future
// ticket, likely alongside whatever validates/sanitizes builder-authored
// layouts.
package starter

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"sort"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
)

// Theme is the starter theme.Theme implementation. It needs a *blocks.
// Registry (read-only lookups only — Get, never Register) to know which
// first-party/plugin block types it's being asked to render; this is a
// runtime dependency the theme reads from, not state it owns or mutates,
// exactly like any other read-only consumer of pkg/blocks.Registry.
type Theme struct {
	registry *blocks.Registry
}

// New returns a starter Theme that renders block types looked up in
// registry.
func New(registry *blocks.Registry) *Theme {
	return &Theme{registry: registry}
}

// Name identifies this theme as "starter".
func (t *Theme) Name() string { return "starter" }

// declaredRegions is starter's own compatibility-contract declaration (PRD
// §9.2, §13.3) of the regions it is designed around, matching the four
// example region names its own package doc discusses. Render itself stays
// generically permissive about any region name present in a given Layout
// (see pageTemplate's doc comment) for forward-compatibility with a Layout
// authored against a not-yet-declared region — Regions() is what a
// Composition Preset/Bundle's Manifest.Slots is checked against at import
// time, a stricter, explainable-up-front declaration than "whatever happens
// to render."
var declaredRegions = []string{"header", "main", "sidebar", "footer"}

// Regions returns starter's declared region names — see declaredRegions.
func (t *Theme) Regions() []string { return declaredRegions }

// pageTemplate is the outer HTML document every route renders into: one
// <section> per Layer-2 region (in stable, sorted-by-name order — Layout.
// Regions is a Go map, which has no iteration order of its own, and
// rendered HTML output must be deterministic), each containing that
// region's already-rendered block HTML in layout order.
var pageTemplate = template.Must(template.New("page").Parse(`<!DOCTYPE html>
<html>
<head><meta charset="utf-8"><title>{{.Title}}</title></head>
<body>
{{range .Regions}}<section data-region="{{.Name}}">{{range .Blocks}}{{.}}
{{end}}</section>
{{end}}</body>
</html>
`))

// blockTemplates holds one named template per renderable first-party
// block type (PRD Ticket P4.3: "render at least the four first-party
// blocks"). Each block type's template receives that block's own Props
// (plus, for "container", a computed "children" key holding its nested
// slot blocks' already-rendered HTML, in order) as a map[string]any.
var blockTemplates = template.Must(template.New("blocks").Parse(`
{{define "heading"}}<h{{.level}}>{{.text}}</h{{.level}}>{{end}}
{{define "paragraph"}}<p>{{.text}}</p>{{end}}
{{define "image"}}<img src="{{.src}}" alt="{{.alt}}">{{end}}
{{define "container"}}<div class="container">{{range .children}}{{.}}
{{end}}</div>{{end}}
`))

// Render walks view's Layer-2 Layout (nil Layout is an error: starter has
// nothing to render without one, unlike headless which can emit an
// items-only document) and produces a full HTML document. contentType is
// always "text/html; charset=utf-8".
func (t *Theme) Render(ctx context.Context, view theme.CompositionView) ([]byte, string, error) {
	layout := view.Layout()
	if layout == nil {
		return nil, "", fmt.Errorf("starter: cannot render a view with no Layer-2 layout")
	}

	title := ""
	if item := view.Item(); item != nil {
		if v, ok := item.Data["title"].(string); ok {
			title = v
		}
	}

	regionNames := make([]string, 0, len(layout.Regions))
	for name := range layout.Regions {
		regionNames = append(regionNames, name)
	}
	sort.Strings(regionNames)

	type regionData struct {
		Name   string
		Blocks []template.HTML
	}
	regions := make([]regionData, 0, len(regionNames))
	for _, name := range regionNames {
		region := layout.Regions[name]
		rd := regionData{Name: name}
		for _, b := range region.Blocks {
			html, err := t.renderBlock(b)
			if err != nil {
				return nil, "", err
			}
			rd.Blocks = append(rd.Blocks, html)
		}
		regions = append(regions, rd)
	}

	var buf bytes.Buffer
	if err := pageTemplate.Execute(&buf, struct {
		Title   string
		Regions []regionData
	}{Title: title, Regions: regions}); err != nil {
		return nil, "", fmt.Errorf("starter: render page: %w", err)
	}
	return buf.Bytes(), "text/html; charset=utf-8", nil
}

// renderBlock renders one block, and — for a container-shaped block — its
// nested slot blocks first (in order), embedding their rendered HTML into
// its own output. This is the recursive walk proving the "container with
// nested blocks renders its children inside it, in order" behavior this
// ticket requires. A leaf block (no Slots) simply recurses zero times.
func (t *Theme) renderBlock(b contract.Block) (template.HTML, error) {
	if _, ok := t.registry.Get(b.Type); !ok {
		return "", fmt.Errorf("starter: block type %q is not registered", b.Type)
	}
	tmpl := blockTemplates.Lookup(b.Type)
	if tmpl == nil {
		return "", fmt.Errorf("starter: no HTML template for block type %q", b.Type)
	}

	data := make(map[string]any, len(b.Props)+1)
	for k, v := range b.Props {
		data[k] = v
	}
	switch b.Type {
	case "heading":
		if _, ok := data["level"]; !ok {
			data["level"] = 1
		}
	case "container":
		children := make([]template.HTML, 0, len(b.Slots["content"]))
		for _, child := range b.Slots["content"] {
			html, err := t.renderBlock(child)
			if err != nil {
				return "", err
			}
			children = append(children, html)
		}
		data["children"] = children
	}

	var buf bytes.Buffer
	if err := blockTemplates.ExecuteTemplate(&buf, b.Type, data); err != nil {
		return "", fmt.Errorf("starter: render block %q: %w", b.Type, err)
	}
	return template.HTML(buf.String()), nil
}
