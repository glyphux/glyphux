package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"time"
)

// MediaItem is a media library entry, mirroring internal/media.Item's wire
// shape.
type MediaItem struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename"`
	MimeType    string    `json:"mime_type"`
	SizeBytes   int64     `json:"size_bytes"`
	Width       int       `json:"width"`
	Height      int       `json:"height"`
	AltText     string    `json:"alt_text"`
	Tags        []string  `json:"tags"`
	Source      string    `json:"source"`
	Attribution string    `json:"attribution"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// MediaTransform describes an image transform pipeline — crop, then
// rotate, then resize, then re-encode in an explicit format — mirroring
// internal/media.TransformOptions and the GET .../file query params it's
// built from. A zero value for a field skips that stage; see
// TransformOptions' doc for exact semantics (e.g. rotate must be 0, 90,
// 180, or 270).
type MediaTransform struct {
	CropX, CropY, CropW, CropH int
	Rotate                     int
	MaxW, MaxH                 int
	Format                     string
}

// MediaMetadataUpdate is the editable metadata on a media item — alt text,
// tags, and source/attribution (PRD §11.4) — mirroring
// internal/media.MetadataUpdate. It replaces every field, so callers
// should send back the full set they want kept.
type MediaMetadataUpdate struct {
	AltText     string   `json:"alt_text"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"`
	Attribution string   `json:"attribution"`
}

// MediaService wraps /api/v0/media.
type MediaService struct{ c *Client }

// Upload adds data to the media library under filename. Requires
// media:write.
func (s *MediaService) Upload(ctx context.Context, filename string, data []byte) (*MediaItem, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("glyphux: build upload: %w", err)
	}
	if _, err := part.Write(data); err != nil {
		return nil, fmt.Errorf("glyphux: build upload: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("glyphux: build upload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", s.c.baseURL+"/api/v0/media", &body)
	if err != nil {
		return nil, fmt.Errorf("glyphux: build request: %w", err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	if s.c.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.c.token)
	}

	resp, err := s.c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("glyphux: request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("glyphux: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, newAPIError(resp.StatusCode, respBody)
	}
	var item MediaItem
	if err := json.Unmarshal(respBody, &item); err != nil {
		return nil, fmt.Errorf("glyphux: decode response: %w", err)
	}
	return &item, nil
}

// Get fetches one media item's metadata by ID.
func (s *MediaService) Get(ctx context.Context, id string) (*MediaItem, error) {
	var item MediaItem
	if err := s.c.doJSON(ctx, "GET", "/api/v0/media/"+url.PathEscape(id), nil, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// List returns every media item's metadata.
func (s *MediaService) List(ctx context.Context) ([]MediaItem, error) {
	var resp struct {
		Items []MediaItem `json:"items"`
	}
	if err := s.c.doJSON(ctx, "GET", "/api/v0/media", nil, &resp); err != nil {
		return nil, err
	}
	return resp.Items, nil
}

// Delete removes a media item and its stored file. Requires media:write.
func (s *MediaService) Delete(ctx context.Context, id string) error {
	return s.c.doJSON(ctx, "DELETE", "/api/v0/media/"+url.PathEscape(id), nil, nil)
}

// UpdateMetadata replaces a media item's editable metadata (alt text, tags,
// source/attribution) and returns the updated item. Requires media:write.
func (s *MediaService) UpdateMetadata(ctx context.Context, id string, update MediaMetadataUpdate) (*MediaItem, error) {
	var item MediaItem
	if err := s.c.doJSON(ctx, "PATCH", "/api/v0/media/"+url.PathEscape(id), update, &item); err != nil {
		return nil, err
	}
	return &item, nil
}

// File downloads the original stored bytes for a media item, along with
// their content type.
func (s *MediaService) File(ctx context.Context, id string) ([]byte, string, error) {
	return s.fetchFile(ctx, "/api/v0/media/"+url.PathEscape(id)+"/file")
}

// FileResized downloads the media item resized to fit within width x height,
// along with the resulting content type.
func (s *MediaService) FileResized(ctx context.Context, id string, width, height int) ([]byte, string, error) {
	q := url.Values{}
	q.Set("w", fmt.Sprint(width))
	q.Set("h", fmt.Sprint(height))
	return s.fetchFile(ctx, "/api/v0/media/"+url.PathEscape(id)+"/file?"+q.Encode())
}

// FileTransformed downloads the media item with a crop/rotate/resize/format
// pipeline applied (internal/api/media.go handleMediaFile's transform query
// params), returning the resulting bytes and their actual content type.
func (s *MediaService) FileTransformed(ctx context.Context, id string, t MediaTransform) ([]byte, string, error) {
	q := url.Values{}
	if t.CropW > 0 || t.CropH > 0 {
		q.Set("crop_x", fmt.Sprint(t.CropX))
		q.Set("crop_y", fmt.Sprint(t.CropY))
		q.Set("crop_w", fmt.Sprint(t.CropW))
		q.Set("crop_h", fmt.Sprint(t.CropH))
	}
	if t.Rotate != 0 {
		q.Set("rotate", fmt.Sprint(t.Rotate))
	}
	if t.MaxW > 0 || t.MaxH > 0 {
		q.Set("w", fmt.Sprint(t.MaxW))
		q.Set("h", fmt.Sprint(t.MaxH))
	}
	if t.Format != "" {
		q.Set("format", t.Format)
	}
	path := "/api/v0/media/" + url.PathEscape(id) + "/file"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	return s.fetchFile(ctx, path)
}

func (s *MediaService) fetchFile(ctx context.Context, path string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", s.c.baseURL+path, nil)
	if err != nil {
		return nil, "", fmt.Errorf("glyphux: build request: %w", err)
	}
	if s.c.token != "" {
		req.Header.Set("Authorization", "Bearer "+s.c.token)
	}
	resp, err := s.c.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("glyphux: request: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("glyphux: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, "", newAPIError(resp.StatusCode, respBody)
	}
	return respBody, resp.Header.Get("Content-Type"), nil
}
