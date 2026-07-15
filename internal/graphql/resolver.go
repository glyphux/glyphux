package graphql

// This file will not be regenerated automatically.
//
// It serves as dependency injection for your app, add any dependencies you
// require here.

import (
	"log/slog"

	"github.com/glyphux/glyphux/internal/composition"
	"github.com/glyphux/glyphux/internal/content"
	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/media"
)

// Resolver is the root GraphQL resolver. It is a client of the same domain
// APIs the REST transport (internal/api) serves — GraphQL is another
// transport over them, never a new privileged path (see
// docs/implementation/active/0004-slice-1-12-graphql-transport.md).
type Resolver struct {
	compositions *composition.Store
	content      *content.API
	media        *media.API
	identities   *identity.Service
	sessions     *identity.Sessions
	log          *slog.Logger
}

// NewResolver wires the GraphQL resolver to the domain APIs.
func NewResolver(comps *composition.Store, contentAPI *content.API, mediaAPI *media.API, identities *identity.Service, sessions *identity.Sessions, log *slog.Logger) *Resolver {
	return &Resolver{
		compositions: comps,
		content:      contentAPI,
		media:        mediaAPI,
		identities:   identities,
		sessions:     sessions,
		log:          log,
	}
}
