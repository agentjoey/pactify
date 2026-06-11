package main

import (
	"fmt"
	"os"

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
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
