package main

import "testing"

// glyphuxd's packaged release binary answers "-version"; glyphux must too,
// alongside its existing "version" subcommand, so the two binaries shipped
// in the same release don't ask a user to remember two different
// conventions for the same question.
func TestVersionFlagSpellingsAllPrintVersion(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"-version"}, {"--version"}} {
		if err := run(args); err != nil {
			t.Fatalf("run(%v) = %v, want nil", args, err)
		}
	}
}
