package main

// Proven at the flag-parsing boundary rather than by spawning the built
// binary as a subprocess: -version must print and return before run() ever
// calls config.Load/bootstrap.Boot, so a packaged release binary can report
// its version without needing a data dir, a writable config path, or a real
// database available.

import (
	"flag"
	"os"
	"testing"
)

func TestVersionFlagShortCircuitsBeforeBoot(t *testing.T) {
	origArgs := os.Args
	origCommandLine := flag.CommandLine
	defer func() {
		os.Args = origArgs
		flag.CommandLine = origCommandLine
	}()

	os.Args = []string{"glyphuxd", "-version"}
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ExitOnError)

	if err := run(); err != nil {
		t.Fatalf("run() with -version = %v, want nil", err)
	}
}
