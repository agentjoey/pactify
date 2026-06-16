package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// hookCommand is the command a client invokes per tool call. Stable across
// rebuilds: it calls the `pactify` on PATH (install assumes pactify is installed).
func hookCommand(kind string) string { return "pactify audit hook --kind " + kind }

// Install registers the project-scoped PreToolUse audit hook for kind at
// repoDir/.claude/settings.json (claude-code; opencode shares the shape until
// verified otherwise — see install_opencode notes). Idempotent: a prior pact
// audit entry is removed before inserting.
func Install(kind, repoDir string) error {
	switch kind {
	case "claude-code", "opencode":
		return installClaudeStyle(kind, filepath.Join(repoDir, ".claude", "settings.json"))
	default:
		return fmt.Errorf("audit install: unsupported kind %q", kind)
	}
}

// Uninstall removes the audit hook entry for kind, leaving other hooks intact.
func Uninstall(kind, repoDir string) error {
	return uninstallClaudeStyle(filepath.Join(repoDir, ".claude", "settings.json"))
}

// Status reports a kind's audit-hook install state for a repo.
type Status struct {
	Kind      string
	Installed bool
}

// Detect reports, per supported kind, whether the project-scoped audit hook is
// installed in repoDir.
func Detect(repoDir string) []Status {
	out := []Status{}
	for _, kind := range []string{"claude-code", "opencode"} {
		s := readSettings(filepath.Join(repoDir, ".claude", "settings.json"))
		installed := false
		for _, e := range sliceOf(mapOf(s, "hooks"), "PreToolUse") {
			if isAuditEntry(e) {
				installed = true
			}
		}
		out = append(out, Status{Kind: kind, Installed: installed})
	}
	return out
}

func installClaudeStyle(kind, settingsPath string) error {
	s := readSettings(settingsPath)
	hooks := mapOf(s, "hooks")
	pre := dropAuditEntries(sliceOf(hooks, "PreToolUse"))
	pre = append(pre, map[string]any{
		"matcher": "*",
		"hooks":   []any{map[string]any{"type": "command", "command": hookCommand(kind)}},
	})
	hooks["PreToolUse"] = pre
	s["hooks"] = hooks
	return writeSettings(settingsPath, s)
}

func uninstallClaudeStyle(settingsPath string) error {
	if _, err := os.Stat(settingsPath); os.IsNotExist(err) {
		return nil
	}
	s := readSettings(settingsPath)
	hooks := mapOf(s, "hooks")
	hooks["PreToolUse"] = dropAuditEntries(sliceOf(hooks, "PreToolUse"))
	s["hooks"] = hooks
	return writeSettings(settingsPath, s)
}

// dropAuditEntries removes any PreToolUse entry whose command contains "audit hook".
func dropAuditEntries(entries []any) []any {
	out := []any{}
	for _, e := range entries {
		if isAuditEntry(e) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func isAuditEntry(e any) bool {
	m, ok := e.(map[string]any)
	if !ok {
		return false
	}
	hh, ok := m["hooks"].([]any)
	if !ok {
		return false
	}
	for _, h := range hh {
		if cmd, _ := mapAny(h)["command"].(string); strings.Contains(cmd, "audit hook") {
			return true
		}
	}
	return false
}

// --- tiny JSON-as-map helpers (settings.json is user-owned; preserve unknown keys) ---

func readSettings(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return map[string]any{}
	}
	var s map[string]any
	if json.Unmarshal(b, &s) != nil || s == nil {
		return map[string]any{}
	}
	return s
}

func writeSettings(path string, s map[string]any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func mapOf(s map[string]any, k string) map[string]any {
	if v, ok := s[k].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}
func sliceOf(s map[string]any, k string) []any {
	if v, ok := s[k].([]any); ok {
		return v
	}
	return []any{}
}
func mapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}
