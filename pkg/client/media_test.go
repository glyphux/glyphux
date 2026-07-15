package client_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"
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
