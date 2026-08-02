// Command glyphux-release builds the release artifact contract (Ticket
// T10c): deterministic per-platform archives (binaries + VERSION) plus a
// SHA256SUMS listing, driven by .github/workflows/release.yml on version
// tag push. RED checkpoint: the command exists; the behavior lands with
// the green implementation.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "glyphux-release: not yet implemented (T10c RED)")
	os.Exit(1)
}
