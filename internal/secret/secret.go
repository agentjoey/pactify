// Package secret reads secrets from the macOS Keychain. Pactify never stores
// tokens in plaintext config — callers fetch them at use-time from the Keychain.
package secret

import (
	"fmt"
	"os/exec"
	"strings"
)

// Keychain returns a generic-password value from the macOS login Keychain by
// service + account. It returns an actionable error if the item is missing (or
// on a non-macOS host where `security` is absent), including the exact command
// to add it.
func Keychain(service, account string) (string, error) {
	out, err := exec.Command("security", "find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		return "", fmt.Errorf(
			"secret %s/%s not found in Keychain — add it with:\n  security add-generic-password -s %s -a %s -w '<value>'",
			service, account, service, account,
		)
	}
	return strings.TrimSpace(string(out)), nil
}

// GLMToken returns the Z.ai (智谱 GLM) auth token from the Keychain — service
// "pactify", account "glm". Used to point a claude-code seat at the GLM
// Anthropic-compatible endpoint.
func GLMToken() (string, error) {
	return Keychain("pactify", "glm")
}
