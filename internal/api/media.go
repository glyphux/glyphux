package api

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/internal/permission"
)

// mediaMaxUploadBytes bounds a single media upload — larger than the generic
// request cap since it carries real file bytes (slice 1.6). The daemon
// exempts /api/v0/media from the global body limiter so this cap, not the
// smaller default, governs uploads.
const mediaMaxUploadBytes = 10 << 20 // 10 MiB

// mediaMetadataMaxBodyBytes bounds the JSON body of PATCH /api/v0/media/{id}.
// The route falls under the /api/v0/media prefix the server-wide body
// limiter exempts (for the upload route's own larger cap above), so this
// JSON-only route must apply its own limit rather than accept an unbounded
// body.
const mediaMetadataMaxBodyBytes = 1 << 20 // 1 MiB, matching the server-wide default

func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, mediaMaxUploadBytes)
	if err := r.ParseMultipartForm(mediaMaxUploadBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, http.StatusRequestEntityTooLarge, "upload exceeds the maximum size")
			return
		}
		s.writeError(w, http.StatusBadRequest, "malformed upload; expected multipart/form-data with a \"file\" field")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "missing \"file\" field")
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		s.log.Error("read media upload", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	// multipart.Writer.CreateFormFile always sets "application/octet-stream"
	// on the part, so sniff the real type from content rather than trust it.
	mimeType := http.DetectContentType(data)

	item, err := s.media.Upload(r.Context(), s.principal(r), header.Filename, mimeType, data)
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleMediaGet(w http.ResponseWriter, r *http.Request) {
	item, err := s.media.Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request) {
	items, err := s.media.List(r.Context())
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

// transformQueryParams is every query param that requests a transform on
// the file-serving route; their presence (not just w/h) switches the
// handler from raw passthrough to the Transform pipeline.
var transformQueryParams = []string{"w", "h", "crop_x", "crop_y", "crop_w", "crop_h", "rotate", "format"}

func hasTransformParams(q url.Values) bool {
	for _, key := range transformQueryParams {
		if q.Has(key) {
			return true
		}
	}
	return false
}

func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	if hasTransformParams(q) {
		opts := media.TransformOptions{Format: q.Get("format")}
		opts.MaxW, _ = strconv.Atoi(q.Get("w"))
		opts.MaxH, _ = strconv.Atoi(q.Get("h"))
		opts.CropX, _ = strconv.Atoi(q.Get("crop_x"))
		opts.CropY, _ = strconv.Atoi(q.Get("crop_y"))
		opts.CropW, _ = strconv.Atoi(q.Get("crop_w"))
		opts.CropH, _ = strconv.Atoi(q.Get("crop_h"))
		opts.Rotate, _ = strconv.Atoi(q.Get("rotate"))
		data, contentType, err := s.media.Transform(r.Context(), id, opts)
		if err != nil {
			s.writeMediaError(w, err)
			return
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}

	rc, item, err := s.media.Open(r.Context(), id)
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("Content-Type", item.MimeType)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, rc)
}

func (s *Server) handleMediaDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.media.Delete(r.Context(), s.principal(r), r.PathValue("id")); err != nil {
		s.writeMediaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// mediaMetadataUpdateRequest is the body of PATCH /api/v0/media/{id} — the
// editable fields (alt text, tags, source/attribution), sent as a full
// replace matching internal/media.MetadataUpdate.
type mediaMetadataUpdateRequest struct {
	AltText     string   `json:"alt_text"`
	Tags        []string `json:"tags"`
	Source      string   `json:"source"`
	Attribution string   `json:"attribution"`
}

func (s *Server) handleMediaUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, mediaMetadataMaxBodyBytes)
	var body mediaMetadataUpdateRequest
	if !s.decodeJSON(w, r, &body) {
		return
	}
	item, err := s.media.UpdateMetadata(r.Context(), s.principal(r), r.PathValue("id"), media.MetadataUpdate{
		AltText:     body.AltText,
		Tags:        body.Tags,
		Source:      body.Source,
		Attribution: body.Attribution,
	})
	if err != nil {
		s.writeMediaError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, item)
}

func (s *Server) writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, permission.ErrDenied):
		// Reachable only if the domain API's own capability check fails
		// despite this package's requireCapability fast-fail already having
		// passed (defense-in-depth, PRD §10.5) — 403 either way.
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
	case errors.Is(err, media.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, media.ErrUnsupportedType):
		s.writeError(w, http.StatusUnsupportedMediaType, err.Error())
	case errors.Is(err, media.ErrInvalidTransform):
		s.writeError(w, http.StatusBadRequest, err.Error())
	default:
		s.log.Error("media request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}
