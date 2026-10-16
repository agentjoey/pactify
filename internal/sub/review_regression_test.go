package sub

import (
	"context"
	"encoding/json"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewReadonlyFilesystem(t *testing.T) {
	for _, scenario := range []string{"ignored-delete", "ignored-modify", "untracked-delete"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			testenv.InstallOpencodeServe(t)
			t.Setenv("COCKPIT_OC_SCENARIO", scenario)
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(scenario, "ignored") {
				os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("victim\n"), 0600)
			}
			os.WriteFile(filepath.Join(repo, "victim"), []byte("original"), 0600)
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			st, err := store.Create("job", Request{Harness: "opencode", Model: "opencode/test-free", Access: "read-only", Cwd: repo, Brief: "test"})
			if err != nil {
				t.Fatal(err)
			}
			st.ConfigMode = "minimal"
			store.Save(st)
			h, _ := Lookup("opencode")
			h.Version = nil
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			r, err := RunJob(ctx, store, "job", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != StatusFailed || r.FailureClass != "readonly_violation" {
				t.Errorf("status=%s class=%s warnings=%v", r.Status, r.FailureClass, r.Warnings)
			}
		})
	}
}
func TestReviewReadonlyLostWorker(t *testing.T) {
	store, _, _ := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	store.Save(st)
	if _, err := store.claim(context.Background(), st.ID, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := store.WithState(context.Background(), st.ID, func(current *State) error { return store.fallbackLocked(current, StatusFailed, "worker absent") }); err != nil {
		t.Fatal(err)
	}
	var r Result
	b, _ := os.ReadFile(store.ResultPath(st.ID))
	json.Unmarshal(b, &r)
	if !strings.Contains(strings.Join(r.Warnings, " "), "Read-only verification unavailable") {
		t.Fatalf("warnings=%v", r.Warnings)
	}
}
func TestReviewDoctorRequiresDeniedWrite(t *testing.T) {
	denial := `{"kind":"tool","tool":{"phase":"end","name":"bash"},"raw":{"part":{"type":"tool","tool":"bash","state":{"status":"error","input":{"command":"echo x > probe.txt"},"error":"Permission denied"}}}}` + "\n"
	for _, tc := range []struct {
		name    string
		denyOn  map[string]bool // read-only probe ids whose log shows the denied write
		writeOn string          // read-only probe id that writes probe.txt
		want    string
		probes  int
	}{
		{"denied", map[string]bool{"read-only": true}, "", "PASS", 1},
		{"denied on retry", map[string]bool{"read-only-2": true}, "", "PASS", 2},
		{"never attempted", nil, "", "WARN", 2},
		{"written", nil, "read-only", "FAIL", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			h, _ := Lookup("opencode")
			h.Home = nil
			h.Models = nil
			h.Version = func(context.Context) (string, error) { return "test", nil }
			probes := 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }, run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
				st, _ := store.Load(id)
				if id == "write" {
					os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
					os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
				} else {
					probes++
					if !strings.Contains(st.Request.Brief, "echo x > probe.txt") {
						t.Error("doctor does not request a real write attempt")
					}
					if tc.denyOn[id] {
						os.WriteFile(store.LogPath(id), []byte(denial), 0600)
					}
					if tc.writeOn == id {
						os.WriteFile(filepath.Join(st.Request.Cwd, "probe.txt"), []byte("x"), 0600)
					}
				}
				return Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: st.Request.Model, ModelVerified: true, InputTokens: 1, ToolCalls: 1, FinalText: "done"}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			for _, c := range reports[0].Checks {
				if c.Name == "Read-only access" && c.Status != tc.want {
					t.Errorf("check=%+v, want %s", c, tc.want)
				}
			}
			if probes != tc.probes {
				t.Errorf("read-only probes=%d, want %d", probes, tc.probes)
			}
		})
	}
}

var deniedDoctorRaw = json.RawMessage(`{"part":{"tool":"bash","state":{"status":"error","input":{"command":"echo x > probe.txt"},"error":"Permission denied"}}}`)

func writeDeniedDoctorLog(t *testing.T, store Store, id string) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"kind": "tool", "raw": deniedDoctorRaw})
	if err := os.WriteFile(store.LogPath(id), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestReviewReadonlyUnbornBaselineUnavailable(t *testing.T) {
	store, sess, backend := workerFixture(t, "read-only")
	st, _ := store.Load("j1")
	st.Request.Harness = "opencode"
	st.Request.Cwd = unbornRepo(t)
	write(t, st.Request.Cwd, "pre.txt", "before\n")
	write(t, st.Request.Cwd, ".gitattributes", "pre.txt filter=fail\n")
	git(t, st.Request.Cwd, "config", "filter.fail.clean", "false")
	git(t, st.Request.Cwd, "config", "filter.fail.required", "true")
	store.Save(st)
	sess.script = func(s *fakeSession) {
		t.Error("unverifiable readonly job reached harness")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, _ := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
	if r.Status != StatusFailed || !strings.Contains(strings.Join(r.Warnings, " "), "Read-only verification unavailable") {
		t.Errorf("status=%s warnings=%v", r.Status, r.Warnings)
	}
}
