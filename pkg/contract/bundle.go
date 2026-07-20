package contract

import (
	"fmt"
	"sort"
)

// CompositionBundleV1 is the contract version for the Composition Bundle
// artifact (PRD §13.2: "a preset collection at site scale: pages + presets +
// sample content + a theme reference" — the typed equivalent of "import this
// starter site / demo").
const CompositionBundleV1 Version = "composition-bundle/v1"

// CompositionBundle is a preset collection at site scale: a named set of
// page Layouts (one per route), optionally some standalone Composition
// Presets included for reuse, sample Layer-1 content to seed, and a
// reference to the theme the bundle was designed against. Importing a
// bundle is "import this starter site / demo" (PRD §13.2) — merging every
// page's Layout into the destination, creating the sample content, all
// gated by the same compatibility contract (PRD §13.3) a lone preset is.
type CompositionBundle struct {
	ContractVersion Version `json:"contract_version"`
	Name            string  `json:"name"`
	Description     string  `json:"description,omitempty"`
	// Theme is the theme.Theme.Name() this bundle was designed against
	// (e.g. "starter") — a hint for the compatibility contract and for a
	// future UI ("this bundle looks best with the Starter theme"), not an
	// enforced requirement on its own; Manifest.Themes is the authoritative
	// declared restriction, if any.
	Theme string `json:"theme,omitempty"`
	// Pages maps route (the same route key internal/layout.Store persists
	// Layouts under, e.g. "home" or "blog/index") to the Layout that route
	// should have once the bundle is imported.
	Pages map[string]Layout `json:"pages"`
	// Presets optionally bundles standalone Composition Presets alongside
	// the pages themselves (e.g. a reusable "pricing section" the starter
	// site's pages happen to use, made available for reuse afterward).
	Presets []CompositionPreset `json:"presets,omitempty"`
	// SampleContent is the Layer-1 content this bundle seeds — a bundle at
	// site scale ships with sample posts/pages, not just empty layouts.
	SampleContent []SampleContentItem `json:"sample_content,omitempty"`
	Manifest      Manifest            `json:"manifest"`
}

// SampleContentItem is one Layer-1 content item a Composition Bundle seeds
// on import — a content type name (matching an already-declared
// contract.ContentType) plus the field data to create it with, the same
// shape internal/content.API.Create already accepts.
type SampleContentItem struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data"`
}

// Validate checks b's structural shape: a recognized contract version, a
// non-empty Name, a well-formed Manifest, valid route keys and structurally
// valid Layouts for every page, every included preset's own Validate(), and
// a non-empty content type name for every sample content item. Like
// CompositionPreset.Validate, this performs no live-registry or live-theme
// checks — see pkg/compat.CheckBundle for that.
func (b *CompositionBundle) Validate() error {
	var errs ValidationErrors

	if b.ContractVersion != CompositionBundleV1 {
		errs = append(errs, ValidationError{
			Path:    "contract_version",
			Message: fmt.Sprintf("unsupported version %q (supported: %s)", b.ContractVersion, CompositionBundleV1),
		})
	}
	if b.Name == "" {
		errs = append(errs, ValidationError{Path: "name", Message: "must not be empty"})
	}
	if b.Manifest.RequiresContract == "" {
		errs = append(errs, ValidationError{Path: "manifest.requires_contract", Message: "must not be empty"})
	}

	routes := make([]string, 0, len(b.Pages))
	for route := range b.Pages {
		routes = append(routes, route)
	}
	sort.Strings(routes)
	for _, route := range routes {
		l := b.Pages[route]
		if err := l.Validate(); err != nil {
			for _, e := range asValidationErrors(err) {
				errs = append(errs, ValidationError{Path: "pages." + route + "." + e.Path, Message: e.Message})
			}
		}
	}

	for i, p := range b.Presets {
		if err := p.Validate(); err != nil {
			for _, e := range asValidationErrors(err) {
				errs = append(errs, ValidationError{Path: fmt.Sprintf("presets[%d].%s", i, e.Path), Message: e.Message})
			}
		}
	}

	for i, item := range b.SampleContent {
		if item.Type == "" {
			errs = append(errs, ValidationError{Path: fmt.Sprintf("sample_content[%d].type", i), Message: "must not be empty"})
		}
	}

	if len(errs) > 0 {
		return errs
	}
	return nil
}

// asValidationErrors normalizes any error returned by a Validate method in
// this package into a ValidationErrors slice — every such method returns
// either nil, a ValidationErrors, or (defensively) some other error, and
// callers that need to re-path each violation (nesting it under
// "pages.<route>." or "presets[i].", as this file's Validate does twice)
// need the individual entries, not just an opaque error.
func asValidationErrors(err error) ValidationErrors {
	if err == nil {
		return nil
	}
	if ve, ok := err.(ValidationErrors); ok {
		return ve
	}
	return ValidationErrors{{Path: "", Message: err.Error()}}
}
