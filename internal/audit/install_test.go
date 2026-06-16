package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallClaudeCodeIdempotent(t *testing.T) {
	repo := t.TempDir()
	if err := Install("claude-code", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := Install("claude-code", repo); err != nil {
		t.Fatalf("install (2nd): %v", err)
	}
	b, err := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks := s["hooks"].(map[string]any)["PreToolUse"].([]any)
	n := 0
	for _, h := range hooks {
		entry := h.(map[string]any)
		for _, hh := range entry["hooks"].([]any) {
			if cmd, _ := hh.(map[string]any)["command"].(string); strings.Contains(cmd, "audit hook") {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 audit hook entry after 2 installs, got %d", n)
	}
}

func TestInstallPreservesExistingHooks(t *testing.T) {
	repo := t.TempDir()
	// Pre-seed a foreign PreToolUse hook (e.g. another tool); install must keep it.
	path := filepath.Join(repo, ".claude", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"hooks":{"PreToolUse":[{"matcher":"*","hooks":[{"type":"command","command":"other-tool x"}]}]},"model":"opus"}`), 0o644)
	if err := Install("claude-code", repo); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "other-tool x") {
		t.Fatalf("foreign hook lost: %s", b)
	}
	if !strings.Contains(string(b), `"model": "opus"`) {
		t.Fatalf("unrelated key lost: %s", b)
	}
	if !strings.Contains(string(b), "audit hook") {
		t.Fatalf("audit hook not added: %s", b)
	}
}

func TestUninstallRemovesEntry(t *testing.T) {
	repo := t.TempDir()
	_ = Install("claude-code", repo)
	if err := Uninstall("claude-code", repo); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(repo, ".claude", "settings.json"))
	if strings.Contains(string(b), "audit hook") {
		t.Fatalf("audit hook still present after uninstall: %s", b)
	}
}

func TestDetect(t *testing.T) {
	repo := t.TempDir()
	for _, s := range Detect(repo) {
		if s.Installed {
			t.Fatalf("%s should not be installed initially", s.Kind)
		}
	}
	_ = Install("claude-code", repo)
	got := Detect(repo)
	found := false
	for _, s := range got {
		if s.Kind == "claude-code" && s.Installed {
			found = true
		}
	}
	if !found {
		t.Fatalf("claude-code should be detected as installed: %+v", got)
	}
}

func TestInstallOpencodeWritesPlugin(t *testing.T) {
	repo := t.TempDir()
	if err := Install("opencode", repo); err != nil {
		t.Fatalf("opencode install: %v", err)
	}
	path := filepath.Join(repo, ".opencode", "plugin", "pact-audit.ts")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("plugin not written: %v", err)
	}
	if !strings.Contains(string(b), "tool.execute.before") || !strings.Contains(string(b), "audit hook --kind opencode") {
		t.Fatalf("plugin content wrong:\n%s", b)
	}
	// Detect reports it installed; Uninstall removes it.
	found := false
	for _, s := range Detect(repo) {
		if s.Kind == "opencode" && s.Installed {
			found = true
		}
	}
	if !found {
		t.Fatal("opencode should be detected as installed after Install")
	}
	if err := Uninstall("opencode", repo); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("plugin should be gone after Uninstall")
	}
}
