//go:build dev

// Ticket T10a: the era/prod-root split. This file builds ONLY under
// `-tags dev` and selects the DEV root as Default()'s embedded seed — the
// dev workflow (signing fixtures with the dev key, testing installs) stays
// intact. Normal builds seed the prod root (see seeds_prod.go).
package config

// embeddedDefaultRoot is the Default() seed for this build mode: the dev
// root in `-tags dev` builds.
func embeddedDefaultRoot() TrustedKey { return embeddedDevRoot() }
