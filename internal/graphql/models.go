package graphql

// Conversions from domain/contract types to generated GraphQL models. Kept
// separate from the resolver bodies (schema.resolvers.go) so codegen
// re-runs never touch this file's logic.

import (
	"sort"
	"strconv"
	"time"

	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/graphql/generated"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
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

// userModel converts an identity.User to its GraphQL model. ID is rendered
// as a string, matching the GraphQL ID scalar's wire representation, even
// though identity.User.ID is an int64 internally.
func userModel(u *identity.User) *generated.User {
	return &generated.User{
		ID:         strconv.FormatInt(u.ID, 10),
		Email:      u.Email,
		Role:       u.Role,
		MfaEnabled: u.MFAEnabled,
		Active:     u.Active,
	}
}

// userIDFromGraphQL parses a GraphQL ID scalar back into identity.User's
// int64 primary key, matching userIDFromPath in internal/api/users.go.
func userIDFromGraphQL(id string) (int64, error) {
	n, err := strconv.ParseInt(id, 10, 64)
	if err != nil {
		return 0, gqlErr("BAD_REQUEST", "invalid user id")
	}
	return n, nil
}

// mediaItemModel converts a media.Item to its GraphQL model.
func mediaItemModel(item *media.Item) *generated.MediaItem {
	tags := item.Tags
	if tags == nil {
		tags = []string{}
	}
	return &generated.MediaItem{
		ID:        item.ID,
		Filename:  item.Filename,
		MimeType:  item.MimeType,
		SizeBytes: int(item.SizeBytes),
		Width:     item.Width,
		Height:    item.Height,
		AltText:     item.AltText,
		Tags:        tags,
		Source:      item.Source,
		Attribution: item.Attribution,
		CreatedAt:   formatTime(item.CreatedAt),
		UpdatedAt:   formatTime(item.UpdatedAt),
	}
}

// contentTypeFromInput converts GraphQL FieldInput values to a
// contract.ContentType, the shape composition.Store.DefineContentType
// expects — mirroring how REST's handleContentTypePut just JSON-decodes
// the request body directly into a contract.ContentType.
func contentTypeFromInput(fields []*generated.FieldInput) contract.ContentType {
	ct := contract.ContentType{Fields: map[string]contract.Field{}}
	for _, f := range fields {
		field := contract.Field{Type: contract.FieldType(f.Type)}
		if f.Required != nil {
			field.Required = *f.Required
		}
		if f.Localized != nil {
			field.Localized = *f.Localized
		}
		if f.To != nil {
			field.To = *f.To
		}
		ct.Fields[f.Name] = field
	}
	return ct
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
