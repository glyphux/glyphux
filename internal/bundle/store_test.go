package bundle_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/bundle"
	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

var admin = &permission.Principal{Role: permission.RoleAdmin}

// harness bundles everything a bundle.Store.Import needs: its own store, a
// layout store to merge pages into, and a content API (over a composition
// declaring an "article" content type) to create sample content against.
type harness struct {
	bundles *bundle.Store
	layouts *layout.Store
	content *content.API
	reg     *blocks.Registry
}

func testHarness(t *testing.T) harness {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append(append([]db.Migration{}, bundle.Migrations...), layout.Migrations...), composition.Migrations...)
	migs = append(migs, content.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}

	comps := composition.NewStore(d)
	comp := &contract.Composition{
		ContractVersion: contract.ContentCompositionV0,
		Site:            contract.Site{Name: "Test"},
		ContentTypes: map[string]contract.ContentType{
			"article": {Fields: map[string]contract.Field{
				"title": {Type: contract.FieldString, Required: true},
			}},
		},
	}
	if err := comps.Save(context.Background(), nil, comp); err != nil {
		t.Fatal(err)
	}

	reg := blocks.New()
	if err := reg.Register(blocks.Definition{Name: "hero", DisplayName: "Hero"}); err != nil {
		t.Fatal(err)
	}

	return harness{
		bundles: bundle.NewStore(d),
		layouts: layout.NewStore(d),
		content: content.NewAPI(comps, content.NewStore(d)),
		reg:     reg,
	}
}

func starterSite() *contract.CompositionBundle {
	return &contract.CompositionBundle{
		ContractVersion: contract.CompositionBundleV1,
		Name:            "starter-site",
		Theme:           "starter",
		Pages: map[string]contract.Layout{
			"home": {
				ContractVersion: contract.LayoutCompositionV1,
				Regions: map[string]contract.Region{
					"main": {Blocks: []contract.Block{{Type: "hero"}}},
				},
			},
		},
		SampleContent: []contract.SampleContentItem{
			{Type: "article", Data: map[string]any{"title": "Hello world"}},
		},
		Manifest: contract.Manifest{
			RequiresContract: contract.LayoutCompositionV1,
			Blocks:           []string{"hero"},
			Slots:            []string{"main"},
		},
	}
}

func TestGetReturnsErrNotFoundBeforeAnySave(t *testing.T) {
	h := testHarness(t)
	if _, err := h.bundles.Get(context.Background(), "nope"); !errors.Is(err, bundle.ErrNotFound) {
		t.Fatalf("Get unsaved id error = %v, want ErrNotFound", err)
	}
}

func TestSaveThenGetRoundTrips(t *testing.T) {
	h := testHarness(t)
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, starterSite())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("Save returned an empty ID")
	}
	got, err := h.bundles.Get(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "starter-site" || len(got.Pages) != 1 {
		t.Fatalf("Get result = %+v, want the saved bundle back", got)
	}
}

func TestSaveRejectsUnregisteredBlockType(t *testing.T) {
	h := testHarness(t)
	emptyReg := blocks.New()
	var verrs contract.ValidationErrors
	if _, err := h.bundles.Save(context.Background(), admin, emptyReg, starterSite()); err == nil || !errors.As(err, &verrs) {
		t.Fatalf("Save with unregistered block type err = %v, want ValidationErrors", err)
	}
}

func TestSaveRequiresPresetsManageCapability(t *testing.T) {
	h := testHarness(t)
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := h.bundles.Save(context.Background(), editor, h.reg, starterSite()); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Save as editor err = %v, want ErrDenied", err)
	}
}

func TestImportMergesPagesAndCreatesSampleContentWhenCompatible(t *testing.T) {
	h := testHarness(t)
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, starterSite())
	if err != nil {
		t.Fatal(err)
	}

	result, err := h.bundles.Import(context.Background(), admin, h.reg, h.layouts, h.content, []string{"main"}, saved.ID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Compat.Compatible {
		t.Fatalf("Import result.Compat = %+v, want compatible", result.Compat)
	}
	if len(result.ImportedPages) != 1 || result.ImportedPages[0] != "home" {
		t.Fatalf("ImportedPages = %v, want [home]", result.ImportedPages)
	}
	if len(result.CreatedContent) != 1 || result.CreatedContent[0].Type != "article" {
		t.Fatalf("CreatedContent = %+v, want one created article", result.CreatedContent)
	}

	page, err := h.layouts.Load(context.Background(), "home")
	if err != nil {
		t.Fatalf("Load merged page: %v", err)
	}
	if len(page.Regions["main"].Blocks) != 1 || page.Regions["main"].Blocks[0].Type != "hero" {
		t.Fatalf("merged home layout = %+v, want the bundle's hero block", page)
	}

	items, err := h.content.List(context.Background(), admin, "article")
	if err != nil {
		t.Fatalf("List articles: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("articles after import = %d, want 1", len(items))
	}
}

func TestImportDeclinesToMergeOrCreateWhenIncompatible(t *testing.T) {
	h := testHarness(t)
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, starterSite())
	if err != nil {
		t.Fatal(err)
	}

	emptyReg := blocks.New() // "hero" not registered on the destination
	result, err := h.bundles.Import(context.Background(), admin, emptyReg, h.layouts, h.content, []string{"main"}, saved.ID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Compat.Compatible {
		t.Fatal("Import result.Compat.Compatible = true, want false")
	}
	if len(result.ImportedPages) != 0 {
		t.Fatalf("ImportedPages = %v, want none merged", result.ImportedPages)
	}
	if _, err := h.layouts.Load(context.Background(), "home"); !errors.Is(err, layout.ErrNotFound) {
		t.Fatalf("Load after declined import err = %v, want ErrNotFound", err)
	}
	items, err := h.content.List(context.Background(), admin, "article")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("articles after declined import = %d, want 0", len(items))
	}
}

func TestImportDeclinesWhenThemeLacksDeclaredSlot(t *testing.T) {
	h := testHarness(t)
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, starterSite())
	if err != nil {
		t.Fatal(err)
	}
	result, err := h.bundles.Import(context.Background(), admin, h.reg, h.layouts, h.content, []string{"header", "footer"}, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.Compat.Compatible {
		t.Fatal("Import result.Compat.Compatible = true, want false")
	}
	if len(result.Compat.MissingSlots) != 1 || result.Compat.MissingSlots[0] != "main" {
		t.Fatalf("MissingSlots = %v, want [main]", result.Compat.MissingSlots)
	}
}

func TestImportReportsSampleContentErrorButStillMergesPages(t *testing.T) {
	h := testHarness(t)
	b := starterSite()
	b.SampleContent = append(b.SampleContent, contract.SampleContentItem{Type: "does-not-exist", Data: map[string]any{}})
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, b)
	if err != nil {
		t.Fatal(err)
	}

	result, err := h.bundles.Import(context.Background(), admin, h.reg, h.layouts, h.content, []string{"main"}, saved.ID)
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if len(result.ImportedPages) != 1 {
		t.Fatalf("ImportedPages = %v, want [home] despite the bad sample item", result.ImportedPages)
	}
	if len(result.CreatedContent) != 1 {
		t.Fatalf("CreatedContent = %+v, want the one good article created", result.CreatedContent)
	}
	if len(result.ContentErrors) != 1 {
		t.Fatalf("ContentErrors = %v, want one reported error for the bad sample item", result.ContentErrors)
	}
}

func TestImportRequiresPresetsManageCapability(t *testing.T) {
	h := testHarness(t)
	saved, err := h.bundles.Save(context.Background(), admin, h.reg, starterSite())
	if err != nil {
		t.Fatal(err)
	}
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := h.bundles.Import(context.Background(), editor, h.reg, h.layouts, h.content, []string{"main"}, saved.ID); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Import as editor err = %v, want ErrDenied", err)
	}
}
