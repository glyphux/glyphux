package graphql

// Principal resolution and capability enforcement for the GraphQL
// transport. There is no separate GraphQL login mutation (see the tracking
// doc) — callers authenticate via the existing REST /api/v0/auth/login and
// send the resulting token as `Authorization: Bearer <token>` on GraphQL
// requests too. This file resolves that header to a principal once per
// request (in HTTP middleware, before the GraphQL executor runs) and
// stashes it in context, mirroring currentUser/requireCapability in
// internal/api/auth.go — those are unexported to package api, so this is
// an independent implementation of the same logic, not a shared one.

import (
	"context"
	"net/http"
	"strings"

	"github.com/glyphux/glyphux/internal/identity"
	"github.com/glyphux/glyphux/internal/permission"
)

type ctxKey int

const principalKey ctxKey = iota

// withPrincipal resolves the bearer token on r, if any, to a principal and
// stashes it in the request context for resolvers to read via principalFrom.
// An absent, unknown, or expired token simply leaves the context
// unauthenticated — GraphQL has no per-transport 401 for a whole request,
// since a single request can mix public and privileged fields; enforcement
// happens per-field in the resolvers via requireCapability.
func (r *Resolver) withPrincipal(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if token, ok := bearerToken(req); ok {
			if u, err := r.sessions.Lookup(req.Context(), token); err == nil {
				req = req.WithContext(context.WithValue(req.Context(), principalKey, u))
			}
		}
		next.ServeHTTP(w, req)
	})
}

// bearerToken extracts the raw token from an `Authorization: Bearer <token>`
// header, matching bearerToken in internal/api/auth.go.
func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix) {
		return strings.TrimSpace(h[len(prefix):]), true
	}
	return "", false
}

// principalFrom returns the authenticated principal stashed by
// withPrincipal, if any.
func principalFrom(ctx context.Context) (*identity.User, bool) {
	u, ok := ctx.Value(principalKey).(*identity.User)
	return u, ok
}

// requireCapability resolves the current principal and checks it holds
// capability, returning a GraphQL error with the matching code if not:
// UNAUTHENTICATED for no principal (REST's 401), FORBIDDEN for an
// under-privileged one (REST's 403) — mirrors requireCapability in
// internal/api/auth.go.
func requireCapability(ctx context.Context, capability permission.Capability) (*identity.User, error) {
	user, ok := principalFrom(ctx)
	if !ok {
		return nil, gqlErr("UNAUTHENTICATED", "authentication required")
	}
	if !permission.Allows(user.Role, capability) {
		return nil, gqlErr("FORBIDDEN", "insufficient permissions")
	}
	return user, nil
}

// canReadDrafts reports whether the request's principal, if any, holds
// content:read_drafts — the gate between the admin view (every status) and
// the public view (published only) of content reads, matching canReadDrafts
// in internal/api/api.go.
func canReadDrafts(ctx context.Context) bool {
	user, ok := principalFrom(ctx)
	return ok && permission.Allows(user.Role, permission.ContentReadDrafts)
}
