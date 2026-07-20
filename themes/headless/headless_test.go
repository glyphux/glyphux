package headless_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/blocks/firstparty"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
	"github.com/glyphux/glyphux/themes/headless"
)

var adminPrincipal = &permission.Principal{Role: permission.RoleAdmin}

func TestRegionsDeclaresNoRestriction(t *testing.T) {
	if got := headless.New().Regions(); got != nil {
		t.Fatalf("Regions() = %v, want nil (headless declares no region restriction)", got)
	}
}

// testContentAPI wires a real content.API over a fresh SQLite DB, exactly
// like capabilities/forms's tests do — this ticket's real dependency, not
// a mock.
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

// realLayout builds a real Layer-2 layout using real first-party block
// types, including the recursive container/slot case.
func realLayout(t *testing.T) *contract.Layout {
	t.Helper()
	registry := blocks.New()
	if err := firstparty.RegisterAll(registry); err != nil {
		t.Fatal(err)
	}
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {
				Blocks: []contract.Block{
					{Type: "heading", Props: map[string]any{"text": "Hello", "level": float64(1)}},
					{
						Type: "container",
						Slots: map[string][]contract.Block{
							"content": {
								{Type: "paragraph", Props: map[string]any{"text": "Body copy."}},
								{Type: "image", Props: map[string]any{"src": "/a.png", "alt": "A"}},
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
	return layout
}

func TestRenderEmitsJSONThatRoundTripsItemAndLayout(t *testing.T) {
	ctx := context.Background()
	api := testContentAPI(t)
	item, err := api.Create(ctx, adminPrincipal, "page", map[string]any{"title": "Home"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	layout := realLayout(t)

	view := theme.NewCompositionView([]*content.Item{item}, layout)
	th := headless.New()

	if got := th.Name(); got != "headless" {
		t.Fatalf("Name() = %q, want %q", got, "headless")
	}

	out, contentType, err := th.Render(ctx, view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	if contentType != "application/json" {
		t.Fatalf("contentType = %q, want application/json", contentType)
	}

	var decoded struct {
		Items []struct {
			ID   string         `json:"id"`
			Type string         `json:"type"`
			Data map[string]any `json:"data"`
		} `json:"items"`
		Layout struct {
			ContractVersion string `json:"contract_version"`
			Regions         map[string]struct {
				Blocks []struct {
					Type  string         `json:"type"`
					Props map[string]any `json:"props"`
					Slots map[string][]struct {
						Type  string         `json:"type"`
						Props map[string]any `json:"props"`
					} `json:"slots"`
				} `json:"blocks"`
			} `json:"regions"`
		} `json:"layout"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal headless output: %v (raw: %s)", err, out)
	}

	if len(decoded.Items) != 1 || decoded.Items[0].ID != item.ID || decoded.Items[0].Data["title"] != "Home" {
		t.Fatalf("decoded items = %+v, want one item %q with title Home", decoded.Items, item.ID)
	}
	if decoded.Layout.ContractVersion != string(contract.LayoutCompositionV1) {
		t.Fatalf("decoded layout contract version = %q", decoded.Layout.ContractVersion)
	}
	main, ok := decoded.Layout.Regions["main"]
	if !ok || len(main.Blocks) != 2 {
		t.Fatalf("decoded regions.main = %+v, want 2 blocks", main)
	}
	if main.Blocks[0].Type != "heading" || main.Blocks[0].Props["text"] != "Hello" {
		t.Fatalf("decoded blocks[0] = %+v", main.Blocks[0])
	}
	container := main.Blocks[1]
	if container.Type != "container" {
		t.Fatalf("decoded blocks[1].Type = %q, want container", container.Type)
	}
	nested, ok := container.Slots["content"]
	if !ok || len(nested) != 2 || nested[0].Type != "paragraph" || nested[1].Type != "image" {
		t.Fatalf("decoded container slots = %+v, want [paragraph image]", container.Slots)
	}
}

func TestRenderEmitsEmptyItemsArrayNotNullWhenNoItems(t *testing.T) {
	th := headless.New()
	view := theme.NewCompositionView(nil, nil)
	out, _, err := th.Render(context.Background(), view)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded struct {
		Items  json.RawMessage `json:"items"`
		Layout json.RawMessage `json:"layout"`
	}
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(decoded.Items) != "[]" {
		t.Fatalf("items = %s, want []", decoded.Items)
	}
	if decoded.Layout != nil {
		t.Fatalf("layout = %s, want omitted (nil)", decoded.Layout)
	}
}
