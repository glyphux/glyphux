package media_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/audit"
	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
)

var (
	mediaAdminPrincipal  = &permission.Principal{Role: permission.RoleAdmin}
	mediaViewerPrincipal = &permission.Principal{Role: permission.RoleViewer}
)

func testAPI(t *testing.T) *media.API {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if err := d.Migrate(context.Background(), media.Migrations); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return media.NewAPI(media.NewStore(d), root)
}

// pngBytes builds a minimal valid PNG of the given dimensions.
func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 50, B: 50, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestUploadStoresFileAndMetadata(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	data := pngBytes(t, 40, 20)

	item, err := api.Upload(ctx, mediaAdminPrincipal, "cover.png", "image/png", data)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if item.ID == "" {
		t.Fatal("Upload returned empty ID")
	}
	if item.Width != 40 || item.Height != 20 {
		t.Errorf("dimensions = %dx%d, want 40x20", item.Width, item.Height)
	}
	if item.SizeBytes != int64(len(data)) {
		t.Errorf("SizeBytes = %d, want %d", item.SizeBytes, len(data))
	}
	if item.MimeType != "image/png" {
		t.Errorf("MimeType = %q, want image/png", item.MimeType)
	}

	got, err := api.Get(ctx, item.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Filename != "cover.png" {
		t.Errorf("Filename = %q, want cover.png", got.Filename)
	}
}

func TestUploadRejectsDisallowedMimeType(t *testing.T) {
	api := testAPI(t)
	_, err := api.Upload(context.Background(), mediaAdminPrincipal, "notes.txt", "text/plain", []byte("hello"))
	if !errors.Is(err, media.ErrUnsupportedType) {
		t.Errorf("got %v, want ErrUnsupportedType", err)
	}
}

func TestGetMissingReturnsNotFound(t *testing.T) {
	api := testAPI(t)
	if _, err := api.Get(context.Background(), "nope"); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestListReturnsAllUploads(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	if _, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Upload(ctx, mediaAdminPrincipal, "b.png", "image/png", pngBytes(t, 10, 10)); err != nil {
		t.Fatal(err)
	}
	items, err := api.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("List returned %d items, want 2", len(items))
	}
}

func TestDeleteRemovesRecordAndFile(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	path := api.StoragePath(item)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("uploaded file missing on disk: %v", err)
	}

	if err := api.Delete(ctx, mediaAdminPrincipal, item.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := api.Get(ctx, item.ID); !errors.Is(err, media.ErrNotFound) {
		t.Errorf("Get after delete: got %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file still exists on disk after delete: %v", err)
	}
}

func TestOpenServesStoredBytes(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	data := pngBytes(t, 12, 8)
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", data)
	if err != nil {
		t.Fatal(err)
	}

	rc, _, err := api.Open(ctx, item.ID)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer rc.Close()
	got := new(bytes.Buffer)
	if _, err := got.ReadFrom(rc); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), data) {
		t.Error("Open returned different bytes than uploaded")
	}
}

func TestTransformResizeScalesImagePreservingAspectRatio(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 40, 20))
	if err != nil {
		t.Fatal(err)
	}

	resized, contentType, err := api.Transform(ctx, item.ID, media.TransformOptions{MaxW: 20})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if contentType != "image/png" {
		t.Errorf("contentType = %q, want image/png", contentType)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(resized))
	if err != nil {
		t.Fatalf("decode resized image: %v", err)
	}
	if cfg.Width != 20 || cfg.Height != 10 {
		t.Errorf("resized dimensions = %dx%d, want 20x10 (aspect preserved)", cfg.Width, cfg.Height)
	}
}

// quadImage builds a 4x2 RGBA PNG with a distinct solid color in each half
// (left red, right blue) so crop/rotate correctness can be verified by
// sampling actual output pixels, not just output dimensions.
func quadImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			if x < 2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255}) // left half: red
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255}) // right half: blue
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func decodePNG(t *testing.T, data []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}

func TestTransformCropExtractsTheRequestedRect(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	// Crop just the blue right half (x=2, w=2).
	out, _, err := api.Transform(ctx, item.ID, media.TransformOptions{CropX: 2, CropY: 0, CropW: 2, CropH: 2})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	img := decodePNG(t, out)
	b := img.Bounds()
	if b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("cropped dimensions = %dx%d, want 2x2", b.Dx(), b.Dy())
	}
	r, g, bl, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	if r != 0 || g != 0 || bl == 0 {
		t.Errorf("cropped pixel = (%d,%d,%d), want blue (0,0,max)", r, g, bl)
	}
}

func TestTransformCropRejectsOutOfBoundsRect(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = api.Transform(ctx, item.ID, media.TransformOptions{CropX: 3, CropY: 0, CropW: 5, CropH: 2})
	if !errors.Is(err, media.ErrInvalidTransform) {
		t.Errorf("got %v, want ErrInvalidTransform", err)
	}
}

func TestTransformRotate90MovesLeftPixelToTop(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	out, _, err := api.Transform(ctx, item.ID, media.TransformOptions{Rotate: 90})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	img := decodePNG(t, out)
	b := img.Bounds()
	// Source was 4 wide x 2 tall; a 90-degree rotation swaps those.
	if b.Dx() != 2 || b.Dy() != 4 {
		t.Fatalf("rotated dimensions = %dx%d, want 2x4", b.Dx(), b.Dy())
	}
	// The source's left half (red) must now be at the top of the rotated
	// image, and the right half (blue) at the bottom.
	topRed, _, topBlue, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	if topRed == 0 || topBlue != 0 {
		t.Errorf("top-left pixel after 90-deg rotate should be red (from source's left half); got r=%d b=%d", topRed, topBlue)
	}
	bottomRed, _, bottomBlue, _ := img.At(b.Min.X, b.Max.Y-1).RGBA()
	if bottomBlue == 0 || bottomRed != 0 {
		t.Errorf("bottom-left pixel after 90-deg rotate should be blue (from source's right half); got r=%d b=%d", bottomRed, bottomBlue)
	}
}

func TestTransformRotateRejectsUnsupportedAngle(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = api.Transform(ctx, item.ID, media.TransformOptions{Rotate: 45})
	if !errors.Is(err, media.ErrInvalidTransform) {
		t.Errorf("got %v, want ErrInvalidTransform", err)
	}
}

func TestTransformFormatConvertsRegardlessOfSource(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	out, contentType, err := api.Transform(ctx, item.ID, media.TransformOptions{Format: "jpeg"})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want image/jpeg", contentType)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("decoded format = %q, want jpeg", format)
	}
	if cfg.Width != 4 || cfg.Height != 2 {
		t.Errorf("dimensions = %dx%d, want 4x2 (format conversion alone doesn't resize)", cfg.Width, cfg.Height)
	}
}

func TestTransformRejectsUnsupportedFormat(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = api.Transform(ctx, item.ID, media.TransformOptions{Format: "webp"})
	if !errors.Is(err, media.ErrInvalidTransform) {
		t.Errorf("got %v, want ErrInvalidTransform", err)
	}
}

// TestTransformComposesCropRotateResizeFormatInOnePipeline proves the four
// stages run together, in order, on a single call — not just individually.
func TestTransformComposesCropRotateResizeFormatInOnePipeline(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "quad.png", "image/png", quadImage(t))
	if err != nil {
		t.Fatal(err)
	}

	// Crop the blue right half (2x2) -> rotate 90 (now 2x2, still all blue)
	// -> resize to max 10x10 (aspect 1:1, stays 10x10 since scaledDimensions
	// scales up? no - scaledDimensions only ever fits within max, upscaling
	// included since maxW/maxH act as a cap on the ratio) -> encode jpeg.
	out, contentType, err := api.Transform(ctx, item.ID, media.TransformOptions{
		CropX: 2, CropY: 0, CropW: 2, CropH: 2,
		Rotate: 90,
		MaxW:   10, MaxH: 10,
		Format: "jpeg",
	})
	if err != nil {
		t.Fatalf("Transform: %v", err)
	}
	if contentType != "image/jpeg" {
		t.Errorf("contentType = %q, want image/jpeg", contentType)
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("format = %q, want jpeg", format)
	}
	if cfg.Width != 10 || cfg.Height != 10 {
		t.Errorf("dimensions = %dx%d, want 10x10", cfg.Width, cfg.Height)
	}
}

func TestUploadDefaultsTagsToEmptyNotNilSlice(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	if item.Tags == nil {
		t.Fatal("Tags is nil, want an empty (non-nil) slice so it serializes as [] not null")
	}
	if len(item.Tags) != 0 {
		t.Errorf("Tags = %v, want empty", item.Tags)
	}
}

func TestUpdateMetadataPersistsTagsAndSourceAttribution(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}

	updated, err := api.UpdateMetadata(ctx, mediaAdminPrincipal, item.ID, media.MetadataUpdate{
		AltText:     "a red square",
		Tags:        []string{"stock", "hero"},
		Source:      "https://example.com/photo",
		Attribution: "Photo by Jane Doe",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if updated.AltText != "a red square" {
		t.Errorf("AltText = %q, want %q", updated.AltText, "a red square")
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "stock" || updated.Tags[1] != "hero" {
		t.Errorf("Tags = %v, want [stock hero]", updated.Tags)
	}
	if updated.Source != "https://example.com/photo" {
		t.Errorf("Source = %q, want %q", updated.Source, "https://example.com/photo")
	}
	if updated.Attribution != "Photo by Jane Doe" {
		t.Errorf("Attribution = %q, want %q", updated.Attribution, "Photo by Jane Doe")
	}

	// Round-trips through a fresh Get too, not just the returned value.
	fetched, err := api.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched.Tags) != 2 || fetched.Source != "https://example.com/photo" || fetched.Attribution != "Photo by Jane Doe" {
		t.Errorf("Get after UpdateMetadata = %+v, want tags/source/attribution to have persisted", fetched)
	}
}

func TestUpdateMetadataReturnsNotFoundForUnknownID(t *testing.T) {
	api := testAPI(t)
	_, err := api.UpdateMetadata(context.Background(), mediaAdminPrincipal, "nope", media.MetadataUpdate{})
	if !errors.Is(err, media.ErrNotFound) {
		t.Errorf("got %v, want ErrNotFound", err)
	}
}

func TestUpdateMetadataRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}

	update := media.MetadataUpdate{AltText: "hijacked"}
	if _, err := api.UpdateMetadata(ctx, mediaViewerPrincipal, item.ID, update); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer UpdateMetadata: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.UpdateMetadata(ctx, nil, item.ID, update); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous UpdateMetadata: got %v, want permission.ErrDenied", err)
	}
	got, err := api.Get(ctx, item.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.AltText == "hijacked" {
		t.Error("denied UpdateMetadata call must not have taken effect")
	}
}

// --- Domain-API boundary capability enforcement (PRD §10.5) ---
//
// These tests call the media domain API directly — bypassing internal/api's
// HTTP transport and its requireCapability check entirely — to prove the
// domain API rejects an under-privileged or anonymous caller on its own.

func TestUploadRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	data := pngBytes(t, 4, 4)

	if _, err := api.Upload(ctx, mediaViewerPrincipal, "a.png", "image/png", data); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Upload: got %v, want permission.ErrDenied", err)
	}
	if _, err := api.Upload(ctx, nil, "a.png", "image/png", data); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Upload: got %v, want permission.ErrDenied", err)
	}
	items, err := api.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("List = %d items, want 0 (denied uploads must not persist)", len(items))
	}
}

func TestDeleteRejectsUnderPrivilegedAndAnonymousPrincipal(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, mediaAdminPrincipal, "a.png", "image/png", pngBytes(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}

	if err := api.Delete(ctx, mediaViewerPrincipal, item.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("viewer Delete: got %v, want permission.ErrDenied", err)
	}
	if err := api.Delete(ctx, nil, item.ID); !errors.Is(err, permission.ErrDenied) {
		t.Errorf("anonymous Delete: got %v, want permission.ErrDenied", err)
	}
	// Item must still exist: neither rejected Delete call took effect.
	if _, err := api.Get(ctx, item.ID); err != nil {
		t.Errorf("item should still exist after denied deletes: %v", err)
	}
}

// ---- Ticket T7 (gap 4): item-level CRUD auditing via the WithAudit option ----

// testAuditAPI wires a media API over a fresh SQLite DB (audit migration
// included) with a live audit logger attached.
func testAuditAPI(t *testing.T) (*media.API, *db.DB) {
	t.Helper()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, media.Migrations...), audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	return media.NewAPI(media.NewStore(d), root, media.WithAudit(audit.NewLogger(d))), d
}

func listMediaAuditRows(t *testing.T, d *db.DB) []audit.Record {
	t.Helper()
	rows, err := audit.NewLogger(d).ListByPlugin(context.Background(), "media")
	if err != nil {
		t.Fatal(err)
	}
	return rows
}

// TestAuditRecordsMediaWritesAndSkipsReads: GIVEN a media API wired with a
// live audit logger, WHEN Upload/UpdateMetadata/Delete run, THEN one row
// per write lands stamped plugin "media" with the pinned action; reads
// (Get/List/Open) write nothing.
func TestAuditRecordsMediaWritesAndSkipsReads(t *testing.T) {
	ctx := context.Background()
	api, d := testAuditAPI(t)

	item, err := api.Upload(ctx, mediaAdminPrincipal, "pic.png", "image/png", pngBytes(t, 4, 4))
	if err != nil {
		t.Fatal(err)
	}
	rows := listMediaAuditRows(t, d)
	if len(rows) != 1 || rows[0].Action != audit.ActionMediaUploaded {
		t.Fatalf("after Upload: rows = %+v, want one %s", rows, audit.ActionMediaUploaded)
	}
	var createDetail map[string]string
	if err := json.Unmarshal([]byte(rows[0].Detail), &createDetail); err != nil {
		t.Fatalf("parse detail %q: %v", rows[0].Detail, err)
	}
	if createDetail["role"] != "admin" || createDetail["item_id"] != item.ID || createDetail["type"] != "media" {
		t.Errorf("upload detail = %v, want role=admin item_id=%s type=media", createDetail, item.ID)
	}

	// Reads write nothing.
	if _, err := api.Get(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := api.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, _, err := api.Open(ctx, item.ID); err != nil {
		t.Fatal(err)
	}
	if len(listMediaAuditRows(t, d)) != 1 {
		t.Error("reads must not write audit rows")
	}

	if _, err := api.UpdateMetadata(ctx, mediaAdminPrincipal, item.ID, media.MetadataUpdate{AltText: "a red square"}); err != nil {
		t.Fatal(err)
	}
	if err := api.Delete(ctx, mediaAdminPrincipal, item.ID); err != nil {
		t.Fatal(err)
	}

	want := []string{audit.ActionMediaUploaded, audit.ActionMediaUpdated, audit.ActionMediaDeleted}
	rows = listMediaAuditRows(t, d)
	if len(rows) != len(want) {
		t.Fatalf("rows = %d, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		if rows[i].Action != w {
			t.Errorf("row %d action = %q, want %q", i, rows[i].Action, w)
		}
	}
}

// TestAuditNilMediaLoggerIsANoOp: GIVEN WithAudit(nil), WHEN any write
// runs, THEN no audit rows appear and behavior is unchanged (no panic).
func TestAuditNilMediaLoggerIsANoOp(t *testing.T) {
	ctx := context.Background()
	d, err := db.OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	migs := append(append([]db.Migration{}, media.Migrations...), audit.Migrations...)
	if err := d.Migrate(context.Background(), migs); err != nil {
		t.Fatal(err)
	}
	api := media.NewAPI(media.NewStore(d), t.TempDir(), media.WithAudit(nil))

	if _, err := api.Upload(ctx, mediaAdminPrincipal, "pic.png", "image/png", pngBytes(t, 4, 4)); err != nil {
		t.Fatal(err)
	}
	if rows := listMediaAuditRows(t, d); len(rows) != 0 {
		t.Errorf("WithAudit(nil) must write no rows, got %d", len(rows))
	}
}
