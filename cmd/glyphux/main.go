// Command glyphux is the optional developer CLI — a secondary surface for
// scripting, automation, and CI (§6.1). It is never the install path; the
// daemon's web wizard is. Phase-0 scope: validate composition documents.
package main

import (
	"fmt"
	"os"

	"github.com/glyphux/glyphux/pkg/contract"
)

const usage = `glyphux — Glyphux developer CLI (secondary surface; glyphuxd is the daemon)

Usage:
  glyphux composition validate <file>   Validate a composition document
  glyphux version                       Print the version
`

var version = "0.0.1-dev"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "glyphux:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "version":
		fmt.Println(version)
		return nil
	case "composition":
		if len(args) == 3 && args[1] == "validate" {
			return validateComposition(args[2])
		}
		return fmt.Errorf("usage: glyphux composition validate <file>")
	default:
		fmt.Print(usage)
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func validateComposition(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	comp, err := contract.Parse(raw)
	if err != nil {
		return err
	}
	fmt.Printf("valid: %s (site %q, %d content types)\n",
		comp.ContractVersion, comp.Site.Name, len(comp.ContentTypes))
	return nil
}
