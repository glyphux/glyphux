package preset_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/layout"
	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/internal/preset"
	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

var admin = &permission.Principal{Role: permission.RoleAdmin}

func testDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	all := append(append([]db.Migration{}, preset.Migrations...), layout.Migrations...)
	if err := d.Migrate(context.Background(), all); err != nil {
		t.Fatal(err)
	}
	return d
}

func registryWithHero(t *testing.T) *blocks.Registry {
	t.Helper()
	r := blocks.New()
	if err := r.Register(blocks.Definition{Name: "hero", DisplayName: "Hero"}); err != nil {
		t.Fatal(err)
	}
	return r
}

func heroPreset() *contract.CompositionPreset {
	return &contract.CompositionPreset{
		ContractVersion: contract.CompositionPresetV1,
		Name:            "hero-section",
		Layout: contract.Layout{
			ContractVersion: contract.LayoutCompositionV1,
			Regions: map[string]contract.Region{
				"main": {Blocks: []contract.Block{{Type: "hero"}}},
			},
		},
		Manifest: contract.Manifest{
			RequiresContract: contract.LayoutCompositionV1,
			Blocks:           []string{"hero"},
			Slots:            []string{"main"},
		},
	}
}

func TestGetReturnsErrNotFoundBeforeAnySave(t *testing.T) {
	s := preset.NewStore(testDB(t))
	if _, err := s.Get(context.Background(), "nope"); !errors.Is(err, preset.ErrNotFound) {
		t.Fatalf("Get unsaved id error = %v, want ErrNotFound", err)
	}
}

func TestSaveThenGetRoundTrips(t *testing.T) {
	s := preset.NewStore(testDB(t))
	reg := registryWithHero(t)
	saved, err := s.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("Save returned an empty ID")
	}

	got, err := s.Get(context.Background(), saved.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "hero-section" || len(got.Layout.Regions["main"].Blocks) != 1 {
		t.Fatalf("Get result = %+v, want the saved preset back", got)
	}

	list, err := s.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != saved.ID {
		t.Fatalf("List = %+v, want one saved preset with ID %q", list, saved.ID)
	}
}

func TestSaveRejectsStructurallyInvalidPreset(t *testing.T) {
	s := preset.NewStore(testDB(t))
	reg := registryWithHero(t)
	bad := heroPreset()
	bad.ContractVersion = "wrong-version"
	var verrs contract.ValidationErrors
	if _, err := s.Save(context.Background(), admin, reg, bad); err == nil || !errors.As(err, &verrs) {
		t.Fatalf("Save with bad contract version err = %v, want ValidationErrors", err)
	}
}

func TestSaveRejectsUnregisteredBlockType(t *testing.T) {
	s := preset.NewStore(testDB(t))
	reg := blocks.New() // "hero" never registered
	var verrs contract.ValidationErrors
	if _, err := s.Save(context.Background(), admin, reg, heroPreset()); err == nil || !errors.As(err, &verrs) {
		t.Fatalf("Save with unregistered block type err = %v, want ValidationErrors", err)
	}
}

func TestSaveRequiresPresetsManageCapability(t *testing.T) {
	s := preset.NewStore(testDB(t))
	reg := registryWithHero(t)
	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := s.Save(context.Background(), editor, reg, heroPreset()); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Save as editor err = %v, want ErrDenied", err)
	}
	if _, err := s.Save(context.Background(), nil, reg, heroPreset()); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Save as anonymous err = %v, want ErrDenied", err)
	}
}

func TestCheckReportsMissingBlockWithoutMutating(t *testing.T) {
	s := preset.NewStore(testDB(t))
	reg := registryWithHero(t)
	saved, err := s.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}

	// Check against a registry missing "hero" (e.g. a different host).
	emptyReg := blocks.New()
	result, err := s.Check(context.Background(), emptyReg, []string{"main"}, saved.ID)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Compatible {
		t.Fatal("Check result.Compatible = true, want false")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
	// Check must not have mutated anything: the preset is still there,
	// still against its original (compatible) registry.
	stillGood, err := s.Check(context.Background(), reg, []string{"main"}, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !stillGood.Compatible {
		t.Fatalf("Check against the original registry after a prior incompatible Check = %+v, want compatible (Check must not mutate)", stillGood)
	}
}

func TestImportMergesCompatiblePresetIntoTargetRoute(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}

	result, err := presets.Import(context.Background(), admin, reg, layouts, []string{"main"}, saved.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("Import result = %+v, want compatible", result)
	}
	got, err := layouts.Load(context.Background(), "home")
	if err != nil {
		t.Fatalf("Load merged layout: %v", err)
	}
	if len(got.Regions["main"].Blocks) != 1 || got.Regions["main"].Blocks[0].Type != "hero" {
		t.Fatalf("merged layout = %+v, want the preset's hero block in region main", got)
	}
}

func TestImportMergesIntoExistingLayoutWithoutClobberingOtherRegions(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)
	if err := reg.Register(blocks.Definition{Name: "heading"}); err != nil {
		t.Fatal(err)
	}

	existing := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"header": {Blocks: []contract.Block{{Type: "heading"}}},
		},
	}
	if err := layouts.Save(context.Background(), admin, reg, "home", existing); err != nil {
		t.Fatal(err)
	}

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := presets.Import(context.Background(), admin, reg, layouts, []string{"header", "main"}, saved.ID, "home"); err != nil {
		t.Fatal(err)
	}

	got, err := layouts.Load(context.Background(), "home")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Regions["header"].Blocks) != 1 || got.Regions["header"].Blocks[0].Type != "heading" {
		t.Fatalf("existing header region = %+v, want untouched", got.Regions["header"])
	}
	if len(got.Regions["main"].Blocks) != 1 || got.Regions["main"].Blocks[0].Type != "hero" {
		t.Fatalf("merged main region = %+v, want the preset's hero block", got.Regions["main"])
	}
}

func TestImportDeclinesToMergeWhenIncompatibleButReportsWhy(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}

	// Import against a registry that no longer has "hero" (simulating a
	// destination host missing the block plugin).
	emptyReg := blocks.New()
	result, err := presets.Import(context.Background(), admin, emptyReg, layouts, []string{"main"}, saved.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Compatible {
		t.Fatal("Import result.Compatible = true, want false")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
	if _, err := layouts.Load(context.Background(), "home"); !errors.Is(err, layout.ErrNotFound) {
		t.Fatalf("Load after declined import err = %v, want ErrNotFound (nothing should have merged)", err)
	}
}

func TestImportDeclinesToMergeWhenThemeLacksDeclaredSlot(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}

	// Destination theme declares only "header"/"footer" — no "main".
	result, err := presets.Import(context.Background(), admin, reg, layouts, []string{"header", "footer"}, saved.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if result.Compatible {
		t.Fatal("Import result.Compatible = true, want false")
	}
	if len(result.MissingSlots) != 1 || result.MissingSlots[0] != "main" {
		t.Fatalf("MissingSlots = %v, want [main]", result.MissingSlots)
	}
}

func TestImportAcceptsAnySlotWhenThemeDeclaresNoRestriction(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}
	// nil themeRegions == a theme like headless that declares no restriction.
	result, err := presets.Import(context.Background(), admin, reg, layouts, nil, saved.ID, "home")
	if err != nil {
		t.Fatalf("Import: %v", err)
	}
	if !result.Compatible {
		t.Fatalf("Import result = %+v, want compatible", result)
	}
}

func TestImportRequiresPresetsManageCapability(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	saved, err := presets.Save(context.Background(), admin, reg, heroPreset())
	if err != nil {
		t.Fatal(err)
	}

	editor := &permission.Principal{Role: permission.RoleEditor}
	if _, err := presets.Import(context.Background(), editor, reg, layouts, []string{"main"}, saved.ID, "home"); !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Import as editor err = %v, want ErrDenied", err)
	}
}

func TestImportUnknownPresetIDReturnsErrNotFound(t *testing.T) {
	d := testDB(t)
	presets := preset.NewStore(d)
	layouts := layout.NewStore(d)
	reg := registryWithHero(t)

	if _, err := presets.Import(context.Background(), admin, reg, layouts, []string{"main"}, "does-not-exist", "home"); !errors.Is(err, preset.ErrNotFound) {
		t.Fatalf("Import unknown id err = %v, want ErrNotFound", err)
	}
}
