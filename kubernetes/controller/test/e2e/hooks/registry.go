// Package hooks holds named imperative escape hatches that scenarios can
// reference from their e2e.yaml. The runner resolves names against Registry
// at suite start; an unknown name is a load-time error.
//
// Stage 1 lands the registry empty. Hooks are added in later stages as the
// applyset and credentials scenarios migrate to the declarative runner.
package hooks

import "context"

// HookFunc is the signature of a scenario hook. The argument shape is
// intentionally minimal at Stage 1 — Scenario will gain the fields hooks need
// (kubectl client, OCM helper, scenario directory, registry URL) as the
// runner is fleshed out in Stages 2–4.
type HookFunc func(ctx context.Context, scenario *Scenario) error

// Scenario is the per-scenario context handed to each hook. It is defined in
// the hooks package so hooks can take a stable dependency on it without
// importing the e2e package (which would create a cycle).
//
// Fields are added incrementally as hooks are written. Stage 1 ships the
// empty struct.
type Scenario struct {
	// Folder is the slash-separated scenario name relative to its root, e.g.
	// "helm/simple" or "credentials/basic-auth".
	Folder string
	// SimpleName is Folder with "/" replaced by "-", safe for embedding in
	// Kubernetes resource names.
	SimpleName string
	// Dir is the absolute path to the scenario folder.
	Dir string
}

// Registry is the single source of truth for hook names. Adding a new hook
// is two lines: define the function in this package, register it here.
var Registry = map[string]HookFunc{}

// Resolve looks up a hook by name. Missing names are surfaced to the runner
// so it can report which scenario referenced an unknown hook.
func Resolve(name string) (HookFunc, bool) {
	fn, ok := Registry[name]
	return fn, ok
}
