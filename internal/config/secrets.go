package config

import "os"

// secret is the one seam this package reads secret values (DB DSNs with
// embedded passwords today; OAuth client secrets once 0009 lands, mailer API
// keys later) through — env vars, exactly as every other GLYPHUX_* setting.
//
// This is deliberately a single function, not a provider interface with
// multiple implementations: today there is exactly one real implementation
// (the environment) and no second one to abstract over yet (§16: "no new
// abstraction unless it retires real duplicated code or solves a real
// external use case"). What it buys now is a named, singular place that
// documents "this is where secrets come from" instead of a bare os.Getenv
// call indistinguishable from every other (non-secret) config value; if a
// real secrets manager is ever needed, only this function's body changes —
// not every call site.
func secret(key string) (string, bool) {
	return os.LookupEnv(key)
}
