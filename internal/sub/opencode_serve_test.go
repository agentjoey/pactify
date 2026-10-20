package sub

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeServePeer(t *testing.T) { testenv.OpencodeServePeer(t) }
func TestOpencodeServeWorkerV2(t *testing.T) {
	for _, scenario := range []string{"permission", "readonly", "violation", "transient", "quota", "unknown"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", scenario)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			access := "write"
			if scenario == "readonly" || scenario == "violation" {
				access = "read-only"
			}
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			if _, err := store.Create("job", Request{Harness: "opencode", Model: "opencode/test-free", Access: access, Cwd: repo, Brief: "test"}); err != nil {
				t.Fatal(err)
			}
			st, _ := store.Load("job")
			st.ConfigMode = "minimal"
			store.Save(st)
			h, _ := Lookup("opencode")
			h.Version = nil
			result, err := RunJob(ctx, store, "job", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return time.Millisecond }})
			if err != nil {
				t.Fatal(err)
			}
			wantStatus := StatusSucceeded
			wantClass := ""
			switch scenario {
			case "violation":
				wantStatus = StatusFailed
				wantClass = "readonly_violation"
			case "quota":
				wantStatus = StatusFailed
				wantClass = "quota"
			case "unknown":
				wantStatus = StatusFailed
				wantClass = "other"
			}
			if result.Status != wantStatus || result.FailureClass != wantClass {
				t.Errorf("result=%+v", result)
			}
			if scenario == "transient" && len(result.Retries) != 1 {
				t.Errorf("retry history=%+v", result.Retries)
			}
			if scenario == "readonly" || scenario == "permission" || scenario == "transient" {
				multiplier := 1
				if scenario == "transient" {
					multiplier = 2
				}
				if result.InputTokens != 122*multiplier || result.OutputTokens != 10*multiplier || result.CostUSD != .25*float64(multiplier) || !result.ModelVerified {
					t.Errorf("accounting/model=%+v", result)
				}
			}
			log, _ := os.ReadFile(store.LogPath("job"))
			if (scenario == "readonly" || scenario == "permission") && !strings.Contains(string(log), "permission.asked") {
				t.Error("permission request not logged")
			}
		})
	}
}
func TestSandboxedContractV2(t *testing.T) {
	for _, name := range Names() {
		t.Run(name, func(t *testing.T) {
			h, _ := Lookup(name)
			want := name == "codex"
			store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			store.Create("job", Request{Harness: name})
			r := Result{Status: StatusSucceeded}
			if err := store.Finish("job", &r, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(store.ResultPath("job"))
			var fields map[string]any
			json.Unmarshal(b, &fields)
			if fields["sandboxed"] != want {
				t.Errorf("result.json sandboxed=%v want=%v", fields["sandboxed"], want)
			}
			c := newHarnessCatalog()
			c.lookupPath = func(string) (string, error) { return "", os.ErrNotExist }
			row := c.harness(context.Background(), store, h, HarnessDefault{}, false)
			b, _ = json.Marshal(row)
			json.Unmarshal(b, &fields)
			if fields["sandboxed"] != want {
				t.Errorf("catalog sandboxed=%v want=%v", fields["sandboxed"], want)
			}
			if name == "opencode" && !row.ReadOnly {
				t.Error("opencode read-only unavailable")
			}
		})
	}
}
func TestOpencodeReadonlyFinishViolationV2(t *testing.T) {
	store, _, _ := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	store.Save(st)
	r := Result{Status: StatusSucceeded, Diff: Diff{Files: []FileDelta{{Path: "unexpected.txt"}}}}
	if err := store.Finish("j1", &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusFailed || r.FailureClass != "readonly_violation" {
		t.Fatalf("result=%+v", r)
	}
}

func TestOpencodeDoctorNetworkV2(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("opencode")
	h.Home = nil
	h.Models = nil
	h.Version = func(context.Context) (string, error) { return "test", nil }
	h.HidesItself = true
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }, run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
		st, _ := store.Load(id)
		if id == "write" {
			if !strings.Contains(st.Request.Brief, "python3 doctor-network.py") {
				t.Error("network evidence must run the supplied script")
			}
			os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
			os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
		}
		return Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: "native/default", ModelVerified: true, InputTokens: 1, ToolCalls: 1, FinalText: "done"}, nil
	}, writeCheck: func(context.Context, Store, string, Result) error { return nil }, readonlyCheck: func(context.Context, Store, string, Result) error { return nil }})
	if err != nil {
		t.Fatal(err)
	}
	for _, check := range reports[0].Checks {
		want := "PASS"
		// Sandbox and non-applicable rows remain visible. This fixture deliberately
		// removes h.Home above; its original live-probe assertions stay PASS.
		switch check.Name {
		case "Sandbox":
			want = "WARN"
		case "Read-only hook", "Managed home":
			want = "SKIP"
		}
		if check.Status != want {
			t.Errorf("check=%+v", check)
		}
	}
}

func TestOpencodeReadonlyWorktreeViolationV2(t *testing.T) {
	store, st := worktreeFixture(t)
	st.Request.Harness = "opencode"
	st.Request.Access = "read-only"
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(st.Worktree.Cwd, "unexpected.txt"), []byte("oops"), 0600); err != nil {
		t.Fatal(err)
	}
	r := Result{Status: StatusSucceeded}
	if err := store.Finish(st.ID, &r, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusFailed || r.FailureClass != "readonly_violation" || len(r.Diff.Files) != 1 {
		t.Fatalf("result=%+v", r)
	}
}

func TestOpencodeAbsentWorkerDeltaV2(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	store.Save(st)
	if _, err := store.claim(context.Background(), st.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	log := `{"kind":"usage","usage":{"inputTokens":122,"outputTokens":10,"costUSD":0.25},"raw":{"accounting":"job_delta"}}` + "\n"
	if err := os.WriteFile(store.LogPath(st.ID), []byte(log), 0600); err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *State) error { return store.fallbackLocked(current, StatusFailed, "worker absent") }); err != nil {
		t.Fatal(err)
	}
	var result Result
	data, _ := os.ReadFile(store.ResultPath(st.ID))
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.InputTokens != 122 || result.OutputTokens != 10 || result.CostUSD != .25 || !result.UsagePartial {
		t.Fatalf("result=%+v", result)
	}
}
func TestCollectorACPFinalV2(t *testing.T) {
	var c Collector
	for i := 0; i < 10000; i++ {
		c.Add(cockpit.Event{Kind: cockpit.EventMessage, Text: "字a"})
		if i%200 == 0 {
			c.Add(cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start"}})
		}
	}
	c.Add(cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted, Final: true, Text: strings.Repeat("字a", 199)})
	if c.FinalText() != strings.Repeat("字a", 199) || c.ToolCalls != 50 {
		t.Fatalf("bytes=%d ToolCalls=%d", len(c.FinalText()), c.ToolCalls)
	}
}
