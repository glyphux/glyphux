// Package media is the kernel media engine (Phase 1 slice 1.6): upload,
// validate, store, and serve media assets. Storage is a local-FS adapter
// rooted under the daemon's data directory — no cloud dependency required
// (Principle 9). Clients touch media state only through this API, never the
// filesystem or database directly (Principle 4).
package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ErrUnsupportedType reports an upload whose MIME type is not in the allowed
// set for media assets.
var ErrUnsupportedType = errors.New("unsupported media type")

// allowedTypes is the Phase-1 upload allowlist: images only, matching the
// `accept: [image]` contract shape for media fields.
var allowedTypes = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/gif":  ".gif",
}

// Item is a single stored media asset and its metadata.
type Item struct {
	ID        string    `json:"id"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	SizeBytes int64     `json:"size_bytes"`
	Width     int       `json:"width"`
	Height    int       `json:"height"`
	AltText   string    `json:"alt_text"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// API is the media domain API — the only sanctioned way for clients to touch
// media state.
type API struct {
	items *Store
	root  string // local-FS storage root, e.g. <data-dir>/media
}

// NewAPI wires the domain API to the kernel store and a local-FS storage root.
// The root is created on first use, not at construction, so tests and dry
// runs never touch disk unnecessarily.
func NewAPI(items *Store, root string) *API {
	return &API{items: items, root: root}
}

// Upload validates data's MIME type, decodes image dimensions where possible,
// stores the bytes on the local-FS adapter, and records the metadata.
func (a *API) Upload(ctx context.Context, filename, mimeType string, data []byte) (*Item, error) {
	ext, ok := allowedTypes[mimeType]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedType, mimeType)
	}
	if err := os.MkdirAll(a.root, 0o755); err != nil {
		return nil, fmt.Errorf("create media storage root: %w", err)
	}

	id := newID()
	storagePath := id + ext
	if err := os.WriteFile(filepath.Join(a.root, storagePath), data, 0o644); err != nil {
		return nil, fmt.Errorf("write media file: %w", err)
	}

	width, height := 0, 0
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(data)); err == nil {
		width, height = cfg.Width, cfg.Height
	}

	now := time.Now().UTC()
	item := &Item{
		ID: id, Filename: filename, MimeType: mimeType, SizeBytes: int64(len(data)),
		Width: width, Height: height, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.items.insert(ctx, record{
		ID: id, Filename: filename, MimeType: mimeType, SizeBytes: item.SizeBytes,
		Width: width, Height: height, StoragePath: storagePath,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		_ = os.Remove(filepath.Join(a.root, storagePath))
		return nil, err
	}
	return item, nil
}

// Get returns a media item's metadata, or ErrNotFound.
func (a *API) Get(ctx context.Context, id string) (*Item, error) {
	r, err := a.items.getByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return recordToItem(r), nil
}

// List returns every media item, oldest first.
func (a *API) List(ctx context.Context) ([]*Item, error) {
	records, err := a.items.list(ctx)
	if err != nil {
		return nil, err
	}
	items := make([]*Item, 0, len(records))
	for _, r := range records {
		items = append(items, recordToItem(r))
	}
	return items, nil
}

// Delete removes a media item's metadata and its stored file. Returns
// ErrNotFound if it does not exist.
func (a *API) Delete(ctx context.Context, id string) error {
	r, err := a.items.getByID(ctx, id)
	if err != nil {
		return err
	}
	if err := a.items.delete(ctx, id); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(a.root, r.StoragePath))
	return nil
}

// Open returns a reader over a media item's stored bytes and its metadata.
// The caller must close the reader.
func (a *API) Open(ctx context.Context, id string) (io.ReadCloser, *Item, error) {
	r, err := a.items.getByID(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(filepath.Join(a.root, r.StoragePath))
	if err != nil {
		return nil, nil, fmt.Errorf("open media file: %w", err)
	}
	return f, recordToItem(r), nil
}

// StoragePath returns the on-disk path of a stored item — a test/inspection
// helper, not part of the domain API contract.
func (a *API) StoragePath(item *Item) string {
	r, err := a.items.getByID(context.Background(), item.ID)
	if err != nil {
		return ""
	}
	return filepath.Join(a.root, r.StoragePath)
}

// Resize decodes a stored image and scales it to fit within (maxW, maxH),
// preserving aspect ratio. A zero dimension is unconstrained. It returns the
// re-encoded bytes and their content type; the stored original is untouched.
func (a *API) Resize(ctx context.Context, id string, maxW, maxH int) ([]byte, string, error) {
	rc, item, err := a.Open(ctx, id)
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()

	src, format, err := image.Decode(rc)
	if err != nil {
		return nil, "", fmt.Errorf("decode media image: %w", err)
	}

	bounds := src.Bounds()
	w, h := scaledDimensions(bounds.Dx(), bounds.Dy(), maxW, maxH)
	dst := nearestNeighborResize(src, w, h)

	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("encode resized jpeg: %w", err)
		}
	default:
		if err := png.Encode(&buf, dst); err != nil {
			return nil, "", fmt.Errorf("encode resized png: %w", err)
		}
	}
	return buf.Bytes(), item.MimeType, nil
}

// scaledDimensions computes output dimensions that fit within maxW x maxH
// while preserving the source aspect ratio. A zero max dimension is treated
// as unconstrained on that axis.
func scaledDimensions(srcW, srcH, maxW, maxH int) (int, int) {
	if maxW <= 0 && maxH <= 0 {
		return srcW, srcH
	}
	ratio := float64(srcW) / float64(srcH)
	switch {
	case maxW > 0 && maxH > 0:
		if float64(maxW)/ratio <= float64(maxH) {
			return maxW, int(float64(maxW) / ratio)
		}
		return int(float64(maxH) * ratio), maxH
	case maxW > 0:
		return maxW, int(float64(maxW) / ratio)
	default:
		return int(float64(maxH) * ratio), maxH
	}
}

// nearestNeighborResize scales src to w x h using nearest-neighbor sampling —
// dependency-free and adequate for thumbnail-grade output.
func nearestNeighborResize(src image.Image, w, h int) *image.RGBA {
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	bounds := src.Bounds()
	sw, sh := bounds.Dx(), bounds.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		sy := bounds.Min.Y + y*sh/h
		for x := 0; x < w; x++ {
			sx := bounds.Min.X + x*sw/w
			dst.Set(x, y, src.At(sx, sy))
		}
	}
	return dst
}

func recordToItem(r record) *Item {
	return &Item{
		ID: r.ID, Filename: r.Filename, MimeType: r.MimeType, SizeBytes: r.SizeBytes,
		Width: r.Width, Height: r.Height, AltText: r.AltText,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
