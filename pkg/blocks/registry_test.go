package blocks_test

import (
	"testing"

	"github.com/glyphux/glyphux/pkg/blocks"
	"github.com/glyphux/glyphux/pkg/contract"
)

func headingDef() blocks.Definition {
	return blocks.Definition{
		Name:        "heading",
		DisplayName: "Heading",
		Props: map[string]contract.Field{
			"text": {Type: contract.FieldString, Required: true},
		},
	}
}

func containerDef() blocks.Definition {
	return blocks.Definition{
		Name:        "container",
		DisplayName: "Container",
		Slots:       []string{"content"},
	}
}

func TestRegistryRegisterThenGet(t *testing.T) {
	r := blocks.New()
	if err := r.Register(headingDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	def, ok := r.Get("heading")
	if !ok {
		t.Fatal("expected heading to be registered")
	}
	if def.DisplayName != "Heading" {
		t.Fatalf("unexpected def: %+v", def)
	}
}

func TestRegistryGetUnknownReturnsFalse(t *testing.T) {
	r := blocks.New()
	_, ok := r.Get("nonexistent")
	if ok {
		t.Fatal("expected ok=false for unregistered block")
	}
}

func TestRegistryRejectsDuplicateName(t *testing.T) {
	r := blocks.New()
	if err := r.Register(headingDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(headingDef()); err == nil {
		t.Fatal("expected error registering a duplicate block name")
	}
}

func TestRegistryRejectsEmptyName(t *testing.T) {
	r := blocks.New()
	def := headingDef()
	def.Name = ""
	if err := r.Register(def); err == nil {
		t.Fatal("expected error registering a block with an empty name")
	}
}

func TestRegistryList(t *testing.T) {
	r := blocks.New()
	if err := r.Register(headingDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(containerDef()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	list := r.List()
	if len(list) != 2 {
		t.Fatalf("expected 2 registered blocks, got %d", len(list))
	}
}

func TestRegistryIsSafeForConcurrentUse(t *testing.T) {
	r := blocks.New()
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			def := blocks.Definition{Name: string(rune('a' + i))}
			_ = r.Register(def)
			r.Get(string(rune('a' + i)))
			r.List()
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}
}

func TestValidateLayoutAcceptsBlocksThatExistInRegistry(t *testing.T) {
	r := blocks.New()
	_ = r.Register(headingDef())
	_ = r.Register(containerDef())

	layout := contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{
				{Type: "heading", Props: map[string]any{"text": "hi"}},
				{Type: "container", Slots: map[string][]contract.Block{
					"content": {{Type: "heading", Props: map[string]any{"text": "nested"}}},
				}},
			}},
		},
	}
	if err := blocks.ValidateLayout(&layout, r); err != nil {
		t.Fatalf("expected valid layout to pass, got %v", err)
	}
}

func TestValidateLayoutRejectsUnknownBlockType(t *testing.T) {
	r := blocks.New()
	_ = r.Register(headingDef())

	layout := contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{{Type: "does-not-exist"}}},
		},
	}
	if err := blocks.ValidateLayout(&layout, r); err == nil {
		t.Fatal("expected error for a block type not present in the registry")
	}
}

func TestValidateLayoutRejectsUnknownBlockTypeInNestedSlot(t *testing.T) {
	r := blocks.New()
	_ = r.Register(containerDef())

	layout := contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{
				{Type: "container", Slots: map[string][]contract.Block{
					"content": {{Type: "does-not-exist"}},
				}},
			}},
		},
	}
	if err := blocks.ValidateLayout(&layout, r); err == nil {
		t.Fatal("expected a nested unknown block type to be caught, not just top-level blocks")
	}
}
