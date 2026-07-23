package main

import (
	"fmt"
	"io"

	"github.com/agentjoey/pactify/internal/registry"
)

// autoRegister idempotently registers dir so serve/dashboard see the project
// without a manual `pactify register` (spec: agent-started projects — an agent
// that inits or orchestrates should appear on the dashboard by default). It is
// best-effort: a name conflict or write error is WARNED, never fatal, because
// registration is a convenience and must not block init/orchestrate. noRegister
// opts out entirely.
func autoRegister(w io.Writer, dir string, noRegister bool) {
	if noRegister {
		return
	}
	name, added, err := registry.EnsureRegistered(dir)
	switch {
	case err != nil:
		fmt.Fprintf(w, "note: auto-register skipped (%v) — run `pactify register %s` to see it on the dashboard\n", err, dir)
	case added:
		fmt.Fprintf(w, "registered %q for the dashboard (pactify serve); --no-register to opt out\n", name)
	}
	// added=false (already registered): silent no-op.
}
