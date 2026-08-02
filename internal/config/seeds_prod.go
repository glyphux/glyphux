//go:build !dev

// Ticket T10a: the era/prod-root split. This file builds ONLY in normal
// (non-`-tags dev`) builds and selects the PROD-era root as Default()'s
// embedded seed — a production daemon does not trust dev-signed packages
// unless an operator explicitly re-enables the dev key via trusted_keys[]
// (additive) or opts into trust_mode=custom-only. See seeds_dev.go for the
// `-tags dev` counterpart.
package config

// embeddedDefaultRoot is the Default() seed for this build mode: the prod
// root in normal builds.
func embeddedDefaultRoot() TrustedKey { return embeddedProdRoot() }
