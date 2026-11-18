package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const cursorInstallHint = "install Cursor CLI from https://cursor.com/cli (installs cursor-agent and agent)"

// cursorConfigDir mirrors Cursor's own lookup: CURSOR_CONFIG_DIR, then
// $XDG_CONFIG_HOME/cursor, then ~/.cursor.
func cursorConfigDir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv("CURSOR_CONFIG_DIR")); dir != "" {
		return filepath.Abs(dir)
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Abs(filepath.Join(xdg, "cursor"))
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cursor"), nil
}

func managedCursorHome() (string, error) { return managedHarnessHome("cursor") }

// cursorHome is the private CURSOR_CONFIG_DIR. Cursor keeps its login in the OS
// keychain or in an auth.json beside the real HOME, both outside this directory
// and both reached because HOME is left unchanged. Nothing is linked: the user's
// cli-config.json (allowlists, sandbox mode, team selection) must not leak into
// delegated jobs, and chats are written under this directory only.
func cursorHome(home, source string) *managedHome {
	return &managedHome{Name: "cursor", Home: home, Source: source, AllowMissingSource: true, Links: map[string]homeLink{}}
}

func doctorCursorHome(store Store, id string, r Result, succeeded bool) error {
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if !succeeded || st.EffectiveConfigMode() != "minimal" || st.HarnessHome == "" {
		return fmt.Errorf("cursor managed-home probe did not succeed")
	}
	source, err := cursorConfigDir()
	if err != nil {
		return err
	}
	return cursorHome(st.HarnessHome, source).Check()
}

func verifyCursor(_ context.Context, v VerificationInput) Verification {
	if v.Info.Model == "" {
		return Verification{UsagePartial: true, Warning: "Cursor did not report the session model; model could not be verified"}
	}
	return Verification{Model: v.Info.Model, Verified: true}
}

func concealCursor(ConcealContext) error {
	return fmt.Errorf("cursor --with-user-config keeps chats in the user's own chat history; use minimal config to hide delegated sessions")
}
