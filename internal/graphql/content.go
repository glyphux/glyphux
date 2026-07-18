package graphql

// Shared helpers for content-item resolvers. getContentItem mirrors the
// locale/drafts dispatch in handleContentGet (internal/api/api.go) exactly:
// GraphQL is a second transport over the same content.API, not a new
// behavior.

import (
	"context"

	"github.com/glyphux/glyphux/internal/content"
)

// getContentItem resolves a single item the way REST's handleContentGet
// does: drafts require content:read_drafts, and a non-empty locale resolves
// localized fields to a single value.
func (r *Resolver) getContentItem(ctx context.Context, typeName, id string, locale *string) (*content.Item, error) {
	loc := ""
	if locale != nil {
		loc = *locale
	}
	drafts := canReadDrafts(ctx)
	switch {
	case loc != "" && drafts:
		return r.content.GetLocalized(ctx, domainPrincipal(ctx), typeName, id, loc)
	case loc != "" && !drafts:
		return r.content.GetLocalizedPublished(ctx, typeName, id, loc)
	case drafts:
		return r.content.Get(ctx, domainPrincipal(ctx), typeName, id)
	default:
		return r.content.GetPublished(ctx, typeName, id)
	}
}

// listContentItems resolves a listing the way REST's handleContentList does.
func (r *Resolver) listContentItems(ctx context.Context, typeName string, locale *string) ([]*content.Item, error) {
	loc := ""
	if locale != nil {
		loc = *locale
	}
	drafts := canReadDrafts(ctx)
	switch {
	case loc != "" && drafts:
		return r.content.ListLocalized(ctx, domainPrincipal(ctx), typeName, loc)
	case loc != "" && !drafts:
		return r.content.ListLocalizedPublished(ctx, typeName, loc)
	case drafts:
		return r.content.List(ctx, domainPrincipal(ctx), typeName)
	default:
		return r.content.ListPublished(ctx, typeName)
	}
}
