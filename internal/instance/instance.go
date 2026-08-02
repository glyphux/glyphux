// Package instance implements the CLI's `glyphux instance open` surface
// (Ticket T10b): open the daemon's setup wizard in the operator's browser.
// The opener is injected so tests use a recorder instead of spawning a
// browser.
package instance

import (
	"context"
	"strings"
)

// Opener opens a URL in the operator's browser.
type Opener interface {
	Open(url string) error
}

// Open opens the browser at the daemon's setup URL derived from the config
// listen address. The daemon's default Addr is ":8080" — a bare listen
// port, so the browser URL needs a host; any other address is used as-is.
// The opener is invoked exactly once and its error propagates.
func Open(ctx context.Context, addr string, opener Opener) error {
	if addr == "" {
		addr = ":8080"
	}
	host := addr
	if strings.HasPrefix(addr, ":") {
		host = "localhost" + addr
	}
	return opener.Open("http://" + host + "/setup")
}
