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

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/permission"
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

// MetadataUpdate is the set of editable fields on a stored media item —
// alt text, tags, and source/attribution (PRD §11.4) — distinct from the
// immutable upload-time fields (filename, dimensions, mime type). It is a
// full replace, not a partial patch: the caller sends back every field it
// wants kept, matching how the admin UI's edit dialog holds the whole
// editable record in one form.
type MetadataUpdate struct {
	AltText     string
	Tags        []string
	Source      string
	Attribution string
}

// API is the media domain API — the only sanctioned way for clients to touch
// media state.
type API struct {
	items *Store
	root  string        // local-FS storage root, e.g. <data-dir>/media
	audit *audit.Logger // nil unless WithAudit wired (Ticket T7)
}

// Option configures optional API behavior beyond the required kernel store.
type Option func(*API)

// WithAudit wires an audit logger so every media write (upload/update
// metadata/delete) records one row via the media recorder (Ticket T7 / gap
// 4). Nil — the zero value — is a byte-identical no-op: no rows, no
// behavior change, no panic. Reads are deliberately un-audited.
func WithAudit(logger *audit.Logger) Option {
	return func(a *API) { a.audit = logger }
}

// NewAPI wires the domain API to the kernel store and a local-FS storage root.
// The root is created on first use, not at construction, so tests and dry
// runs never touch disk unnecessarily.
func NewAPI(items *Store, root string, opts ...Option) *API {
	a := &API{items: items, root: root}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// auditItem best-effort logs one media write. A nil logger is a no-op; a
// logging failure never masks the write itself. The domain boundary sees
// role-only principals, so Actor.ID is always "" here (owner-confirmed).
func (a *API) auditItem(ctx context.Context, action string, principal *permission.Principal, id string) {
	if a.audit == nil {
		return
	}
	_ = a.audit.RecordMedia(ctx, action, audit.Actor{Role: permission.RoleOf(principal)}, id)
}

// Upload validates data's MIME type, decodes image dimensions where possible,
// stores the bytes on the local-FS adapter, and records the metadata.
// principal must hold media:write — checked here at the domain-API
// boundary (PRD §10.5) independent of whether a transport handler already
// checked; a nil principal (anonymous) is always denied.
func (a *API) Upload(ctx context.Context, principal *permission.Principal, filename, mimeType string, data []byte) (*Item, error) {
	if !permission.AllowsPrincipal(principal, permission.MediaWrite) {
		return nil, permission.ErrDenied
	}
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
		Width: width, Height: height, Tags: []string{}, CreatedAt: now, UpdatedAt: now,
	}
	if err := a.items.insert(ctx, record{
		ID: id, Filename: filename, MimeType: mimeType, SizeBytes: item.SizeBytes,
		Width: width, Height: height, StoragePath: storagePath, Tags: []string{},
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		_ = os.Remove(filepath.Join(a.root, storagePath))
		return nil, err
	}
	a.auditItem(ctx, audit.ActionMediaUploaded, principal, item.ID)
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
// ErrNotFound if it does not exist. principal must hold media:write,
// checked here at the domain-API boundary.
func (a *API) Delete(ctx context.Context, principal *permission.Principal, id string) error {
	if !permission.AllowsPrincipal(principal, permission.MediaWrite) {
		return permission.ErrDenied
	}
	r, err := a.items.getByID(ctx, id)
	if err != nil {
		return err
	}
	if err := a.items.delete(ctx, id); err != nil {
		return err
	}
	_ = os.Remove(filepath.Join(a.root, r.StoragePath))
	a.auditItem(ctx, audit.ActionMediaDeleted, principal, id)
	return nil
}

// UpdateMetadata replaces id's editable metadata — alt text, tags, and
// source/attribution — and returns the updated item. Returns ErrNotFound if
// id does not exist. principal must hold media:write, checked here at the
// domain-API boundary (PRD §10.5).
func (a *API) UpdateMetadata(ctx context.Context, principal *permission.Principal, id string, update MetadataUpdate) (*Item, error) {
	if !permission.AllowsPrincipal(principal, permission.MediaWrite) {
		return nil, permission.ErrDenied
	}
	if _, err := a.items.getByID(ctx, id); err != nil {
		return nil, err
	}
	if err := a.items.updateMetadata(ctx, id, update.AltText, update.Tags, update.Source, update.Attribution, time.Now().UTC()); err != nil {
		return nil, err
	}
	a.auditItem(ctx, audit.ActionMediaUpdated, principal, id)
	return a.Get(ctx, id)
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

// ErrInvalidTransform reports a malformed crop rectangle, an unsupported
// rotation angle, or an unsupported output format requested of Transform.
var ErrInvalidTransform = errors.New("invalid media transform")

// TransformOptions describes an image transform pipeline applied in a fixed
// order — crop, then rotate, then resize, then re-encode in an explicit
// output format — so a caller can request any combination in one call, the
// same way width/height have always been passed as HTTP query params on
// the file-serving route. A zero value for a stage skips it: CropW == 0 &&
// CropH == 0 skips crop, Rotate == 0 skips rotation, MaxW == 0 && MaxH == 0
// skips resize, and Format == "" keeps the source's own encoding (jpeg
// stays jpeg; anything else — png, gif — encodes as png), matching the
// pre-existing behavior before Format became an explicit, overridable
// parameter.
type TransformOptions struct {
	CropX, CropY, CropW, CropH int
	Rotate                     int
	MaxW, MaxH                 int
	Format                     string
}

// Transform decodes a stored image and applies crop -> rotate -> resize ->
// format-encode, in that order, returning the transformed bytes and the
// content type that actually matches those bytes (not necessarily the
// stored original's MIME type — a caller can request a different output
// format than the source). The stored original is untouched.
func (a *API) Transform(ctx context.Context, id string, opts TransformOptions) ([]byte, string, error) {
	rc, _, err := a.Open(ctx, id)
	if err != nil {
		return nil, "", err
	}
	defer rc.Close()

	src, srcFormat, err := image.Decode(rc)
	if err != nil {
		return nil, "", fmt.Errorf("decode media image: %w", err)
	}

	img := src
	if opts.CropW > 0 || opts.CropH > 0 {
		img, err = cropImage(img, opts.CropX, opts.CropY, opts.CropW, opts.CropH)
		if err != nil {
			return nil, "", err
		}
	}

	if opts.Rotate != 0 {
		switch opts.Rotate {
		case 90, 180, 270:
			img = rotateImage(img, opts.Rotate)
		default:
			return nil, "", fmt.Errorf("%w: rotate must be 0, 90, 180, or 270, got %d", ErrInvalidTransform, opts.Rotate)
		}
	}

	if opts.MaxW > 0 || opts.MaxH > 0 {
		bounds := img.Bounds()
		w, h := scaledDimensions(bounds.Dx(), bounds.Dy(), opts.MaxW, opts.MaxH)
		img = nearestNeighborResize(img, w, h)
	}

	format := opts.Format
	if format == "" {
		if srcFormat == "jpeg" {
			format = "jpeg"
		} else {
			format = "png"
		}
	}

	var buf bytes.Buffer
	switch format {
	case "jpeg":
		if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
			return nil, "", fmt.Errorf("encode transformed jpeg: %w", err)
		}
		return buf.Bytes(), "image/jpeg", nil
	case "png":
		if err := png.Encode(&buf, img); err != nil {
			return nil, "", fmt.Errorf("encode transformed png: %w", err)
		}
		return buf.Bytes(), "image/png", nil
	default:
		return nil, "", fmt.Errorf("%w: format must be \"jpeg\" or \"png\", got %q", ErrInvalidTransform, format)
	}
}

// cropImage returns the sub-rectangle (x, y, x+w, y+h) of src, relative to
// its own bounds. Returns ErrInvalidTransform if the rectangle is empty or
// falls outside src's bounds.
func cropImage(src image.Image, x, y, w, h int) (image.Image, error) {
	b := src.Bounds()
	if w <= 0 || h <= 0 || x < 0 || y < 0 || x+w > b.Dx() || y+h > b.Dy() {
		return nil, fmt.Errorf("%w: crop rect (%d,%d,%d,%d) out of bounds for %dx%d image", ErrInvalidTransform, x, y, w, h, b.Dx(), b.Dy())
	}
	rect := image.Rect(b.Min.X+x, b.Min.Y+y, b.Min.X+x+w, b.Min.Y+y+h)
	// Use SubImage where the concrete type supports it (zero-copy view);
	// fall back to a manual pixel copy for image.Image implementations that
	// don't (e.g. some decoders' internal types).
	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	if si, ok := src.(subImager); ok {
		return si.SubImage(rect), nil
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for yy := 0; yy < h; yy++ {
		for xx := 0; xx < w; xx++ {
			dst.Set(xx, yy, src.At(rect.Min.X+xx, rect.Min.Y+yy))
		}
	}
	return dst, nil
}

// rotateImage rotates src clockwise by degrees, which must be 90, 180, or
// 270 (checked by the caller) — the increments the stdlib image package
// makes cheap and lossless via pixel remapping; arbitrary angles would need
// interpolation and introduce quality loss, which the pipeline doesn't need
// for V1 (PRD §11.4's "rotate" is satisfied by fixed increments).
func rotateImage(src image.Image, degrees int) image.Image {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	switch degrees {
	case 90:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(h-1-y, x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 180:
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(w-1-x, h-1-y, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	case 270:
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.Set(y, w-1-x, src.At(b.Min.X+x, b.Min.Y+y))
			}
		}
		return dst
	default:
		return src
	}
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
	tags := r.Tags
	if tags == nil {
		tags = []string{}
	}
	return &Item{
		ID: r.ID, Filename: r.Filename, MimeType: r.MimeType, SizeBytes: r.SizeBytes,
		Width: r.Width, Height: r.Height, AltText: r.AltText,
		Tags: tags, Source: r.Source, Attribution: r.Attribution,
		CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt,
	}
}

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
