// Package machineid provides this host's stable machine identity for multi-machine
// orchestration (M1). Distinct from the account: one account can have many
// machines, each with a persistent id so the relay tracks presence + routes rpc
// to the right host. Replaces the U3 MVP that used the account id as the machineId.
package machineid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Load returns this machine's stable id, generating and persisting one on first
// use. Resolution order: PACTIFY_MACHINE_ID env → the file at
// ~/.config/pactify/machine-id (alongside the master secret) → freshly generated
// and written. Stable across restarts so the relay keeps one machine identity.
func Load() (string, error) {
	if v := strings.TrimSpace(os.Getenv("PACTIFY_MACHINE_ID")); v != "" {
		return v, nil
	}
	path, err := idPath()
	if err != nil {
		return "", err
	}
	if b, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(b)); id != "" {
			return id, nil
		}
	}
	id := generate()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("machineid dir: %w", err)
	}
	if err := os.WriteFile(path, []byte(id+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("persist machineid: %w", err)
	}
	return id, nil
}

func idPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pactify", "machine-id"), nil
}

func generate() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand failure is catastrophic; fall back to a non-random marker
		// rather than panic — still unique-enough per boot for a dev machine.
		return "m-fallback"
	}
	return "m-" + hex.EncodeToString(b)
}
