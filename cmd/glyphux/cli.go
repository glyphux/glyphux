// Command glyphux is the optional developer CLI — a secondary surface for
// scripting, automation, and CI (§6.1). It is never the install path; the
// daemon's web wizard is. The T10b service/instance commands dispatch
// through runWith(deps) — the injectable seam tests drive with fakes.
package main

import (
	"fmt"

	"github.com/glyphux/glyphux/internal/instance"
	"github.com/glyphux/glyphux/internal/service"
)

// deps carries the injectable collaborators the service/instance commands
// dispatch to — a real service.Manager and browser opener in production,
// fakes in tests.
type deps struct {
	addr    string
	service *service.Manager
	opener  instance.Opener
}

// defaultDeps builds the production deps: the config default listen
// address and a manager over the host platform.
func defaultDeps() deps {
	return deps{addr: ":8080"}
}

// runWith dispatches args against d — the testable seam behind run().
func runWith(args []string, d deps) error {
	// T10b RED: service/instance dispatch is not wired yet; every command
	// falls through to the unknown-command error.
	return fmt.Errorf("unknown command %q", args[0])
}
