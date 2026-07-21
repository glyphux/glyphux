package theme_test

import (
	"testing"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/glyphux/glyphux/pkg/theme"
)

func TestNewCompositionViewItemReturnsFirstItem(t *testing.T) {
	first := &content.Item{ID: "1", Type: "page"}
	second := &content.Item{ID: "2", Type: "page"}
	v := theme.NewCompositionView([]*content.Item{first, second}, nil)

	if got := v.Item(); got != first {
		t.Fatalf("Item() = %v, want %v", got, first)
	}
	items := v.Items()
	if len(items) != 2 || items[0] != first || items[1] != second {
		t.Fatalf("Items() = %v, want [%v %v]", items, first, second)
	}
	if v.Layout() != nil {
		t.Fatalf("Layout() = %v, want nil", v.Layout())
	}
}

func TestNewCompositionViewItemReturnsNilForEmptyItems(t *testing.T) {
	v := theme.NewCompositionView(nil, nil)
	if got := v.Item(); got != nil {
		t.Fatalf("Item() = %v, want nil", got)
	}
	if got := v.Items(); len(got) != 0 {
		t.Fatalf("Items() = %v, want empty", got)
	}
}

func TestNewCompositionViewCarriesLayout(t *testing.T) {
	layout := &contract.Layout{
		ContractVersion: contract.LayoutCompositionV1,
		Regions: map[string]contract.Region{
			"main": {Blocks: []contract.Block{{Type: "heading"}}},
		},
	}
	v := theme.NewCompositionView(nil, layout)
	if v.Layout() != layout {
		t.Fatalf("Layout() = %v, want %v", v.Layout(), layout)
	}
}
