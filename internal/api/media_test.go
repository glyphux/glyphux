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
