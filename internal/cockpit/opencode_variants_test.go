package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeDiscoveryVariants(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	d, err := OpencodeModelDetails(context.Background(), StartOpts{RepoDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(d.Efforts, []string{"high", "low", "max"}) {
		t.Fatalf("efforts=%v", d.Efforts)
	}
	want := map[string][]string{
		"opencode/test-free":             {"high", "low"},
		"opencode/paid":                  {"high", "max"},
		"minimax-coding-plan/MiniMax-M3": {},
	}
	if !reflect.DeepEqual(d.ModelEfforts, want) {
		t.Fatalf("per-model efforts=%v", d.ModelEfforts)
	}
}

func TestOpencodeRejectsInvalidEffortBeforePrompt(t *testing.T) {
	for _, tc := range []struct{ model, effort, choices string }{
		{"opencode/test-free", "max", "high, low"},
		{"opencode/paid", "low", "high, max"},
		{"minimax-coding-plan/MiniMax-M3", "high", "none"},
		{"opencode/test-free", "HIGH", "high, low"},
	} {
		t.Run(tc.model+"/"+tc.effort, func(t *testing.T) {
			data := testenv.InstallOpencodeServe(t)
			s, err := NewOpencodeBackend().Start(context.Background(), StartOpts{RepoDir: t.TempDir(), Model: tc.model, Effort: tc.effort})
			if err == nil {
				s.Close()
			}
			if err == nil || !strings.Contains(err.Error(), tc.choices) || !strings.Contains(err.Error(), tc.model) || !strings.Contains(err.Error(), "--effort") || FailureFromError("opencode", err, time.Now()).Class != "model" {
				t.Fatalf("invalid effort accepted or unhelpful diagnostic: %v", err)
			}
			if got := ClassifyFailure("opencode", err.Error(), nil, time.Now()); got.Class != "model" {
				t.Fatalf("saved diagnostic lost model classification: %+v", got)
			}
			if _, err := os.Stat(filepath.Join(data, "prompt.json")); !os.IsNotExist(err) {
				t.Fatal("invalid effort submitted a prompt", err)
			}
		})
	}
}

func TestOpencodeResumeEffortUsesSessionModel(t *testing.T) {
	testenv.InstallOpencodeServe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free"}
	backend := NewOpencodeBackend()
	s, err := backend.Start(ctx, opts)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Prompt(ctx, UserMessage{Text: "first"}); err != nil {
		s.Close()
		t.Fatal(err)
	}
	id := s.ThreadID()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	// A changed global default must not select variants for the wrong model.
	t.Setenv("COCKPIT_OPENCODE_DEFAULT_MODEL", "opencode/paid")
	opts.Model, opts.Effort = "", "low"
	s, err = backend.(OptsResumer).ResumeWith(ctx, id, opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if got := s.(*opencodeSession).opts.Model; got != "opencode/test-free" {
		t.Fatalf("resumed model=%q", got)
	}
}

func TestOpencodeEffortUsesConfiguredModel(t *testing.T) {
	for _, configured := range []string{"opencode/test-free", "opencode/paid", ""} {
		t.Run(configured, func(t *testing.T) {
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OPENCODE_DEFAULT_MODEL", configured)
			s, err := NewOpencodeBackend().Start(context.Background(), StartOpts{RepoDir: t.TempDir(), Effort: "low"})
			if err == nil {
				defer s.Close()
			}
			if configured == "opencode/test-free" {
				if err != nil || s.(*opencodeSession).opts.Model != configured {
					t.Fatalf("configured model was not selected: session=%v err=%v", s, err)
				}
			} else if err == nil || FailureFromError("opencode", err, time.Now()).Class != "model" {
				t.Fatalf("unvalidated default effort: %v", err)
			}
		})
	}
}

func TestOpencodeEffortRequestIsNotActual(t *testing.T) {
	data := testenv.InstallOpencodeServe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s, err := NewOpencodeBackend().Start(ctx, StartOpts{RepoDir: t.TempDir(), Model: "opencode/test-free", Effort: "high"})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Prompt(ctx, UserMessage{Text: "test"}); err != nil {
		t.Fatal(err)
	}
	var prompt struct{ Variant string }
	b, err := os.ReadFile(filepath.Join(data, "prompt.json"))
	if err != nil || json.Unmarshal(b, &prompt) != nil || prompt.Variant != "high" {
		t.Fatalf("prompt=%s err=%v", b, err)
	}
	if info := s.(*opencodeSession).Info(); info.Effort != "" || info.Model != "opencode/test-free" {
		t.Fatalf("requested effort reported as actual without upstream evidence: %+v", info)
	}
}
