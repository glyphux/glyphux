// Package instance implements the CLI's `glyphux instance open` surface
// (Ticket T10b): open the daemon's setup wizard in the operator's browser.
// The opener is injected so tests use a recorder instead of spawning a
// browser. RED checkpoint: the contract exists, the behavior is stubbed.
package instance

import (
	"context"
	"errors"
)

// Opener opens a URL in the operator's browser.
type Opener interface {
	Open(url string) error
}

// ErrNotImplemented is the RED checkpoint stub error.
var ErrNotImplemented = errors.New("instance: not yet implemented (T10b)")

// Open opens the browser at the daemon's setup URL derived from the config
// listen address (":8080" → http://localhost:8080/setup).
func Open(ctx context.Context, addr string, opener Opener) error {
	return ErrNotImplemented
}
