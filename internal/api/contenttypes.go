package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/permission"
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
	types := comp.ContentTypes
	if types == nil {
		// A composition with no declared content types has a nil Go map;
		// encoding/json serializes that as JSON null, which non-Go clients
		// cannot safely treat as an iterable record. Normalize to {} at the
		// wire boundary so content_types is always an object.
		types = map[string]contract.ContentType{}
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"content_types": types})
}

func (s *Server) handleContentTypePut(w http.ResponseWriter, r *http.Request) {
	var ct contract.ContentType
	if !s.decodeJSON(w, r, &ct) {
		return
	}
	comp, err := s.compositions.DefineContentType(r.Context(), s.principal(r), r.PathValue("name"), ct)
	if err != nil {
		s.writeContentTypeError(w, err)
		return
	}
	s.writeJSON(w, http.StatusOK, comp.ContentTypes[r.PathValue("name")])
}

// errContentTypeHasItems signals the delete-guard rejected a removal because
// content items of that type still exist.
var errContentTypeHasItems = errors.New("content type has existing items; delete or migrate them first")

func (s *Server) handleContentTypeDelete(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")

	// A cheap up-front check for the common case (fast 409 without even
	// starting the CAS loop); the guard below re-checks immediately before
	// the actual write, closing the window where an item could be created
	// between this check and the delete committing.
	count, err := s.content.CountItems(r.Context(), name)
	if err != nil {
		s.log.Error("count content items", "error", err)
		s.writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if count > 0 {
		s.writeError(w, http.StatusConflict, errContentTypeHasItems.Error())
		return
	}

	guard := func(ctx context.Context) error {
		n, err := s.content.CountItems(ctx, name)
		if err != nil {
			return err
		}
		if n > 0 {
			return errContentTypeHasItems
		}
		return nil
	}
	if _, err := s.compositions.RemoveContentTypeGuarded(r.Context(), s.principal(r), name, guard); err != nil {
		if errors.Is(err, errContentTypeHasItems) {
			s.writeError(w, http.StatusConflict, err.Error())
			return
		}
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
	case errors.Is(err, permission.ErrDenied):
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
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
