package cockpit

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func kimiReadonlyReviewWire(t *testing.T, home, wire string) string {
	t.Helper()
	dir := filepath.Join(home, "sessions", "session")
	if err := os.MkdirAll(filepath.Join(dir, "agents", "main"), 0700); err != nil {
		t.Fatal(err)
	}
	index, _ := json.Marshal(map[string]string{"sessionId": "session", "sessionDir": dir})
	if err := os.WriteFile(filepath.Join(home, "session_index.jsonl"), append(index, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents/main/wire.jsonl"), []byte(wire), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestKimiReadonlyReviewWireEvidence(t *testing.T) {
	safe := `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}` + "\n"
	for _, scenario := range []string{"long-line", "later-violation", "empty-agent", "called-agent-without-wire", "wire-without-snapshot", "incomplete-before-violation"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			wire := safe
			if scenario == "long-line" {
				wire = `{"type":"message","padding":"` + strings.Repeat("x", (16<<20)+10) + "\"}\n" + safe
			}
			if scenario == "later-violation" || scenario == "incomplete-before-violation" {
				wire += strings.ReplaceAll(safe, "Read", "Bash")
			}
			if scenario == "called-agent-without-wire" {
				wire += `{"type":"llm.request","agentId":"child"}` + "\n"
			}
			dir := kimiReadonlyReviewWire(t, home, wire)
			if scenario == "empty-agent" || scenario == "called-agent-without-wire" {
				os.MkdirAll(filepath.Join(dir, "agents/child"), 0700)
			}
			if scenario == "wire-without-snapshot" || scenario == "incomplete-before-violation" {
				os.MkdirAll(filepath.Join(dir, "agents/aaa"), 0700)
				os.WriteFile(filepath.Join(dir, "agents/aaa/wire.jsonl"), []byte(`{"type":"llm.request"}`+"\n"), 0600)
			}
			err := CheckKimiReadonly(home, "session")
			if scenario == "later-violation" || scenario == "incomplete-before-violation" {
				var violation *KimiReadOnlyViolation
				if !errors.As(err, &violation) {
					t.Fatalf("later violation not detected: %v", err)
				}
				return
			}
			if scenario == "called-agent-without-wire" || scenario == "wire-without-snapshot" {
				if err == nil {
					t.Fatal("missing called-agent evidence accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestKimiReadonlyReviewCanonicalSessionPaths(t *testing.T) {
	home := t.TempDir()
	kimiReadonlyReviewWire(t, home, `{"type":"llm.tools_snapshot","tools":[]}`+"\n")
	alias := filepath.Join(t.TempDir(), "home-alias")
	if err := os.Symlink(home, alias); err != nil {
		t.Fatal(err)
	}
	if err := CheckKimiReadonly(alias, "session"); err != nil {
		t.Errorf("canonical alias rejected: %v", err)
	}
	if runtime.GOOS == "darwin" {
		// Both spellings resolve into writable /private/tmp through /var's alias.
		fixture, err := os.MkdirTemp("/private/tmp", "kimi-path-")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(fixture)
		base := filepath.Base(fixture)
		kimiReadonlyReviewWire(t, "/private/var/../tmp/"+base, `{"type":"llm.tools_snapshot","tools":[]}`+"\n")
		if err := CheckKimiReadonly("/var/../tmp/"+base, "session"); err != nil {
			t.Errorf("/var spelling rejected: %v", err)
		}
	}
}
