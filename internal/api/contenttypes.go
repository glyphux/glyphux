package api

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/pkg/contract"
)

func (s *Server) handleContentTypesList(w http.ResponseWriter, r *http.Request) {
	comp, err := s.compositions.Load(r.Context())
	if errors.Is(err, composition.ErrNotFound) {
		s.writeError(w, http.StatusConflict, "setup not completed; visit /setup")
		return
	}
	if err != nil {
		s.log.Error("load composition", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"content_types": comp.ContentTypes})
}

func (s *Server) handleContentTypePut(w http.ResponseWriter, r *http.Request) {
	var ct contract.ContentType
	if !s.decodeJSON(w, r, &ct) {
		return
	}
	comp, err := s.compositions.DefineContentType(r.Context(), r.PathValue("name"), ct)
	if err != nil {
		s.writeContentTypeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, comp.ContentTypes[r.PathValue("name")])
}

func (s *Server) handleContentTypeDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	count, err := s.content.CountItems(r.Context(), name)
	if err != nil {
		s.log.Error("count content items", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if count > 0 {
		s.writeError(w, http.StatusConflict, "content type has existing items; delete or migrate them first")
		return
	}

	if _, err := s.compositions.RemoveContentType(r.Context(), name); err != nil {
		s.writeContentTypeError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// writeContentTypeError maps composition domain errors to HTTP status codes.
func (s *Server) writeContentTypeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, composition.ErrContentTypeNotFound):
		s.writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, composition.ErrNotFound):
		s.writeError(w, http.StatusConflict, "setup not completed; visit /setup")
	default:
		var verrs contract.ValidationErrors
		if errors.As(err, &verrs) {
			issues := make([]string, len(verrs))
			for i, v := range verrs {
				issues[i] = v.Error()
			}
			s.writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
				"error":  "validation failed",
				"issues": issues,
			})
			return
		}
		s.log.Error("content type request", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
	}
}
