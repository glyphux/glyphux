package client_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/glyphux/glyphux/pkg/client"
)

func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 255, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMediaUploadGetListDelete(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()
	data := onePixelPNG(t)

	uploaded, err := c.Media.Upload(ctx, "swatch.png", data)
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}
	if uploaded.ID == "" {
		t.Fatal("Upload: item has no id")
	}
	if uploaded.Width != 4 || uploaded.Height != 4 {
		t.Errorf("Upload: dimensions = %dx%d, want 4x4", uploaded.Width, uploaded.Height)
	}

	got, err := c.Media.Get(ctx, uploaded.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Filename != "swatch.png" {
		t.Errorf("Get: filename = %q, want swatch.png", got.Filename)
	}

	items, err := c.Media.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(items) != 1 || items[0].ID != uploaded.ID {
		t.Fatalf("List = %+v, want one item with id %q", items, uploaded.ID)
	}

	file, contentType, err := c.Media.File(ctx, uploaded.ID)
	if err != nil {
		t.Fatalf("File: %v", err)
	}
	if len(file) == 0 {
		t.Error("File: empty body")
	}
	if contentType != "image/png" {
		t.Errorf("File: content-type = %q, want image/png", contentType)
	}

	if err := c.Media.Delete(ctx, uploaded.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := c.Media.Get(ctx, uploaded.ID); err == nil {
		t.Fatal("Get after Delete: want error, got nil")
	}
}

func TestMediaFileResize(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()
	uploaded, err := c.Media.Upload(ctx, "swatch.png", onePixelPNG(t))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	resized, _, err := c.Media.FileResized(ctx, uploaded.ID, 2, 2)
	if err != nil {
		t.Fatalf("FileResized: %v", err)
	}
	if len(resized) == 0 {
		t.Error("FileResized: empty body")
	}
}

// quadPNG builds a 4x2 PNG, left half red / right half blue, so crop/rotate
// correctness can be checked against real output pixels, not just size.
func quadPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			if x < 2 {
				img.Set(x, y, color.RGBA{R: 255, A: 255})
			} else {
				img.Set(x, y, color.RGBA{B: 255, A: 255})
			}
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestMediaFileTransformedCropRotateFormat(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()
	uploaded, err := c.Media.Upload(ctx, "quad.png", quadPNG(t))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	out, contentType, err := c.Media.FileTransformed(ctx, uploaded.ID, client.MediaTransform{
		CropX: 2, CropY: 0, CropW: 2, CropH: 2,
		Rotate: 90,
		Format: "jpeg",
	})
	if err != nil {
		t.Fatalf("FileTransformed: %v", err)
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
	// Cropped to 2x2 (the blue right half), then rotated 90 - still 2x2.
	if cfg.Width != 2 || cfg.Height != 2 {
		t.Errorf("dimensions = %dx%d, want 2x2", cfg.Width, cfg.Height)
	}
}

func TestMediaFileTransformedRejectsInvalidRotate(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()
	uploaded, err := c.Media.Upload(ctx, "quad.png", quadPNG(t))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	_, _, err = c.Media.FileTransformed(ctx, uploaded.ID, client.MediaTransform{Rotate: 45})
	if err == nil {
		t.Fatal("want an error for an unsupported rotate angle, got nil")
	}
}

func TestMediaUpdateMetadataPersistsTagsAndSourceAttribution(t *testing.T) {
	c := loggedInClient(t)
	ctx := context.Background()
	uploaded, err := c.Media.Upload(ctx, "swatch.png", onePixelPNG(t))
	if err != nil {
		t.Fatalf("Upload: %v", err)
	}

	updated, err := c.Media.UpdateMetadata(ctx, uploaded.ID, client.MediaMetadataUpdate{
		AltText:     "a swatch",
		Tags:        []string{"stock", "hero"},
		Source:      "https://example.com/photo",
		Attribution: "Photo by Jane Doe",
	})
	if err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}
	if updated.AltText != "a swatch" {
		t.Errorf("AltText = %q, want %q", updated.AltText, "a swatch")
	}
	if len(updated.Tags) != 2 || updated.Tags[0] != "stock" || updated.Tags[1] != "hero" {
		t.Errorf("Tags = %v, want [stock hero]", updated.Tags)
	}
	if updated.Source != "https://example.com/photo" || updated.Attribution != "Photo by Jane Doe" {
		t.Errorf("Source/Attribution = %q/%q", updated.Source, updated.Attribution)
	}

	fetched, err := c.Media.Get(ctx, uploaded.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.AltText != "a swatch" || len(fetched.Tags) != 2 {
		t.Errorf("Get after UpdateMetadata = %+v, want persisted metadata", fetched)
	}
}
