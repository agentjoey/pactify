//go:build record

package cockpit

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit/golden"
)

func TestRecordOpencode2Golden(t *testing.T) {
	root := os.Getenv("PACTIFY_RECORD_HOME")
	if root == "" {
		t.Skip("host only: set isolated PACTIFY_RECORD_HOME and PACTIFY_HOME")
	}
	if !filepath.IsAbs(root) || root != os.Getenv("PACTIFY_HOME") {
		t.Fatal("record home must be absolute and equal PACTIFY_HOME")
	}
	for _, key := range []string{"HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		if err := golden.ManagedDirectory(root, os.Getenv(key)); err != nil {
			t.Fatalf("%s must be managed below record home", key)
		}
	}
	scenario := os.Getenv("PACTIFY_RECORD_SCENARIO")
	if !map[string]bool{"write": true, "readonly": true, "resume": true, "cancel": true, "failure": true}[scenario] {
		t.Fatal("invalid scenario")
	}
	repo := filepath.Join(root, "opencode2", "repo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	var diagnostics bytes.Buffer
	opts := StartOpts{RepoDir: repo, Access: AccessWrite, MinimalConfig: true, Stderr: &diagnostics}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	models, err := Opencode2Models(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	model := os.Getenv("PACTIFY_RECORD_MODEL")
	if model == "" {
		for _, m := range models {
			if strings.HasPrefix(m, "opencode/") && strings.HasSuffix(m, "-free") {
				model = m
				break
			}
		}
	}
	if !(strings.HasPrefix(model, "opencode/") && strings.HasSuffix(model, "-free") || strings.HasPrefix(model, "minimax-coding-plan/")) {
		t.Fatal("record model must be opencode/*-free or minimax-coding-plan/*")
	}
	if err := validateOpencodeModel(model, models); err != nil {
		t.Fatal(err)
	}
	versionCmd := exec.CommandContext(ctx, "opencode", "--version")
	versionOut, err := versionCmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	version := strings.Fields(string(versionOut))
	if len(version) == 0 || !strings.HasPrefix(version[len(version)-1], "v2.") {
		t.Fatal("recording requires opencode 2.x")
	}
	tr := golden.Transcript{Harness: "opencode2", Version: version[len(version)-1], Scenario: scenario, Model: model, Prompt: "Use your shell tool to write golden.txt containing alpha in the current directory. Then reply exactly GOLDEN_WRITE_OK."}
	switch scenario {
	case "readonly":
		opts.Access = AccessReadOnly
		tr.Prompt = "Read golden.txt with the read tool. Then try to write denied.txt only if a writing tool is available. Explain why shell and writes are unavailable in this session. Do not seek a workaround."
	case "resume":
		b, err := os.ReadFile(filepath.Join(root, "opencode2-session"))
		if err != nil {
			t.Fatal(err)
		}
		tr.Resume = string(b)
		tr.Prompt = "Read golden.txt using your tool, and reply exactly GOLDEN_RESUME_alpha."
	case "cancel":
		tr.Prompt = "Use your shell tool to run sleep 60 now. Wait for it to finish before replying."
	case "failure":
		tr.Prompt = "Reply exactly GOLDEN_FAILURE_PROBE."
	}
	r := &golden.Recorder{}
	opts.Transcript = r
	opts.Model = model
	result, err := goldenRun(ctx, tr, opts, "", nil)
	if err != nil {
		t.Fatalf("record: %v; diagnostics: %s", err, golden.SanitizeText(diagnostics.String()))
	}
	if scenario == "write" {
		b, err := os.ReadFile(filepath.Join(repo, "golden.txt"))
		if err != nil || strings.TrimSpace(string(b)) != "alpha" {
			t.Fatal("write evidence missing", err)
		}
		if err := os.WriteFile(filepath.Join(root, "opencode2-session"), []byte(result.SessionID), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if scenario == "readonly" {
		if _, err := os.Stat(filepath.Join(repo, "denied.txt")); !os.IsNotExist(err) {
			t.Fatal("read-only recording wrote denied.txt")
		}
		readSeen := false
		for _, f := range r.Snapshot() {
			if f.Path != "/api/event" {
				continue
			}
			for _, line := range strings.Split(f.Data, "\n") {
				line = strings.TrimPrefix(line, "data: ")
				var event struct {
					Type string `json:"type"`
					Data struct {
						Name string `json:"name"`
					} `json:"data"`
				}
				if json.Unmarshal([]byte(line), &event) != nil {
					continue
				}
				if event.Type == "session.tool.input.started" || event.Type == "session.tool.called" {
					if event.Data.Name == "shell" || event.Data.Name == "edit" {
						t.Fatal("readonly recording exposed a disabled tool")
					}
					if event.Data.Name == "read" {
						readSeen = true
					}
				}
			}
		}
		if !readSeen || result.Failure != "" {
			t.Fatal("readonly read evidence missing or job failed")
		}
	}
	if scenario == "failure" && result.Failure != "model" {
		t.Fatalf("failure=%s want model", result.Failure)
	}
	tr.Frames = r.Snapshot()
	tr.Expected, _ = json.Marshal(result)
	dir := filepath.Join("testdata", "golden", "opencode2")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, scenario+".json")
	if err := golden.Save(path, tr); err != nil {
		t.Fatal(err)
	}
	t.Log("Saved", path)
}
