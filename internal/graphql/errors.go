package graphql

// Error mapping from domain errors to GraphQL errors. GraphQL responses are
// always HTTP 200 (transport-level failures aside), so there is no status
// code to carry the REST transport's 404/422/409/401/403 distinctions —
// instead each error carries a machine-readable "code" extension mirroring
// those same distinctions, so callers can branch on it the way a REST
// client branches on status. See internal/api/api.go's writeContentError /
// writeMediaError / writeContentTypeError, which this mirrors.

import (
	"errors"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/media"
	"github.com/glyphux/glyphux/pkg/contract"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

// gqlErr builds a GraphQL error carrying a "code" extension.
func gqlErr(code, message string) *gqlerror.Error {
	return &gqlerror.Error{
		Message:    message,
		Extensions: map[string]any{"code": code},
	}
}

// mapCompositionError maps composition.Store errors, matching
// handleComposition/handleContentTypesList's status mapping in REST.
func (r *Resolver) mapCompositionError(err error) error {
	if errors.Is(err, composition.ErrNotFound) {
		return gqlErr("SETUP_REQUIRED", "setup not completed; visit /setup")
	}
	r.log.Error("composition request", "error", err)
	return gqlErr("INTERNAL", "internal error")
}

// mapContentError maps content.API errors, matching writeContentError in
// internal/api/api.go: unknown type / missing item -> NOT_FOUND, validation
// failures -> VALIDATION (with the field issues carried as an extension so
// they are not swallowed), composition not yet set up -> SETUP_REQUIRED.
func (r *Resolver) mapContentError(err error) error {
	switch {
	case errors.Is(err, content.ErrUnknownType), errors.Is(err, content.ErrNotFound):
		return gqlErr("NOT_FOUND", err.Error())
	case errors.Is(err, content.ErrValidation):
		var ve *content.ValidationError
		if errors.As(err, &ve) {
			gerr := gqlErr("VALIDATION", "validation failed")
			gerr.Extensions["issues"] = ve.Issues
			return gerr
		}
		return gqlErr("VALIDATION", err.Error())
	case errors.Is(err, composition.ErrNotFound):
		return gqlErr("SETUP_REQUIRED", "setup not completed; visit /setup")
	default:
		r.log.Error("content request", "error", err)
		return gqlErr("INTERNAL", "internal error")
	}
}

// mapMediaError maps media.API errors, matching writeMediaError.
func (r *Resolver) mapMediaError(err error) error {
	switch {
	case errors.Is(err, media.ErrNotFound):
		return gqlErr("NOT_FOUND", err.Error())
	case errors.Is(err, media.ErrUnsupportedType):
		return gqlErr("UNSUPPORTED_MEDIA_TYPE", err.Error())
	default:
		r.log.Error("media request", "error", err)
		return gqlErr("INTERNAL", "internal error")
	}
}

// mapContentTypeError maps composition content-type management errors,
// matching writeContentTypeError.
func (r *Resolver) mapContentTypeError(err error) error {
	switch {
	case errors.Is(err, composition.ErrContentTypeNotFound):
		return gqlErr("NOT_FOUND", err.Error())
	case errors.Is(err, composition.ErrNotFound):
		return gqlErr("SETUP_REQUIRED", "setup not completed; visit /setup")
	default:
		var verrs contract.ValidationErrors
		if errors.As(err, &verrs) {
			issues := make([]string, len(verrs))
			for i, v := range verrs {
				issues[i] = v.Error()
			}
			gerr := gqlErr("VALIDATION", "validation failed")
			gerr.Extensions["issues"] = issues
			return gerr
		}
		r.log.Error("content type request", "error", err)
		return gqlErr("INTERNAL", "internal error")
	}
}
