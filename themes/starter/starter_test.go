package starter_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
	"github.com/glyphux/glyphux/themes/starter"
)

var adminPrincipal = &permission.Principal{Role: permission.RoleAdmin}

func testContentAPI(t *testing.T) *content.API {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, composition.Migrations...), content.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"page": {
				Fields: map[string]contract.Field{
					"title": {Type: contract.FieldString, Required: true},
				},
			},
		},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}
	return content.NewAPI(comps, content.NewStore(d))
}

func testRegistry(t *testing.T) *blocks.Registry {
	t.Helper()
	r := blocks.New()
	if err := firstparty.RegisterAll(r); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNameIsStarter(t *testing.T) {
	if got := starter.New(testRegistry(t)).Name(); got != "starter" {
		t.Fatalf("Name() = %q, want %q", got, "starter")
	}
}

func TestRenderErrorsWithoutLayout(t *testing.T) {
	th := starter.New(testRegistry(t))
	view := theme.NewCompositionView(nil, nil)
	if _, _, err := th.Render(context.Background(), view); err == nil {
		t.Fatal("Render with no Layout: expected error, got nil")
	}
}

func TestRenderProducesHTMLForAllFourFirstPartyBlocksIncludingNestedSlot(t *testing.T) {
	ctx := context.Background()
	api := testContentAPI(t)
	item, err := api.Create(ctx, adminPrincipal, "page", map[string]any{"title": "My Page"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	registry := testRegistry(t)
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {
				Blocks: []contract.Block{
					{Type: "heading", Props: map[string]any{"text": "Hello", "level": 2}},
					{
						Type: "container",
						Slots: map[string][]contract.Block{
							"content": {
								{Type: "paragraph", Props: map[string]any{"text": "Body copy."}},
								{Type: "image", Props: map[string]any{"src": "/a.png", "alt": "An image"}},
							},
						},
					},
				},
			},
		},
	}
	if err := layout.Validate(); err != nil {
		t.Fatalf("layout.Validate: %v", err)
	}
	if err := blocks.ValidateLayout(layout, registry); err != nil {
		t.Fatalf("blocks.ValidateLayout: %v", err)
	}

	th := starter.New(registry)
	view := theme.NewCompositionView([]*content.Item{item}, layout)
	out, contentType, err := th.Render(ctx, view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if contentType != "text/html; charset=utf-8" {
		t.Fatalf("contentType = %q", contentType)
	}
	html := string(out)

	if !strings.Contains(html, "<title>My Page</title>") {
		t.Fatalf("missing page title, got:\n%s", html)
	}
	if !strings.Contains(html, "<h2>Hello</h2>") {
		t.Fatalf("missing rendered heading, got:\n%s", html)
	}
	if !strings.Contains(html, `<div class="container">`) {
		t.Fatalf("missing rendered container, got:\n%s", html)
	}
	if !strings.Contains(html, "<p>Body copy.</p>") {
		t.Fatalf("missing rendered paragraph, got:\n%s", html)
	}
	if !strings.Contains(html, `<img src="/a.png" alt="An image">`) {
		t.Fatalf("missing rendered image, got:\n%s", html)
	}

	// The recursive-slot case: the container's rendered <div> must contain
	// its children's HTML nested INSIDE it, in order (paragraph before
	// image), not appended as siblings elsewhere in the document.
	containerIdx := strings.Index(html, `<div class="container">`)
	containerEnd := strings.Index(html[containerIdx:], "</div>") + containerIdx
	inner := html[containerIdx:containerEnd]
	pIdx := strings.Index(inner, "<p>Body copy.</p>")
	imgIdx := strings.Index(inner, `<img src="/a.png"`)
	if pIdx == -1 || imgIdx == -1 || pIdx > imgIdx {
		t.Fatalf("expected paragraph then image nested inside container div, got inner:\n%s", inner)
	}
}

func TestRenderAutoEscapesUntrustedHeadingTextButNotRichTextParagraph(t *testing.T) {
	registry := testRegistry(t)
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {
				Blocks: []contract.Block{
					{Type: "heading", Props: map[string]any{"text": `<script>alert(1)</script>`}},
					{Type: "paragraph", Props: map[string]any{"text": `<strong>bold</strong>`}},
				},
			},
		},
	}
	th := starter.New(registry)
	view := theme.NewCompositionView(nil, layout)
	out, _, err := th.Render(context.Background(), view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	html := string(out)

	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Fatalf("heading text was not escaped, got:\n%s", html)
	}
	if !strings.Contains(html, "&lt;script&gt;alert(1)&lt;/script&gt;") {
		t.Fatalf("expected escaped heading text, got:\n%s", html)
	}
	// Rich text (paragraph) is intentionally rendered unescaped: it is
	// sanitized server-side at write time (internal/content's
	// sanitizeRichText), not at render time.
	if !strings.Contains(html, "<strong>bold</strong>") {
		t.Fatalf("expected unescaped rich-text paragraph body, got:\n%s", html)
	}
}

func TestRenderErrorsOnUnknownBlockType(t *testing.T) {
	registry := blocks.New() // deliberately empty
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{{Type: "does-not-exist"}}},
		},
	}
	th := starter.New(registry)
	view := theme.NewCompositionView(nil, layout)
	if _, _, err := th.Render(context.Background(), view); err == nil {
		t.Fatal("Render with unregistered block type: expected error, got nil")
	}
}

func TestRenderIsDeterministicAcrossRegionsRegardlessOfMapOrder(t *testing.T) {
	registry := testRegistry(t)
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"zzz-footer": {Blocks: []contract.Block{{Type: "heading", Props: map[string]any{"text": "Footer"}}}},
			"aaa-header": {Blocks: []contract.Block{{Type: "heading", Props: map[string]any{"text": "Header"}}}},
		},
	}
	th := starter.New(registry)
	view := theme.NewCompositionView(nil, layout)

	var prev string
	for i := 0; i < 5; i++ {
		out, _, err := th.Render(context.Background(), view)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		if i > 0 && string(out) != prev {
			t.Fatalf("Render output not deterministic across runs")
		}
		prev = string(out)
	}
	headerIdx := strings.Index(prev, "Header")
	footerIdx := strings.Index(prev, "Footer")
	if headerIdx == -1 || footerIdx == -1 || headerIdx > footerIdx {
		t.Fatalf("expected aaa-header region before zzz-footer region (sorted region order), got:\n%s", prev)
	}
}
