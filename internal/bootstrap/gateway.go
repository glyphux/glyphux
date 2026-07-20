package bootstrap

import (
	"net/http"
	"sync/atomic"
)

// Gateway is an http.Handler whose target can be swapped at runtime. It is
// how the daemon moves from serving the wizard-only phase to serving the
// full application in the same process, on the same listener, with no
// restart (§6.2) — completing setup calls Switch once, and every request
// after that is routed to the new handler.
type Gateway struct {
	current atomic.Pointer[http.Handler]
}

// NewGateway builds a Gateway that serves initial until Switch is called.
func NewGateway(initial http.Handler) *Gateway {
	g := &Gateway{}
	g.current.Store(&initial)
	return g
}

// Switch changes which handler subsequent requests are routed to. Safe to
// call concurrently with ServeHTTP.
func (g *Gateway) Switch(h http.Handler) {
	g.current.Store(&h)
}

func (g *Gateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*g.current.Load()).ServeHTTP(w, r)
}
