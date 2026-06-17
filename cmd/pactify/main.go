package main

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/agentmanifest"
	"github.com/agentjoey/pactify/internal/pact"
)

// Overridden at release time via -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Stamp the CLI's self-reported client version onto join provenance.
	pact.ClientVersion = version
	// Merge user-declared custom agents (~/.pactify/agents/*.toml) into the registry
	// before any command runs (covers the CLI and the serve subcommand alike).
	for _, w := range agentmanifest.LoadAndRegister() {
		fmt.Fprintln(os.Stderr, "pactify: custom agent manifest skipped — "+w)
	}
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
