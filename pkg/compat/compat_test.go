package compat_test

import (
	"testing"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/compat"
	"github.com/glyphux/glyphux/pkg/contract"
)

func registryWith(names ...string) *blocks.Registry {
	r := blocks.New()
	for _, n := range names {
		if err := r.Register(blocks.Definition{Name: n}); err != nil {
			panic(err)
		}
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

func TestCheckPresetAllDependenciesPresentIsCompatible(t *testing.T) {
	registry := registryWith("hero")
	result := compat.CheckPreset(heroPreset(), registry, []string{"header", "main", "footer"})

	if !result.Compatible {
		t.Fatalf("Compatible = false, want true (result: %+v)", result)
	}
	if len(result.MissingBlocks) != 0 || len(result.MissingSlots) != 0 || result.UnsupportedContract != "" {
		t.Fatalf("expected a clean Result, got %+v", result)
	}
}

func TestCheckPresetMissingBlockType(t *testing.T) {
	registry := registryWith() // "hero" never registered
	result := compat.CheckPreset(heroPreset(), registry, []string{"main"})

	if result.Compatible {
		t.Fatal("Compatible = true, want false")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
}

func TestCheckPresetMissingBlockTypeReferencedButNotDeclared(t *testing.T) {
	// The manifest under-declares (forgets "hero"); CheckPreset must still
	// catch it by walking the actual composition tree, not just trusting
	// the manifest's own Blocks list.
	preset := heroPreset()
	preset.Manifest.Blocks = nil
	registry := registryWith() // still nothing registered

	result := compat.CheckPreset(preset, registry, []string{"main"})

	if result.Compatible {
		t.Fatal("Compatible = true, want false")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "hero" {
		t.Fatalf("MissingBlocks = %v, want [hero]", result.MissingBlocks)
	}
}

func TestCheckPresetMissingSlot(t *testing.T) {
	registry := registryWith("hero")
	// Destination theme only declares "header"/"footer" — no "main".
	result := compat.CheckPreset(heroPreset(), registry, []string{"header", "footer"})

	if result.Compatible {
		t.Fatal("Compatible = true, want false")
	}
	if len(result.MissingSlots) != 1 || result.MissingSlots[0] != "main" {
		t.Fatalf("MissingSlots = %v, want [main]", result.MissingSlots)
	}
	if len(result.MissingBlocks) != 0 {
		t.Fatalf("MissingBlocks = %v, want none", result.MissingBlocks)
	}
}

func TestCheckPresetThemeWithNoDeclaredRegionsAcceptsAnySlot(t *testing.T) {
	registry := registryWith("hero")
	// nil themeRegions == "no declared restriction" (e.g. themes/headless).
	result := compat.CheckPreset(heroPreset(), registry, nil)

	if !result.Compatible {
		t.Fatalf("Compatible = false, want true (result: %+v)", result)
	}
	if len(result.MissingSlots) != 0 {
		t.Fatalf("MissingSlots = %v, want none when theme declares no restriction", result.MissingSlots)
	}
}

func TestCheckPresetUnsupportedContractVersion(t *testing.T) {
	preset := heroPreset()
	preset.Manifest.RequiresContract = "layout-composition/v99"
	registry := registryWith("hero")

	result := compat.CheckPreset(preset, registry, []string{"main"})

	if result.Compatible {
		t.Fatal("Compatible = true, want false")
	}
	if result.UnsupportedContract != "layout-composition/v99" {
		t.Fatalf("UnsupportedContract = %q, want %q", result.UnsupportedContract, "layout-composition/v99")
	}
}

func TestCheckBundleAggregatesAcrossPagesAndPresets(t *testing.T) {
	bundle := &contract.CompositionBundle{
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
			"pricing": {
				ContractVersion: contract.LayoutCompositionV1,
				Regions: map[string]contract.Region{
					"main": {Blocks: []contract.Block{{Type: "pricing-table"}}},
				},
			},
		},
		Manifest: contract.Manifest{RequiresContract: contract.LayoutCompositionV1},
	}

	// Only "hero" is registered; "pricing-table" (used on the "pricing"
	// page, not merely declared) is missing.
	registry := registryWith("hero")
	result := compat.CheckBundle(bundle, registry, []string{"header", "main", "footer"})

	if result.Compatible {
		t.Fatal("Compatible = true, want false")
	}
	if len(result.MissingBlocks) != 1 || result.MissingBlocks[0] != "pricing-table" {
		t.Fatalf("MissingBlocks = %v, want [pricing-table]", result.MissingBlocks)
	}
}

func TestCheckBundleAllDependenciesPresentIsCompatible(t *testing.T) {
	bundle := &contract.CompositionBundle{
		ContractVersion: contract.CompositionBundleV1,
		Name:            "starter-site",
		Pages: map[string]contract.Layout{
			"home": {
				ContractVersion: contract.LayoutCompositionV1,
				Regions: map[string]contract.Region{
					"main": {Blocks: []contract.Block{{Type: "hero"}}},
				},
			},
		},
		Manifest: contract.Manifest{RequiresContract: contract.LayoutCompositionV1},
	}
	registry := registryWith("hero")
	result := compat.CheckBundle(bundle, registry, []string{"main"})

	if !result.Compatible {
		t.Fatalf("Compatible = false, want true (result: %+v)", result)
	}
}
