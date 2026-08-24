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

func TestInstallGeminiIdempotentPreservesKeys(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".gemini", "settings.json")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"hooks":{"BeforeTool":[{"matcher":"*","hooks":[{"type":"command","command":"other-tool x"}]}]},"model":"flash"}`), 0o644)

	if err := Install("gemini", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := Install("gemini", repo); err != nil {
		t.Fatalf("install (2nd): %v", err)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "other-tool x") {
		t.Fatalf("foreign hook lost: %s", b)
	}
	if !strings.Contains(string(b), `"model": "flash"`) {
		t.Fatalf("unrelated key lost: %s", b)
	}
	if !strings.Contains(string(b), `"command": "pactify audit hook --kind gemini"`) {
		t.Fatalf("gemini audit hook missing: %s", b)
	}

	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings not valid JSON: %v", err)
	}
	hooks := s["hooks"].(map[string]any)["BeforeTool"].([]any)
	n := 0
	for _, h := range hooks {
		entry := h.(map[string]any)
		for _, hh := range entry["hooks"].([]any) {
			if cmd, _ := hh.(map[string]any)["command"].(string); strings.Contains(cmd, "audit hook --kind gemini") {
				n++
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 gemini audit hook entry after 2 installs, got %d", n)
	}

	if err := Uninstall("gemini", repo); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "audit hook") {
		t.Fatalf("audit hook still present after uninstall: %s", b)
	}
	if !strings.Contains(string(b), `"model": "flash"`) {
		t.Fatalf("unrelated key lost after uninstall: %s", b)
	}
}

func TestDetectGemini(t *testing.T) {
	repo := t.TempDir()
	_ = Install("gemini", repo)
	found := false
	for _, s := range Detect(repo) {
		if s.Kind == "gemini" && s.Installed {
			found = true
		}
	}
	if !found {
		t.Fatalf("gemini should be detected as installed: %+v", Detect(repo))
	}
}

// agy loads workspace-scoped hooks from `<workspace>/.agents/hooks.json`, whose
// top level is a map of hook NAME → event config (not claude's
// `{"hooks":{"<event>":[...]}}`). Verified against agy 1.1.19; see
// docs/backlog.md [AUDIT].
func TestInstallAntigravityWritesAgentsHooksJSON(t *testing.T) {
	repo := t.TempDir()
	path := filepath.Join(repo, ".agents", "hooks.json")
	// Pre-seed a foreign named hook; install must keep it.
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(`{"lint-checker":{"PostToolUse":[{"matcher":"run_command","hooks":[{"type":"command","command":"./scripts/lint.sh"}]}]}}`), 0o644)

	if err := Install("antigravity", repo); err != nil {
		t.Fatalf("install: %v", err)
	}
	if err := Install("antigravity", repo); err != nil {
		t.Fatalf("install (2nd): %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("hooks.json not written: %v", err)
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("hooks.json not valid JSON: %v", err)
	}
	if _, ok := s["lint-checker"]; !ok {
		t.Fatalf("foreign named hook lost: %s", b)
	}
	if !strings.Contains(string(b), `"command": "pactify audit hook --kind antigravity"`) {
		t.Fatalf("antigravity audit hook missing: %s", b)
	}

	// Exactly one audit handler after two installs, under PreToolUse with a
	// wildcard matcher (agy's grouped shape for tool-scoped events).
	n := 0
	for _, v := range s {
		groups, ok := v.(map[string]any)["PreToolUse"].([]any)
		if !ok {
			continue
		}
		for _, g := range groups {
			entry := g.(map[string]any)
			if entry["matcher"] != "*" {
				continue
			}
			for _, hh := range entry["hooks"].([]any) {
				if cmd, _ := hh.(map[string]any)["command"].(string); strings.Contains(cmd, "audit hook --kind antigravity") {
					n++
				}
			}
		}
	}
	if n != 1 {
		t.Fatalf("want exactly 1 antigravity audit handler after 2 installs, got %d\n%s", n, b)
	}

	found := false
	for _, st := range Detect(repo) {
		if st.Kind == "antigravity" && st.Installed {
			found = true
		}
	}
	if !found {
		t.Fatalf("antigravity should be detected as installed: %+v", Detect(repo))
	}

	if err := Uninstall("antigravity", repo); err != nil {
		t.Fatalf("uninstall: %v", err)
	}
	b, _ = os.ReadFile(path)
	if strings.Contains(string(b), "audit hook") {
		t.Fatalf("audit hook still present after uninstall: %s", b)
	}
	if !strings.Contains(string(b), "lint-checker") {
		t.Fatalf("foreign named hook lost on uninstall: %s", b)
	}
	for _, st := range Detect(repo) {
		if st.Kind == "antigravity" && st.Installed {
			t.Fatal("antigravity should not be detected after Uninstall")
		}
	}
}

// With no foreign hooks left, uninstall removes the file rather than leaving an
// empty `{}` behind in the user's repo. Uninstall on a repo that never had one
// is a no-op.
func TestUninstallAntigravityCleansUpEmptyFile(t *testing.T) {
	repo := t.TempDir()
	if err := Uninstall("antigravity", repo); err != nil {
		t.Fatalf("uninstall on clean repo should be a no-op: %v", err)
	}
	if err := Install("antigravity", repo); err != nil {
		t.Fatal(err)
	}
	if err := Uninstall("antigravity", repo); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".agents", "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("hooks.json should be removed once no hooks remain")
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
