// CORE-03 REVIEW (behavior-first): composition contract round-trip.
//
// Given a fully-populated Composition (ContentType + Field + Capabilities),
// Layout (Regions -> Blocks -> Slots), CompositionPreset (Layout +
// Manifest), and CompositionBundle (Pages + Presets + SampleContent +
// Manifest), When each is serialized to JSON and deserialized, Then:
//   - the JSON round-trip is lossless at the wire level (re-marshaling the
//     decoded copy is byte-identical to the original — every typed field and
//     every prop value survives), and
//   - any numeric value inside Block.Props decodes as float64 — the
//     contract's documented behavior (internal/content validate.go checkKind:
//     "Values arrive as decoded JSON, so numbers are float64"). int -> float64
//     is the intended JSON number kind, not a round-trip loss.
package contract

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// reviewIsNumber reports whether v is a JSON-numeric Go type (int/uint/float
// family). Props are authored as Go ints and arrive from JSON as float64 —
// see reviewPropsFloat64Intent.
func reviewIsNumber(v any) bool {
	switch v.(type) {
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return true
	}
	return false
}

// reviewPropsFloat64Intent walks the round-tripped blocks against the
// original and verifies the intended JSON semantics: every numeric prop
// value decoded as float64, every non-numeric prop value survived unchanged,
// and no block/type was dropped or renamed. Returns "" on success, else a
// diagnostic naming the field.
func reviewPropsFloat64Intent(original, roundTripped []Block, path string) string {
	for i := range original {
		if i >= len(roundTripped) {
			return fmt.Sprintf("%s[%d]: block dropped in round-trip", path, i)
		}
		if original[i].Type != roundTripped[i].Type {
			return fmt.Sprintf("%s[%d].type: %q -> %q", path, i, original[i].Type, roundTripped[i].Type)
		}
		for k, ov := range original[i].Props {
			rv, ok := roundTripped[i].Props[k]
			if !ok {
				return fmt.Sprintf("%s[%d].props.%s: missing after round-trip", path, i, k)
			}
			if reviewIsNumber(ov) {
				if _, isF64 := rv.(float64); !isF64 {
					return fmt.Sprintf("%s[%d].props.%s: numeric prop decoded as %T, want float64 (JSON number kind)", path, i, k, rv)
				}
			} else if !reflect.DeepEqual(ov, rv) {
				return fmt.Sprintf("%s[%d].props.%s: non-numeric prop changed: %#v -> %#v", path, i, k, ov, rv)
			}
		}
		for slot, children := range original[i].Slots {
			if m := reviewPropsFloat64Intent(children, roundTripped[i].Slots[slot], path+fmt.Sprintf("[%d].slots.%s", i, slot)); m != "" {
				return m
			}
		}
	}
	return ""
}

// reviewAssertLayoutRoundTrip asserts a Layout round-trip is lossless at the
// wire level and that Props semantics hold: numbers decode as float64,
// everything else survives unchanged.
func reviewAssertLayoutRoundTrip(t *testing.T, what string, got, want Layout) {
	t.Helper()
	rawGot, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	rawWant, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("marshal want: %v", err)
	}
	if !bytes.Equal(rawGot, rawWant) {
		t.Errorf("FAIL: %s JSON round-trip is not byte-identical\n got: %s\nwant: %s", what, rawGot, rawWant)
		return
	}
	t.Logf("PASS: %s JSON round-trip is byte-identical (%d bytes) — all typed fields, regions, and prop values preserved at the wire level", what, len(rawGot))
	for region, r := range want.Regions {
		if m := reviewPropsFloat64Intent(r.Blocks, got.Regions[region].Blocks, "regions."+region+".blocks"); m != "" {
			t.Errorf("FAIL: %s — %s", what, m)
			return
		}
	}
	t.Logf("PASS: %s — numeric Props decode as float64 (JSON number kind, per internal/content validate.go checkKind); non-numeric props unchanged", what)
}

// reviewComposition returns a fully-populated Layer-1 Composition.
func reviewComposition() Composition {
	return Composition{
		ContractVersion: ContentCompositionV0,
		Site:            Site{Name: "Review Site"},
		ContentTypes: map[string]ContentType{
			"article": {
				Fields: map[string]Field{
					"title":    {Type: FieldString, Required: true},
					"body":     {Type: FieldRichText, Localized: true},
					"views":    {Type: FieldNumber},
					"featured": {Type: FieldBoolean, Required: true},
					"author":   {Type: FieldRelation, To: "author"},
					"cover":    {Type: FieldMedia},
					"published": {Type: FieldDate, Localized: false},
				},
			},
			"author": {
				Fields: map[string]Field{
					"name": {Type: FieldString, Required: true},
				},
			},
		},
		Capabilities: []string{"content", "layout", "presets"},
	}
}

// reviewLayout returns a fully-populated Layer-2 Layout with nested slots.
func reviewLayout() Layout {
	return Layout{
		ContractVersion: LayoutCompositionV1,
		Regions: map[string]Region{
			"header": {
				Blocks: []Block{{Type: "site-title", Props: map[string]any{"text": "Hello"}}},
			},
			"main": {
				Blocks: []Block{
					{Type: "hero", Props: map[string]any{"title": "Welcome", "cta": "Start"}},
					{
						Type:  "container",
						Props: map[string]any{"columns": 2},
						Slots: map[string][]Block{
							"content": {
								{Type: "heading", Props: map[string]any{"level": "h2"}},
								{Type: "paragraph", Props: map[string]any{"text": "Body"}},
							},
						},
					},
				},
			},
		},
	}
}

// TestReviewCompositionRoundTrip — Layer-1 Composition, lossless JSON
// (typed fields only — no untyped Props map).
func TestReviewCompositionRoundTrip(t *testing.T) {
	t.Run("Given a fully-populated Composition, When JSON round-tripped, Then all fields survive (deep equality)", func(t *testing.T) {
		want := reviewComposition()
		t.Logf("Given Composition with %d content types, %d fields on 'article', %d capabilities",
			len(want.ContentTypes), len(want.ContentTypes["article"].Fields), len(want.Capabilities))
		t.Logf("When  serialized to JSON and deserialized")
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got Composition
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("FAIL: round-trip differs\n got: %+v\nwant: %+v", got, want)
			return
		}
		t.Logf("PASS: %d JSON bytes round-tripped losslessly (Site=%q, ContractVersion=%q)", len(raw), got.Site.Name, got.ContractVersion)
	})
}

// TestReviewLayoutRoundTrip — Layer-2 Layout (Regions/Blocks/Slots).
func TestReviewLayoutRoundTrip(t *testing.T) {
	t.Run("Given a fully-populated Layout with nested slots, When JSON round-tripped, Then typed fields and non-numeric props are lossless and numeric props decode as float64", func(t *testing.T) {
		want := reviewLayout()
		t.Logf("Given Layout with %d regions, %d blocks in 'main' (nested slots, numeric prop columns=2)",
			len(want.Regions), len(want.Regions["main"].Blocks))
		t.Logf("When  serialized to JSON and deserialized")
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got Layout
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		reviewAssertLayoutRoundTrip(t, "Layout", got, want)
	})
}

// TestReviewPresetRoundTrip — CompositionPreset (Layout + Manifest).
func TestReviewPresetRoundTrip(t *testing.T) {
	t.Run("Given a fully-populated CompositionPreset with Manifest, When JSON round-tripped, Then typed fields are exact and numeric props decode as float64", func(t *testing.T) {
		want := CompositionPreset{
			ContractVersion: CompositionPresetV1,
			Name:            "pricing-section",
			Description:     "A reusable pricing section",
			Layout: Layout{
				ContractVersion: LayoutCompositionV1,
				Regions:         map[string]Region{"main": {Blocks: []Block{{Type: "pricing-table", Props: map[string]any{"tiers": 3}}}}},
			},
			Manifest: Manifest{
				RequiresContract: LayoutCompositionV1,
				Blocks:           []string{"pricing-table"},
				Slots:            []string{"main"},
				Themes:           []string{"starter"},
			},
		}
		t.Logf("Given CompositionPreset %q with %d manifest blocks and %d manifest slots (numeric prop tiers=3)",
			want.Name, len(want.Manifest.Blocks), len(want.Manifest.Slots))
		t.Logf("When  serialized to JSON and deserialized")
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got CompositionPreset
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if got.ContractVersion != want.ContractVersion || got.Name != want.Name || got.Description != want.Description || !reflect.DeepEqual(got.Manifest, want.Manifest) {
			t.Errorf("FAIL: preset typed fields changed\n got: %+v\nwant: %+v", got, want)
			return
		}
		t.Logf("PASS: preset typed fields (ContractVersion, Name, Description, Manifest) round-trip exactly")
		reviewAssertLayoutRoundTrip(t, "Preset.Layout", got.Layout, want.Layout)
	})
}

// TestReviewBundleRoundTrip — CompositionBundle (Pages + Presets +
// SampleContent + Manifest).
func TestReviewBundleRoundTrip(t *testing.T) {
	t.Run("Given a fully-populated CompositionBundle, When JSON round-tripped, Then typed fields are exact and numeric props decode as float64", func(t *testing.T) {
		want := CompositionBundle{
			ContractVersion: CompositionBundleV1,
			Name:            "starter-demo",
			Description:     "Starter site demo bundle",
			Theme:           "starter",
			Pages: map[string]Layout{
				"home": reviewLayout(),
				"blog/index": {
					ContractVersion: LayoutCompositionV1,
					Regions:         map[string]Region{"main": {Blocks: []Block{{Type: "post-list"}}}},
				},
			},
			Presets: []CompositionPreset{
				{
					ContractVersion: CompositionPresetV1,
					Name:            "pricing-section",
					Layout:          reviewLayout(),
					Manifest:        Manifest{RequiresContract: LayoutCompositionV1, Blocks: []string{"hero"}},
				},
			},
			SampleContent: []SampleContentItem{
				{Type: "article", Data: map[string]any{"title": "Hello world", "body": "<p>hi</p>"}},
			},
			Manifest: Manifest{
				RequiresContract: LayoutCompositionV1,
				Blocks:           []string{"hero", "post-list", "site-title"},
				Slots:            []string{"header", "main"},
				Themes:           []string{"starter"},
			},
		}
		t.Logf("Given CompositionBundle %q with %d pages, %d presets, %d sample items",
			want.Name, len(want.Pages), len(want.Presets), len(want.SampleContent))
		t.Logf("When  serialized to JSON and deserialized")
		raw, err := json.Marshal(want)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var got CompositionBundle
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		// Wire-level losslessness for the whole bundle.
		rawGot, err := json.Marshal(got)
		if err != nil {
			t.Fatalf("re-marshal: %v", err)
		}
		if !bytes.Equal(rawGot, raw) {
			t.Errorf("FAIL: bundle JSON round-trip is not byte-identical\n got: %s\nwant: %s", rawGot, raw)
			return
		}
		t.Logf("PASS: bundle JSON round-trip is byte-identical (%d bytes) — Name, Description, Theme, Manifest, SampleContent, Pages all preserved at the wire level", len(rawGot))

		// Numeric-props semantics across every embedded layout.
		for route, page := range want.Pages {
			if m := reviewPropsFloat64Intent(page.Regions["main"].Blocks, got.Pages[route].Regions["main"].Blocks, "pages."+route+".regions.main.blocks"); m != "" {
				t.Errorf("FAIL: bundle — %s", m)
				return
			}
		}
		for i, p := range want.Presets {
			if m := reviewPropsFloat64Intent(p.Layout.Regions["main"].Blocks, got.Presets[i].Layout.Regions["main"].Blocks, fmt.Sprintf("presets[%d].layout.regions.main.blocks", i)); m != "" {
				t.Errorf("FAIL: bundle — %s", m)
				return
			}
		}
		t.Logf("PASS: bundle — numeric Props decode as float64 (JSON number kind); non-numeric props unchanged (pages=%d, presets=%d, sample_content=%d)",
			len(got.Pages), len(got.Presets), len(got.SampleContent))
	})
}
