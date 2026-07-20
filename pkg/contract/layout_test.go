package contract

import "testing"

func validLayout() Layout {
	return Layout{
		ContractVersion: LayoutCompositionV1,
		Regions: map[string]Region{
			"main": {
				Blocks: []Block{
					{Type: "heading", Props: map[string]any{"text": "Hello"}},
					{
						Type: "container",
						Slots: map[string][]Block{
							"content": {
								{Type: "paragraph", Props: map[string]any{"text": "Body"}},
							},
						},
					},
				},
			},
		},
	}
}

func TestLayoutValidateAcceptsWellFormedDocument(t *testing.T) {
	l := validLayout()
	if err := l.Validate(); err != nil {
		t.Fatalf("expected valid layout to pass, got %v", err)
	}
}

func TestLayoutValidateRejectsUnsupportedVersion(t *testing.T) {
	l := validLayout()
	l.ContractVersion = "layout-composition/v99"
	if err := l.Validate(); err == nil {
		t.Fatal("expected error for unsupported contract version")
	}
}

func TestLayoutValidateRejectsInvalidRegionName(t *testing.T) {
	l := validLayout()
	l.Regions["Bad-Name!"] = Region{Blocks: []Block{{Type: "heading"}}}
	if err := l.Validate(); err == nil {
		t.Fatal("expected error for invalid region name")
	}
}

func TestLayoutValidateRejectsBlockWithEmptyType(t *testing.T) {
	l := validLayout()
	region := l.Regions["main"]
	region.Blocks = append(region.Blocks, Block{Type: ""})
	l.Regions["main"] = region
	if err := l.Validate(); err == nil {
		t.Fatal("expected error for block with empty type")
	}
}

func TestLayoutValidateRejectsInvalidSlotName(t *testing.T) {
	l := validLayout()
	region := l.Regions["main"]
	region.Blocks[1].Slots["Bad Slot!"] = []Block{{Type: "paragraph"}}
	l.Regions["main"] = region
	if err := l.Validate(); err == nil {
		t.Fatal("expected error for invalid slot name")
	}
}

func TestLayoutValidateRecursesIntoNestedSlotBlocks(t *testing.T) {
	l := validLayout()
	region := l.Regions["main"]
	region.Blocks[1].Slots["content"] = append(region.Blocks[1].Slots["content"], Block{Type: ""})
	l.Regions["main"] = region
	if err := l.Validate(); err == nil {
		t.Fatal("expected error to surface from a nested slot block, not just top-level blocks")
	}
}

func TestLayoutValidateReportsMultipleViolations(t *testing.T) {
	l := validLayout()
	l.ContractVersion = "bogus"
	region := l.Regions["main"]
	region.Blocks = append(region.Blocks, Block{Type: ""})
	l.Regions["main"] = region

	err := l.Validate()
	if err == nil {
		t.Fatal("expected error")
	}
	errs, ok := err.(ValidationErrors)
	if !ok {
		t.Fatalf("expected ValidationErrors, got %T", err)
	}
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 violations, got %d", len(errs))
	}
}
