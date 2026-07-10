package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/glyphux/glyphux/internal/media"
)

// mediaMaxUploadBytes bounds a single media upload — larger than the generic
// request cap since it carries real file bytes (slice 1.6). The daemon
// exempts /api/v0/media from the global body limiter so this cap, not the
// smaller default, governs uploads.
const mediaMaxUploadBytes = 10 << 20 // 10 MiB

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

	item, err := s.media.Upload(r.Context(), header.Filename, mimeType, data)
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

func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	q := r.URL.Query()
	if q.Has("w") || q.Has("h") {
		width, _ := strconv.Atoi(q.Get("w"))
		height, _ := strconv.Atoi(q.Get("h"))
		data, contentType, err := s.media.Resize(r.Context(), id, width, height)
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
	if err := s.media.Delete(r.Context(), r.PathValue("id")); err != nil {
		s.writeMediaError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) writeMediaError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, media.ErrNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, media.ErrUnsupportedType):
		s.writeError(w, http.StatusUnsupportedMediaType, err.Error())
	default:
		s.log.Error("media request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}
