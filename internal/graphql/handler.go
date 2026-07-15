package graphql

// NewHandler assembles the GraphQL HTTP transport: gqlgen's executor over
// the generated schema, wrapped with principal resolution so resolvers can
// enforce capabilities.

import (
	"net/http"

	"github.com/99designs/gqlgen/graphql/handler"
	"github.com/glyphux/glyphux/internal/graphql/generated"
)

// NewHandler builds the http.Handler to mount at POST /graphql.
func NewHandler(r *Resolver) http.Handler {
	srv := handler.NewDefaultServer(generated.NewExecutableSchema(generated.Config{Resolvers: r}))
	return r.withPrincipal(srv)
}
