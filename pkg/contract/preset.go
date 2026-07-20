package contract

import "fmt"

// CompositionPresetV1 is the contract version for the Composition Preset
// artifact (PRD §13.2: "a serialized fragment of Layer-2 composition"). This
// is the preset *document's own* format version — distinct from the
// Manifest.RequiresContract field, which instead declares which
// LayoutCompositionV1-family version the preset's embedded Layout fragment
// was authored against.
const CompositionPresetV1 Version = "composition-preset/v1"

// CompositionPreset is a saved, importable fragment of Layer-2 composition
// (PRD §13.2): a named arrangement of blocks with placeholder content, at
// any scale from a single section to a full page. It is built directly from
// Layout/Region/Block — the same types a running route's Layout persists —
// rather than a parallel format, so "import a template" is exactly "merge
// this Layout fragment into a target Layout," never a bespoke deserializer.
//
// A CompositionPreset also carries a Manifest: the compatibility-contract
// declaration of what it assumes about the host it's imported into (PRD
// §13.3) — which block types it references and which theme regions it
// targets — so import-time validation can explain *why* a preset can't
// render cleanly ("needs the pricing-table block") instead of silently
// producing broken output.
type CompositionPreset struct {
	ContractVersion Version  `json:"contract_version"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	Layout          Layout   `json:"layout"`
	Manifest        Manifest `json:"manifest"`
}

// Manifest is the compatibility-contract declaration a Composition Preset or
// Composition Bundle carries (PRD §13.3): what the artifact assumes about
// the host it's imported into, checked both when it is published (does the
// declaration match what the composition actually references) and when it
// is imported (does the destination host actually have what's declared).
type Manifest struct {
	// RequiresContract is the Layer-2 Layout contract version this
	// artifact's composition was authored against (e.g.
	// LayoutCompositionV1). A host that doesn't recognize this version
	// cannot safely render the artifact even if every declared block and
	// slot happens to be present — see pkg/compat.Result.UnsupportedContract.
	RequiresContract Version `json:"requires_contract"`
	// Blocks lists every block type the artifact's composition assumes is
	// registered. Declared explicitly (not solely inferred by walking the
	// composition tree) so an author's stated intent and the actual tree
	// can be cross-checked at publish time — pkg/compat catches both "the
	// manifest forgot to declare a type the tree uses" and "the manifest
	// declares more than the tree actually needs."
	Blocks []string `json:"blocks,omitempty"`
	// Slots lists every theme *region* name (pkg/contract.Layout.Regions'
	// top-level keys — e.g. "header", "main", "sidebar" — NOT a Block's own
	// nested Slots, which are block-defined, not theme-defined) the
	// artifact's composition assumes the destination theme exposes.
	Slots []string `json:"slots,omitempty"`
	// Themes optionally restricts declared compatibility to specific theme
	// Name()s (e.g. "starter"). Empty means no declared restriction: the
	// artifact is compatible with any theme whose declared regions satisfy
	// Slots.
	Themes []string `json:"themes,omitempty"`
}

// Validate checks p's structural shape: a recognized contract version, a
// non-empty Name, a well-formed Manifest (non-empty RequiresContract), and
// p.Layout.Validate()'s own structural checks. Like contract.Layout.Validate,
// this performs no live-registry or live-theme checks — that is
// pkg/compat.CheckPreset's job, since checking against a live
// *blocks.Registry or a running theme's declared regions requires a runtime
// dependency this package deliberately has none of.
func (p *CompositionPreset) Validate() error {
	var errs ValidationErrors

	if p.ContractVersion != CompositionPresetV1 {
		errs = append(errs, ValidationError{
			Path:    "contract_version",
			Message: fmt.Sprintf("unsupported version %q (supported: %s)", p.ContractVersion, CompositionPresetV1),
		})
	}
	if p.Name == "" {
		errs = append(errs, ValidationError{Path: "name", Message: "must not be empty"})
	}
	if p.Manifest.RequiresContract == "" {
		errs = append(errs, ValidationError{Path: "manifest.requires_contract", Message: "must not be empty"})
	}
	if err := p.Layout.Validate(); err != nil {
		for _, e := range asValidationErrors(err) {
			errs = append(errs, ValidationError{Path: "layout." + e.Path, Message: e.Message})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}
