package api

import (
	"errors"
	"net/http"

	"github.com/glyphux/glyphux/internal/permission"
	"github.com/glyphux/glyphux/pkg/contract"
)

// writeDomainError maps the tail every domain-error-to-HTTP translator in
// this package shares, once its own not-found-shaped sentinels (which vary
// per domain — composition.ErrContentTypeNotFound/ErrNotFound,
// layout.ErrNotFound, ...) have already been checked and didn't match:
// permission.ErrDenied → 403, a contract.ValidationErrors → 422 with an
// issue list, anything else → logged under logTag and 500. Callers
// (writeContentTypeError, writeLayoutError) check their own sentinels first
// in their own switch and fall through to this only for the remaining,
// identical cases every one of them used to duplicate in full.
func (s *Server) writeDomainError(w http.ResponseWriter, err error, logTag string) {
	if errors.Is(err, permission.ErrDenied) {
		s.writeError(w, http.StatusForbidden, "insufficient permissions")
		return
	}
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
	s.log.Error(logTag, "error", err)
	s.writeError(w, http.StatusInternalServerError, "internal error")
}
