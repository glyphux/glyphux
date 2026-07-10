package media_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/glyphux/glyphux/internal/db"
	"github.com/glyphux/glyphux/internal/media"
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

	item, err := api.Upload(ctx, "cover.png", "image/png", data)
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
	_, err := api.Upload(context.Background(), "notes.txt", "text/plain", []byte("hello"))
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
	if _, err := api.Upload(ctx, "a.png", "image/png", pngBytes(t, 10, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := api.Upload(ctx, "b.png", "image/png", pngBytes(t, 10, 10)); err != nil {
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
	item, err := api.Upload(ctx, "a.png", "image/png", pngBytes(t, 10, 10))
	if err != nil {
		t.Fatal(err)
	}
	path := api.StoragePath(item)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("uploaded file missing on disk: %v", err)
	}

	if err := api.Delete(ctx, item.ID); err != nil {
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
	item, err := api.Upload(ctx, "a.png", "image/png", data)
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

func TestResizeScalesImagePreservingAspectRatio(t *testing.T) {
	api := testAPI(t)
	ctx := context.Background()
	item, err := api.Upload(ctx, "a.png", "image/png", pngBytes(t, 40, 20))
	if err != nil {
		t.Fatal(err)
	}

	resized, contentType, err := api.Resize(ctx, item.ID, 20, 0)
	if err != nil {
		t.Fatalf("Resize: %v", err)
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
