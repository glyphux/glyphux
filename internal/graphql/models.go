package graphql

// Conversions from domain/contract types to generated GraphQL models. Kept
// separate from the resolver bodies (schema.resolvers.go) so codegen
// re-runs never touch this file's logic.

import (
	"sort"
	"time"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/graphql/generated"
	"github.com/glyphux/glyphux/pkg/contract"
)

// formatTime renders a timestamp the way REST's JSON encoding of time.Time
// does: RFC 3339.
func formatTime(t time.Time) string {
	return t.Format(time.RFC3339Nano)
}

// contentItemModel converts a content.Item to its GraphQL model.
func contentItemModel(item *content.Item) *generated.ContentItem {
	return &generated.ContentItem{
		ID:        item.ID,
		Type:      item.Type,
		Data:      item.Data,
		Status:    item.Status,
		Version:   item.Version,
		CreatedAt: formatTime(item.CreatedAt),
		UpdatedAt: formatTime(item.UpdatedAt),
	}
}

// contentTypeDefs converts the composition's declared content types to
// GraphQL models, sorted by name for a stable response order (Go map
// iteration is unordered).
func contentTypeDefs(comp *contract.Composition) []*generated.ContentTypeDef {
	names := make([]string, 0, len(comp.ContentTypes))
	for name := range comp.ContentTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make([]*generated.ContentTypeDef, 0, len(names))
	for _, name := range names {
		defs = append(defs, contentTypeDef(name, comp.ContentTypes[name]))
	}
	return defs
}

func contentTypeDef(name string, ct contract.ContentType) *generated.ContentTypeDef {
	fieldNames := make([]string, 0, len(ct.Fields))
	for name := range ct.Fields {
		fieldNames = append(fieldNames, name)
	}
	sort.Strings(fieldNames)

	fields := make([]*generated.FieldDef, 0, len(fieldNames))
	for _, fname := range fieldNames {
		fields = append(fields, fieldDef(fname, ct.Fields[fname]))
	}
	return &generated.ContentTypeDef{Name: name, Fields: fields}
}

func fieldDef(name string, f contract.Field) *generated.FieldDef {
	def := &generated.FieldDef{
		Name:      name,
		Type:      string(f.Type),
		Required:  f.Required,
		Localized: f.Localized,
	}
	if f.To != "" {
		to := f.To
		def.To = &to
	}
	return def
}
