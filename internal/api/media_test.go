package api_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func pngBytes(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func uploadRequest(t *testing.T, filename string, data []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", filename)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}

func TestMediaUploadGetListDeleteOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)
	data := pngBytes(t, 20, 10)
	body, contentType := uploadRequest(t, "cover.png", data)

	// Anonymous upload is rejected.
	anonBody, anonType := uploadRequest(t, "cover.png", data)
	anonReq := httptest.NewRequest(http.MethodPost, "/api/v0/media", anonBody)
	anonReq.Header.Set("Content-Type", anonType)
	anonRec := httptest.NewRecorder()
	h.ServeHTTP(anonRec, anonReq)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anon upload = %d, want 401", anonRec.Code)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v0/media", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, body %s", rec.Code, rec.Body.String())
	}
	created := decode(t, rec)
	id, _ := created["id"].(string)
	if id == "" {
		t.Fatal("upload returned empty id")
	}
	if created["width"].(float64) != 20 || created["height"].(float64) != 10 {
		t.Errorf("dimensions = %v x %v, want 20 x 10", created["width"], created["height"])
	}

	// Get metadata (public read).
	rec = do(t, h, http.MethodGet, "/api/v0/media/"+id, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get = %d", rec.Code)
	}

	// List.
	rec = do(t, h, http.MethodGet, "/api/v0/media", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list = %d", rec.Code)
	}
	var listBody struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listBody); err != nil {
		t.Fatal(err)
	}
	if len(listBody.Items) != 1 {
		t.Fatalf("list returned %d items, want 1", len(listBody.Items))
	}

	// Serve original bytes.
	rec = do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("file = %d", rec.Code)
	}
	if !bytes.Equal(rec.Body.Bytes(), data) {
		t.Error("served bytes differ from uploaded bytes")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}

	// Resize via query params.
	rec = do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?w=10", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("resize = %d", rec.Code)
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode resized: %v", err)
	}
	if cfg.Width != 10 || cfg.Height != 5 {
		t.Errorf("resized = %dx%d, want 10x5", cfg.Width, cfg.Height)
	}

	// Anonymous delete is rejected; authed delete succeeds.
	if rec := do(t, h, http.MethodDelete, "/api/v0/media/"+id, nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("anon delete = %d, want 401", rec.Code)
	}
	rec = doWithCookieBody(t, h, http.MethodDelete, "/api/v0/media/"+id, cookie, nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/api/v0/media/"+id, nil); rec.Code != http.StatusNotFound {
		t.Errorf("get after delete = %d, want 404", rec.Code)
	}
}

// quadPNG builds a 4x2 PNG, left half red / right half blue, so crop and
// rotate correctness can be verified against real output pixels over HTTP,
// not just dimensions or a 200 status.
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

func uploadQuad(t *testing.T, h http.Handler, cookie *http.Cookie) string {
	t.Helper()
	body, contentType := uploadRequest(t, "quad.png", quadPNG(t))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/media", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload = %d, body %s", rec.Code, rec.Body.String())
	}
	id, _ := decode(t, rec)["id"].(string)
	if id == "" {
		t.Fatal("upload returned empty id")
	}
	return id
}

func TestMediaFileCropOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	rec := do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?crop_x=2&crop_y=0&crop_w=2&crop_h=2", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("crop = %d, body %s", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 2 || b.Dy() != 2 {
		t.Fatalf("cropped dimensions = %dx%d, want 2x2", b.Dx(), b.Dy())
	}
	r, _, bl, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	if r != 0 || bl == 0 {
		t.Errorf("cropped pixel = r=%d b=%d, want blue (0, max)", r, bl)
	}
}

func TestMediaFileCropOutOfBoundsIs400(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	rec := do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?crop_x=0&crop_y=0&crop_w=99&crop_h=99", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("out-of-bounds crop = %d, want 400", rec.Code)
	}
}

func TestMediaFileRotateOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	rec := do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?rotate=90", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("rotate = %d, body %s", rec.Code, rec.Body.String())
	}
	img, err := png.Decode(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != 2 || b.Dy() != 4 {
		t.Fatalf("rotated dimensions = %dx%d, want 2x4", b.Dx(), b.Dy())
	}
	topR, _, topB, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	if topR == 0 || topB != 0 {
		t.Errorf("top pixel after rotate should be red; got r=%d b=%d", topR, topB)
	}
}

func TestMediaFileFormatOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	rec := do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?format=jpeg", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("format = %d, body %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	_, format, err := image.DecodeConfig(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("decoded format = %q, want jpeg", format)
	}
}

func TestMediaFileInvalidRotateIs400(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	rec := do(t, h, http.MethodGet, "/api/v0/media/"+id+"/file?rotate=45", nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("invalid rotate = %d, want 400", rec.Code)
	}
}

func TestMediaUpdateMetadataOverHTTP(t *testing.T) {
	h, cookie := authedServer(t)
	id := uploadQuad(t, h, cookie)

	patch := map[string]any{
		"alt_text":    "a quad",
		"tags":        []string{"stock", "hero"},
		"source":      "https://example.com/photo",
		"attribution": "Photo by Jane Doe",
	}
	patchBody, _ := json.Marshal(patch)

	// Anonymous update is rejected.
	anonReq := httptest.NewRequest(http.MethodPatch, "/api/v0/media/"+id, bytes.NewReader(patchBody))
	anonReq.Header.Set("Content-Type", "application/json")
	anonRec := httptest.NewRecorder()
	h.ServeHTTP(anonRec, anonReq)
	if anonRec.Code != http.StatusUnauthorized {
		t.Fatalf("anon patch = %d, want 401", anonRec.Code)
	}

	rec := doWithCookieBody(t, h, http.MethodPatch, "/api/v0/media/"+id, cookie, patch)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch = %d, body %s", rec.Code, rec.Body.String())
	}
	got := decode(t, rec)
	if got["alt_text"] != "a quad" {
		t.Errorf("alt_text = %v, want %q", got["alt_text"], "a quad")
	}
	tags, _ := got["tags"].([]any)
	if len(tags) != 2 || tags[0] != "stock" || tags[1] != "hero" {
		t.Errorf("tags = %v, want [stock hero]", got["tags"])
	}
	if got["source"] != "https://example.com/photo" {
		t.Errorf("source = %v", got["source"])
	}
	if got["attribution"] != "Photo by Jane Doe" {
		t.Errorf("attribution = %v", got["attribution"])
	}

	// Round-trips through a fresh GET too.
	rec = do(t, h, http.MethodGet, "/api/v0/media/"+id, nil)
	got = decode(t, rec)
	if got["alt_text"] != "a quad" {
		t.Errorf("GET after patch alt_text = %v, want %q", got["alt_text"], "a quad")
	}
}

func TestMediaUploadRejectsUnsupportedType(t *testing.T) {
	h, cookie := authedServer(t)
	body, contentType := uploadRequest(t, "notes.txt", []byte("hello world"))
	req := httptest.NewRequest(http.MethodPost, "/api/v0/media", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("upload .txt = %d, want 415", rec.Code)
	}
}
