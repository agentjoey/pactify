package cockpit

import (
	"context"
	"encoding/json"
	"github.com/agentjoey/pactify/internal/cockpit/golden"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real v2 recordings are deliberately supplied by the host; ordinary tests
// never contact a model. Handwritten peers cover these flows before recording.
func TestOpencode2GoldenReplay(t *testing.T) {
	files, _ := filepath.Glob("testdata/golden/opencode2/*.json")
	if len(files) == 0 {
		t.Skip("host opencode2 recordings have not been captured")
	}
	if len(files) != 5 {
		t.Fatalf("need write, readonly, resume, cancel and failure recordings; got %d", len(files))
	}
	for _, path := range files {
		t.Run(filepath.Base(path), func(t *testing.T) {
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := golden.Scan(string(b)); err != nil {
				t.Fatal(err)
			}
			tr, err := readGolden(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := replayGolden(t, tr); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpencode2SyntheticGoldenReplay(t *testing.T) {
	for _, scenario := range []string{"write", "readonly", "resume", "cancel", "failure"} {
		t.Run(scenario, func(t *testing.T) {
			opts := oc2Opts(t)
			t.Setenv("COCKPIT_OC2_SCENARIO", scenario)
			tr := golden.Transcript{Harness: "opencode2", Scenario: scenario, Model: "opencode/test-free", Prompt: "synthetic replay"}
			if scenario == "resume" {
				tr.Resume = "ses_root"
			}
			if scenario == "readonly" {
				opts.Access = AccessReadOnly
			}
			recorder := &golden.Recorder{}
			opts.Transcript = recorder
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			result, err := goldenRun(ctx, tr, opts, "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "cancel" {
				found := false
				for _, e := range result.Events {
					if e == "tool:start" {
						found = true
					}
				}
				if !found {
					t.Fatal("cancel replay requires tool start")
				}
			}
			tr.Frames = recorder.Snapshot()
			tr.Expected, _ = json.Marshal(result)
			if err := replayGolden(t, tr); err != nil {
				t.Fatal(err)
			}
			if scenario == "write" {
				changed := false
				for i, f := range tr.Frames {
					if strings.Contains(f.Data, `"tokens"`) {
						tr.Frames[i].Data = strings.ReplaceAll(f.Data, `"tokens"`, `"tokenz"`)
						changed = true
					}
				}
				if !changed {
					t.Fatal("mutation did not touch accounting")
				}
				if err := replayGolden(t, tr); err == nil {
					t.Fatal("token mutation was not detected")
				}
			}
		})
	}
}
