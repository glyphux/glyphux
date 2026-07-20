package client

import (
	"context"
	"net/url"

	"github.com/glyphux/glyphux/pkg/contract"
)

// ContentTypesService wraps /api/v0/content-types — structural schema
// management, distinct from ContentService's per-item CRUD.
type ContentTypesService struct{ c *Client }

// List returns every currently declared content type.
func (s *ContentTypesService) List(ctx context.Context) (map[string]contract.ContentType, error) {
	var resp struct {
		ContentTypes map[string]contract.ContentType `json:"content_types"`
	}
	if err := s.c.doJSON(ctx, "GET", "/api/v0/content-types", nil, &resp); err != nil {
		return nil, err
	}
	return resp.ContentTypes, nil
}

// Define creates a new content type or replaces an existing one's field set.
// Requires content_types:manage (admin only).
func (s *ContentTypesService) Define(ctx context.Context, name string, ct contract.ContentType) (*contract.ContentType, error) {
	var result contract.ContentType
	if err := s.c.doJSON(ctx, "PUT", "/api/v0/content-types/"+url.PathEscape(name), ct, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Delete removes a content type. Requires content_types:manage. Fails with
// an *APIError (status 409) if items of that type still exist, or 404 if the
// type is unknown.
func (s *ContentTypesService) Delete(ctx context.Context, name string) error {
	return s.c.doJSON(ctx, "DELETE", "/api/v0/content-types/"+url.PathEscape(name), nil, nil)
}
