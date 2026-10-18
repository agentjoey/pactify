package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencode2Peer(t *testing.T) { testenv.Opencode2Peer(t) }
func TestOpencode2VersionSelectionAndWorker(t *testing.T) {
	for _, access := range []string{"write", "read-only"} {
		t.Run(access, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			testenv.InstallOpencode2(t)
			if access == "read-only" {
				t.Setenv("COCKPIT_OC2_SCENARIO", "readonly")
			}
			h, _ := Lookup("opencode")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			version, err := h.Version(ctx)
			if err != nil || version != "v2.0.32" {
				t.Fatalf("version=%s err=%v", version, err)
			}
			selected := h.forVersion(version)
			if fmt.Sprintf("%T", selected.Backend()) != "*cockpit.opencode2Backend" || len(selected.Home.directories()) != 4 || selected.ModelDetails == nil {
				t.Fatal("incomplete v2 selection")
			}
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			_, err = store.Create("v2", Request{Harness: "opencode", Access: access, Model: "opencode/test-free", Effort: "low", Cwd: repo, Brief: "synthetic"})
			if err != nil {
				t.Fatal(err)
			}
			result, err := RunJob(ctx, store, "v2", h, WorkerConfig{})
			if err != nil || result.Status != StatusSucceeded || !result.ModelVerified || result.InputTokens != 45 || result.OutputTokens != 15 {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			st, _ := store.Load("v2")
			if !strings.Contains(st.HarnessHome, "opencode2/share") || len(st.HarnessHomes) != 3 {
				t.Fatalf("homes=%+v", st)
			}
			if err := doctorOpencode2Home(store, "v2", result, true); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(filepath.Join(st.HarnessHome, "opencode/auth.json")); !os.IsNotExist(err) {
				t.Fatal("v2 must not link auth.json")
			}
		})
	}
	calls := 0
	version := onceVersion(func(context.Context) (string, error) { calls++; return "v2.0.32", nil })
	version(context.Background())
	version(context.Background())
	if calls != 1 {
		t.Fatal("version probed more than once")
	}
}
func TestOpencode2CrossVersionResume(t *testing.T) {
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	os.MkdirAll(store.Dir("old"), 0700)
	for _, pair := range [][2]string{{"1.19.0", "v2.0.32"}, {"v2.0.32", "1.19.0"}, {"v2.0.32", "v2.0.33"}} {
		b, _ := json.Marshal(Result{CLIVersion: pair[0], SessionID: "ses_same_shape"})
		os.WriteFile(store.ResultPath("old"), b, 0600)
		st := State{ResumeOf: "old", Request: Request{Resume: "ses_same_shape"}}
		err := store.checkOpencodeResume(st, pair[1])
		want := opencodeMajor(pair[0]) != opencodeMajor(pair[1])
		if (err != nil) != want {
			t.Fatalf("pair=%v err=%v", pair, err)
		}
	}
}
func TestOpencode2UserConfigHomeAndConceal(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		t.Setenv(key, t.TempDir())
	}
	h, _ := Lookup("opencode")
	h = h.forVersion("v2.0.32")
	_, _, env, layouts, err := prepareHarnessHomes(context.Background(), h.Home, "", nil, false, "")
	if err != nil || len(layouts) != 0 {
		t.Fatal(err)
	}
	for _, entry := range env {
		key, value, _ := strings.Cut(entry, "=")
		if os.Getenv(key) != value {
			t.Fatal(entry)
		}
	}
	if err := h.Conceal(ConcealContext{}); err == nil || !strings.Contains(err.Error(), "user's session list") {
		t.Fatal(err)
	}
}
func TestOpencode2CatalogUsesV2(t *testing.T) {
	testenv.InstallOpencode2(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("opencode")
	catalog := newHarnessCatalog()
	row := catalog.harness(context.Background(), Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}, h, HarnessDefault{}, true)
	if row.Version != "v2.0.32" || row.Models.Error != "" || len(row.Models.Items) != 1 || len(row.Efforts) != 1 || row.Efforts[0] != "low" {
		t.Fatalf("catalog=%+v", row)
	}
}

func TestOpencode2DoctorSelectsBackendAndPrivateDatabase(t *testing.T) {
	testenv.InstallOpencode2(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("opencode")
	calls := 0
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "synthetic", nil }, run: func(_ context.Context, store Store, id string, h Harness, _ WorkerConfig) (Result, error) {
		calls++
		if fmt.Sprintf("%T", h.Backend()) != "*cockpit.opencode2Backend" {
			t.Error("doctor selected wrong backend")
		}
		st, _ := store.Load(id)
		if id == "write" {
			os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
			os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
		} else {
			os.WriteFile(store.LogPath(id), []byte(`{"kind":"tool","tool":{"name":"shell","phase":"input"},"raw":{"assistantMessageID":"msg_a","id":"call1","name":"shell","input":{"command":"echo x > probe.txt"}}}`+"\n"+`{"kind":"tool","tool":{"name":"shell","phase":"end"},"raw":{"assistantMessageID":"msg_a","id":"call1","error":{"message":"Permission denied"}}}`+"\n"), 0600)
		}
		root, _ := managedHarnessHome("opencode2")
		st.HarnessHome = filepath.Join(root, "share")
		st.HarnessHomes = map[string]string{"XDG_CONFIG_HOME": filepath.Join(root, "config"), "XDG_STATE_HOME": filepath.Join(root, "state"), "XDG_CACHE_HOME": filepath.Join(root, "cache")}
		store.Save(st)
		return Result{SessionID: "ses_root", Status: StatusSucceeded, CLIVersion: "v2.0.32", ModelVerified: true, ModelActual: st.Request.Model, InputTokens: 1, ToolCalls: 1, FinalText: "done"}, nil
	}})
	if err != nil || len(reports) != 1 || calls != 2 {
		t.Fatalf("reports=%+v calls=%d err=%v", reports, calls, err)
	}
	for _, check := range reports[0].Checks {
		if check.Name == "Managed home" || check.Name == "Session hiding" || check.Name == "Read-only access" {
			if check.Status != "PASS" {
				t.Errorf("check=%+v", check)
			}
		}
	}
}

func TestOpencode2UserInterruptionIsCanceled(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	testenv.InstallOpencode2(t)
	t.Setenv("COCKPIT_OC2_SCENARIO", "user")
	repo := t.TempDir()
	if err := initDoctorRepo(repo, false); err != nil {
		t.Fatal(err)
	}
	store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
	if _, err := store.Create("user-stop", Request{Harness: "opencode", Access: "write", Model: "opencode/test-free", Cwd: repo, Brief: "synthetic"}); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("opencode")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, err := RunJob(ctx, store, "user-stop", h, WorkerConfig{})
	if err != nil || r.Status != StatusCanceled || r.FailureClass != "canceled" {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}

func TestOpencodeVersionProbeErrorStopsWorkerAndCatalog(t *testing.T) {
	store, _, backend := workerFixture(t, "write")
	h := newFakeHarness(backend)
	h.Name = "opencode"
	h.Version = func(context.Context) (string, error) { return "", fmt.Errorf("synthetic version failure") }
	_, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err == nil || !strings.Contains(err.Error(), "cannot determine opencode CLI version") || backend.gotOpts.Env != nil {
		t.Fatalf("err=%v started=%t", err, backend.gotOpts.Env != nil)
	}
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
		t.Fatal("discovery started after version failure")
		return nil, nil
	}
	catalog := newHarnessCatalog()
	catalog.lookupPath = func(string) (string, error) { return "fake", nil }
	row := catalog.harness(context.Background(), store, h, HarnessDefault{}, false)
	if !strings.Contains(row.Models.Error, "synthetic version failure") {
		t.Fatalf("row=%+v", row)
	}
}
func TestOpencodeCatalogInvalidatesAcrossVersions(t *testing.T) {
	testenv.InstallOpencode2(t)
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("opencode")
	h.Home = nil
	h.Version = func(context.Context) (string, error) { return "1.19.0", nil }
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) { return []string{"opencode/v1-only"}, nil }
	catalog := newHarnessCatalog()
	store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
	first := catalog.harness(context.Background(), store, h, HarnessDefault{}, false)
	if len(first.Models.Items) != 1 || first.Models.Items[0] != "opencode/v1-only" {
		t.Fatalf("first=%+v", first)
	}
	h.Version = func(context.Context) (string, error) { return "v2.0.32", nil }
	second := catalog.harness(context.Background(), store, h, HarnessDefault{}, false)
	if second.Models.Error != "" || len(second.Models.Items) != 1 || second.Models.Items[0] != "opencode/test-free" {
		t.Fatalf("stale v1 catalog survived version change: %+v", second)
	}
}

func TestOpencode2BackgroundWarningStaysInformational(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(fmt.Sprint(failed), func(t *testing.T) {
			store, session, backend := workerFixture(t, "write")
			if err := store.WithState(context.Background(), "j1", func(st *State) error { st.Request.Harness = "opencode"; return store.Save(*st) }); err != nil {
				t.Fatal(err)
			}
			session.script = func(s *fakeSession) {
				s.events <- cockpit.Event{Kind: cockpit.EventSystem, State: "warning", Text: "Opencode background child session is still active"}
				if failed {
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "Quota exhausted", Raw: json.RawMessage(`{"type":"provider.quota","message":"Quota exhausted"}`)}
				} else {
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "Actual result"}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
			}
			_, err := RunJob(context.Background(), store, "j1", newFakeHarness(backend), WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			st, _ := store.Load("j1")
			want := ""
			if failed {
				want = "Quota exhausted"
			}
			if st.Error != want {
				t.Fatalf("error=%q want=%q", st.Error, want)
			}
		})
	}
}

func TestOpencode2ResumeRequiresKnownMajorAndPrivateHome(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
	os.MkdirAll(store.Dir("old"), 0700)
	st := State{ResumeOf: "old", Request: Request{Resume: "ses_same_shape"}}
	for _, content := range []string{"", `{}`, `{"cliVersion":"unknown"}`, `{"cliVersion":"1.19.0"}`} {
		if content == "" {
			os.Remove(store.ResultPath("old"))
		} else {
			os.WriteFile(store.ResultPath("old"), []byte(content), 0600)
		}
		if err := store.checkOpencodeResume(st, "v2.0.32"); err == nil {
			t.Fatal("v2 accepted unknown or different previous major", content)
		}
	}
	os.Remove(store.ResultPath("old"))
	if err := store.checkOpencodeResume(st, "1.19.0"); err != nil {
		t.Fatal("missing old result broke v1 resume", err)
	}
	if got := store.opencodeResumeWarning(st, "1.19.0"); got != opencodeResumeUnknownWarning {
		t.Fatal("missing v1 provenance warning", got)
	}
	external := State{Request: Request{Resume: "ses_external"}}
	if err := store.checkOpencodeResume(external, "v2.0.32"); err == nil {
		t.Fatal("unversioned external resume accepted")
	}
	st.ConfigMode = "minimal"
	for _, home := range []string{"/unexpected/old-v1", filepath.Join(os.Getenv("PACTIFY_HOME"), "harness-homes/opencode/share")} {
		st.HarnessHome = home
		if err := checkOpencode2SavedHomes(st); err == nil {
			t.Fatal("saved foreign home accepted", home)
		}
		if _, err := os.Stat(home); !os.IsNotExist(err) {
			t.Fatal("guard touched saved home", err)
		}
	}
	st.HarnessHome = ""
	for i, d := range opencode2Homes().directories() {
		home, _ := d.Managed()
		if i == 0 {
			st.HarnessHome = home
		} else {
			if st.HarnessHomes == nil {
				st.HarnessHomes = map[string]string{}
			}
			st.HarnessHomes[d.Env] = home
		}
	}
	if err := checkOpencode2SavedHomes(st); err != nil {
		t.Fatal(err)
	}
	st.HarnessHomes["XDG_CACHE_HOME"] = "/old/cache"
	if err := checkOpencode2SavedHomes(st); err == nil {
		t.Fatal("foreign auxiliary home accepted")
	}
}
