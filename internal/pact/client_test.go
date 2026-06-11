package pact_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/pact"
)

// clientRepo bootstraps an init'd pact repo (foreign cwd) with one worker seat
// and returns the repo dir plus a handle acting as that worker.
func clientRepo(t *testing.T) (repo string, w *pact.Project) {
	t.Helper()
	repo = newGitRepo(t)
	other := t.TempDir()
	t.Chdir(other)
	t.Setenv("PACT_AGENT_ID", "orch")
	t.Setenv("PACT_DIR", "")
	orch := pact.At(repo).As("orch")
	seats := []string{
		"orch:orchestrator:CLAUDE.md",
		"w:worker:A.md",
	}
	if err := orch.Init("p", seats); err != nil {
		t.Fatal(err)
	}
	return repo, pact.At(repo).As("w")
}

// lastJoinPayload returns the payload of the last join event in the log.
func lastJoinPayload(t *testing.T, repo string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(repo, ".pact/log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			EventType string         `json:"event_type"`
			Payload   map[string]any `json:"payload"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("bad log line %q: %v", line, err)
		}
		if ev.EventType == "join" {
			payload = ev.Payload
		}
	}
	if payload == nil {
		t.Fatal("no join event in log")
	}
	return payload
}

// TestJoinWithClientWritesClientObject: JoinWithClient embeds client{name,version}.
func TestJoinWithClientWritesClientObject(t *testing.T) {
	repo, w := clientRepo(t)
	if err := w.JoinWithClient("w", "worker", "claude-code", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	p := lastJoinPayload(t, repo)
	cl, ok := p["client"].(map[string]any)
	if !ok {
		t.Fatalf("join payload missing client object: %v", p)
	}
	if cl["name"] != "claude-code" || cl["version"] != "1.2.3" {
		t.Fatalf("client object wrong: %v", cl)
	}
}

// TestJoinWrapsWithCLIIdentity: plain Join (CLI wrapper) fills pactify-cli +
// the injected ClientVersion package var.
func TestJoinWrapsWithCLIIdentity(t *testing.T) {
	repo, w := clientRepo(t)
	old := pact.ClientVersion
	pact.ClientVersion = "9.9.9"
	t.Cleanup(func() { pact.ClientVersion = old })
	if err := w.Join("w", "worker"); err != nil {
		t.Fatal(err)
	}
	p := lastJoinPayload(t, repo)
	cl, ok := p["client"].(map[string]any)
	if !ok {
		t.Fatalf("Join must fill the CLI client identity: %v", p)
	}
	if cl["name"] != "pactify-cli" || cl["version"] != "9.9.9" {
		t.Fatalf("CLI client identity wrong: %v", cl)
	}
}

// TestJoinWithoutClientOmitsField: an empty clientName emits NO client key, so
// client-free logs stay byte-identical to pre-feature logs.
func TestJoinWithoutClientOmitsField(t *testing.T) {
	repo, w := clientRepo(t)
	if err := w.JoinWithClient("w", "worker", "", ""); err != nil {
		t.Fatal(err)
	}
	p := lastJoinPayload(t, repo)
	if _, present := p["client"]; present {
		t.Fatalf("client key must be absent when name is empty: %v", p)
	}
	// roster payload (roles) is still intact.
	if _, ok := p["roles"]; !ok {
		t.Fatalf("roles must remain present: %v", p)
	}
}

// TestStateNeverContainsClient: provenance lives in the log only — STATE.yml is
// byte-clean of any client text, regardless of how the seat joined.
func TestStateNeverContainsClient(t *testing.T) {
	repo, w := clientRepo(t)
	if err := w.JoinWithClient("w", "worker", "claude-code", "1.2.3"); err != nil {
		t.Fatal(err)
	}
	st, err := os.ReadFile(filepath.Join(repo, ".pact/STATE.yml"))
	if err != nil {
		t.Fatal(err)
	}
	state := string(st)
	for _, needle := range []string{"client", "claude-code", "1.2.3", "pactify-cli"} {
		if strings.Contains(state, needle) {
			t.Fatalf("STATE.yml must never contain client text %q:\n%s", needle, state)
		}
	}
}
