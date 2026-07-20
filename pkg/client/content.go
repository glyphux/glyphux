package client

import (
	"context"
	"net/url"
	"strconv"
	"time"
)

// Item is a content item, mirroring internal/content.Item's wire shape.
type Item struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Data      map[string]any `json:"data"`
	Status    string         `json:"status"`
	Version   int            `json:"version"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// ContentService wraps /api/v0/content.
type ContentService struct{ c *Client }

// Create makes a new draft item of typeName with the given field data.
// Requires the content:write capability.
func (s *ContentService) Create(ctx context.Context, typeName string, data map[string]any) (*Item, error) {
	var item Item
	if err := s.c.doJSON(ctx, "POST", "/api/v0/content/"+url.PathEscape(typeName), data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// Get fetches one item by type and ID. The caller's capabilities determine
// whether drafts are visible (content:read_drafts) or only published items.
func (s *ContentService) Get(ctx context.Context, typeName, id string) (*Item, error) {
	var item Item
	if err := s.c.doJSON(ctx, "GET", "/api/v0/content/"+url.PathEscape(typeName)+"/"+url.PathEscape(id), nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// List returns every item of typeName visible to the caller (drafts included
// only if the caller holds content:read_drafts).
func (s *ContentService) List(ctx context.Context, typeName string) ([]Item, error) {
	var resp struct {
		Items []Item `json:"items"`
	}
	if err := s.c.doJSON(ctx, "GET", "/api/v0/content/"+url.PathEscape(typeName), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Update replaces the field data of an existing item. Requires content:write.
func (s *ContentService) Update(ctx context.Context, typeName, id string, data map[string]any) (*Item, error) {
	var item Item
	if err := s.c.doJSON(ctx, "PUT", "/api/v0/content/"+url.PathEscape(typeName)+"/"+url.PathEscape(id), data, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// Delete removes an item permanently. Requires content:write.
func (s *ContentService) Delete(ctx context.Context, typeName, id string) error {
	return s.c.doJSON(ctx, "DELETE", "/api/v0/content/"+url.PathEscape(typeName)+"/"+url.PathEscape(id), nil, nil)
}

// Version is one historical version of an item, mirroring
// internal/content.Version's wire shape.
type Version struct {
	Version   int            `json:"version"`
	Data      map[string]any `json:"data"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
}

func (s *ContentService) itemPath(typeName, id, suffix string) string {
	return "/api/v0/content/" + url.PathEscape(typeName) + "/" + url.PathEscape(id) + suffix
}

// Publish marks an item published, making it visible to callers without
// content:read_drafts. Requires content:publish.
func (s *ContentService) Publish(ctx context.Context, typeName, id string) (*Item, error) {
	var item Item
	if err := s.c.doJSON(ctx, "POST", s.itemPath(typeName, id, "/publish"), nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// Unpublish reverts an item to draft. Requires content:publish.
func (s *ContentService) Unpublish(ctx context.Context, typeName, id string) (*Item, error) {
	var item Item
	if err := s.c.doJSON(ctx, "POST", s.itemPath(typeName, id, "/unpublish"), nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// ListVersions returns an item's version history, oldest first.
func (s *ContentService) ListVersions(ctx context.Context, typeName, id string) ([]Version, error) {
	var resp struct {
		Versions []Version `json:"versions"`
	}
	if err := s.c.doJSON(ctx, "GET", s.itemPath(typeName, id, "/versions"), nil, &resp); err != nil {
		return nil, err
	}
	return resp.Versions, nil
}

// Rollback restores an item's field data to a prior version, creating a new
// version rather than rewriting history. Requires content:write.
func (s *ContentService) Rollback(ctx context.Context, typeName, id string, version int) (*Item, error) {
	var item Item
	path := s.itemPath(typeName, id, "/rollback/"+strconv.Itoa(version))
	if err := s.c.doJSON(ctx, "POST", path, nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}
