// Package compat implements the compatibility contract (PRD §13.3): the
// "hard part" of composition presets and bundles. Merging composition is
// trivial; predicting whether it will render correctly once merged is not.
// A Composition Preset or Bundle declares, in its Manifest, which block
// types and theme regions it assumes exist; a theme separately declares
// (pkg/theme.Theme.Regions) which regions it actually exposes. This package
// cross-checks a preset/bundle's declared assumptions — and its actual
// composition tree, in case the two have drifted — against the live
// *blocks.Registry and a destination theme's declared regions, producing a
// structured Result rather than a bare pass/fail: enough detail to drive an
// explainable "this preset needs the `pricing-table` block — install it?"
// prompt (PRD §13.3), not just a rejected import with no explanation.
//
// This package is deliberately decoupled from pkg/theme: it takes a
// destination theme's already-called Regions() as a plain []string, not a
// theme.Theme. pkg/theme imports internal/content (a kernel-internal
// package), so depending on it here would make this otherwise-public,
// plugin-importable package (like pkg/blocks and pkg/contract) transitively
// reach into kernel internals — exactly the boundary PRD §17.2 prohibits for
// public contract packages.
package compat

import (
	"sort"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

// Result is the compatibility contract's structured outcome. Compatible is
// true only when every declared/used block type is registered, every
// declared/used region is either satisfied by the destination theme or the
// theme declared no restriction at all, and the artifact's declared
// Layer-2 contract version is one this host recognizes.
type Result struct {
	Compatible bool `json:"compatible"`
	// MissingBlocks lists every block type the artifact declares or
	// actually references that is not registered in the destination
	// *blocks.Registry — sorted, de-duplicated.
	MissingBlocks []string `json:"missing_blocks,omitempty"`
	// MissingSlots lists every region name the artifact declares or
	// actually targets that the destination theme did not declare via
	// Regions() — sorted, de-duplicated. Always empty when the destination
	// theme's Regions() was nil (no declared restriction to check against).
	MissingSlots []string `json:"missing_slots,omitempty"`
	// UnsupportedContract holds the artifact's declared
	// Manifest.RequiresContract value when it names a Layer-2 contract
	// version this host does not recognize (currently, only
	// contract.LayoutCompositionV1 is supported) — reported separately from
	// missing blocks/slots because no amount of "install this block plugin"
	// fixes a contract-version mismatch.
	UnsupportedContract string `json:"unsupported_contract,omitempty"`
}

// supportedLayoutContract is the set of Layer-2 Layout contract versions
// this host's compatibility contract recognizes. A single-entry set today
// (only contract.LayoutCompositionV1 exists); kept as a set rather than a
// single `==` comparison so a future version bump that intends to support
// both an old and new version simultaneously (a migration window) is a
// one-line change here, not a restructuring of CheckPreset/CheckBundle.
var supportedLayoutContract = map[contract.Version]bool{
	contract.LayoutCompositionV1: true,
}

// CheckPreset validates preset's Manifest and actual composition tree
// against registry (the live, running *blocks.Registry) and themeRegions
// (the destination theme's declared Theme.Regions(), or nil if that theme
// declares no restriction), returning a structured Result. Both the
// manifest's declared Blocks/Slots and the block types/region actually
// referenced by preset.Layout are checked — catching both "the manifest
// under-declares what the tree needs" and "the tree references something
// that was never registered in the first place."
//
// CheckPreset does not itself call preset.Validate(); a caller should run
// that first for structural checks (mirroring contract.Layout.Validate then
// blocks.ValidateLayout's existing two-step precedent) — CheckPreset assumes
// preset is already structurally well-formed.
func CheckPreset(preset *contract.CompositionPreset, registry *blocks.Registry, themeRegions []string) Result {
	blockSet := stringSet(preset.Manifest.Blocks)
	addUsedBlockTypes(blockSet, preset.Layout)

	slotSet := stringSet(preset.Manifest.Slots)
	for region := range preset.Layout.Regions {
		slotSet[region] = true
	}

	return check(preset.Manifest.RequiresContract, blockSet, slotSet, registry, themeRegions)
}

// CheckBundle validates bundle's Manifest, every page Layout, and every
// included preset's own composition against registry and themeRegions,
// aggregating the union of every missing block/slot found across all of
// them into one Result — a bundle is incompatible if any one of its pieces
// is.
func CheckBundle(bundle *contract.CompositionBundle, registry *blocks.Registry, themeRegions []string) Result {
	blockSet := stringSet(bundle.Manifest.Blocks)
	slotSet := stringSet(bundle.Manifest.Slots)

	for _, l := range bundle.Pages {
		addUsedBlockTypes(blockSet, l)
		for region := range l.Regions {
			slotSet[region] = true
		}
	}
	for _, p := range bundle.Presets {
		for b := range stringSet(p.Manifest.Blocks) {
			blockSet[b] = true
		}
		addUsedBlockTypes(blockSet, p.Layout)
		for s := range stringSet(p.Manifest.Slots) {
			slotSet[s] = true
		}
		for region := range p.Layout.Regions {
			slotSet[region] = true
		}
	}

	return check(bundle.Manifest.RequiresContract, blockSet, slotSet, registry, themeRegions)
}

// check is the shared core both CheckPreset and CheckBundle reduce to once
// they've each assembled their own artifact-shape-specific block/slot sets:
// look up every declared/used block type in registry, every declared/used
// slot in themeRegions (if any restriction was declared), and the contract
// version against supportedLayoutContract.
func check(requiresContract contract.Version, blockSet, slotSet map[string]bool, registry *blocks.Registry, themeRegions []string) Result {
	var result Result

	for name := range blockSet {
		if _, ok := registry.Get(name); !ok {
			result.MissingBlocks = append(result.MissingBlocks, name)
		}
	}
	sort.Strings(result.MissingBlocks)

	if themeRegions != nil {
		allowed := stringSet(themeRegions)
		for name := range slotSet {
			if !allowed[name] {
				result.MissingSlots = append(result.MissingSlots, name)
			}
		}
		sort.Strings(result.MissingSlots)
	}

	if !supportedLayoutContract[requiresContract] {
		result.UnsupportedContract = string(requiresContract)
	}

	result.Compatible = len(result.MissingBlocks) == 0 && len(result.MissingSlots) == 0 && result.UnsupportedContract == ""
	return result
}

// addUsedBlockTypes walks l's regions/blocks/slots tree (via
// contract.WalkBlocks, the one shared tree-walk pkg/contract already
// exports) and adds every block Type actually present into set.
func addUsedBlockTypes(set map[string]bool, l contract.Layout) {
	for regionName, region := range l.Regions {
		contract.WalkBlocks("regions."+regionName+".blocks", region.Blocks, func(_ string, b contract.Block) contract.ValidationErrors {
			if b.Type != "" {
				set[b.Type] = true
			}
			return nil
		})
	}
}

func stringSet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, s := range items {
		set[s] = true
	}
	return set
}
