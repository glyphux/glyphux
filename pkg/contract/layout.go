package contract

import "fmt"

// LayoutCompositionV1 is the Layer-2 (Layout Composition) contract version
// (PRD §14 slice 4.1: "blocks, slots, regions, sections — additive to
// Layer 1, never polluting it"). It is a wholly separate document from
// Layer 1's Composition — a Layout describes how blocks are arranged for
// one route/template, not what content types exist — so it is its own
// top-level type, not a field bolted onto Composition.
const LayoutCompositionV1 Version = "layout-composition/v1"

// Layout is the Layer-2 root document: the arrangement of blocks across a
// route/template's named regions.
type Layout struct {
	ContractVersion Version           `json:"contract_version"`
	Regions         map[string]Region `json:"regions"`
}

// Region is a named placement area within a layout/template (e.g. "header",
// "main", "sidebar", "footer") — a theme declares which regions it exposes
// (PRD §9.2); a Layout places blocks into them.
type Region struct {
	Blocks []Block `json:"blocks"`
}

// Block is one placed instance of a registered block type, with its own
// prop values and, for container-shaped blocks, nested blocks per named
// slot. "Sections" from the PRD's "blocks, slots, regions, sections" list
// are modeled as an ordinary Block (by convention, one whose Type names a
// section-shaped block registered like any other) rather than a distinct
// Go type — the PRD doesn't specify a structural difference between a
// "section" and any other container block, so introducing a second type
// with no differentiated behavior would be speculative.
type Block struct {
	Type string `json:"type"`
	// Props are this block's own field values — validated structurally
	// here only as "present or absent" (a map); a registered block
	// definition's prop schema (pkg/blocks.Definition.Props) is checked
	// against a live registry one level up, not by this package (pkg/
	// contract must not depend on a runtime registry — see this slice's
	// tracking doc).
	Props map[string]any `json:"props,omitempty"`
	// Slots holds nested blocks per named slot, for container-shaped
	// blocks. A leaf block (e.g. "heading") simply has no entries here.
	Slots map[string][]Block `json:"slots,omitempty"`
}

// Validate checks l's structural shape: valid contract version, valid
// region/slot names, and every block (recursively, through every nested
// slot) has a non-empty Type. It does NOT check that a Block's Type or
// Props match a real registered block definition — that is
// pkg/blocks.ValidateLayout's job, since checking against a live registry
// requires a runtime dependency this package deliberately has none of.
func (l *Layout) Validate() error {
	var errs ValidationErrors

	if l.ContractVersion != LayoutCompositionV1 {
		errs = append(errs, ValidationError{
			Path:    "contract_version",
			Message: fmt.Sprintf("unsupported version %q (supported: %s)", l.ContractVersion, LayoutCompositionV1),
		})
	}

	for regionName, region := range l.Regions {
		if !validIdent(regionName) {
			errs = append(errs, ValidationError{
				Path:    "regions." + regionName,
				Message: "region name must be a lowercase identifier (a-z, 0-9, _)",
			})
		}
		errs = append(errs, validateBlocks("regions."+regionName+".blocks", region.Blocks)...)
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// validateBlocks recursively validates blocks (and every block's nested
// slot blocks), returning every violation found at or below path.
func validateBlocks(path string, blocks []Block) ValidationErrors {
	var errs ValidationErrors
	for i, b := range blocks {
		blockPath := fmt.Sprintf("%s[%d]", path, i)
		if b.Type == "" {
			errs = append(errs, ValidationError{Path: blockPath + ".type", Message: "must not be empty"})
		}
		for slotName, slotBlocks := range b.Slots {
			slotPath := blockPath + ".slots." + slotName
			if !validIdent(slotName) {
				errs = append(errs, ValidationError{
					Path:    slotPath,
					Message: "slot name must be a lowercase identifier (a-z, 0-9, _)",
				})
			}
			errs = append(errs, validateBlocks(slotPath, slotBlocks)...)
		}
	}
	return errs
}
