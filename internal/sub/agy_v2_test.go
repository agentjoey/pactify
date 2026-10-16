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
)

func TestAgyV2ReadOnlyBaseline(t *testing.T) {
	for _, kind := range []string{"tracked", "untracked", "ignored", "missing"} {
		t.Run(kind, func(t *testing.T) {
			store, _, _ := workerFixture(t, "read-only")
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			path := filepath.Join(st.Request.Cwd, "probe.txt")
			_ = os.WriteFile(path, []byte("unchanged"), 0600)
			if kind == "ignored" {
				_ = os.WriteFile(filepath.Join(st.Request.Cwd, ".gitignore"), []byte("probe.txt\n"), 0600)
			}
			if kind == "tracked" {
				_, _ = runGit(st.Request.Cwd, "add", "probe.txt")
			}
			st.ReadOnlyBaseline, _ = captureReadOnly(st.Request.Cwd)
			st.StartedAt = time.Now()
			if kind == "missing" {
				st.ReadOnlyBaseline = nil
			} else {
				_ = os.Remove(path)
			}
			_ = store.Save(st)
			r := Result{Status: StatusSucceeded}
			if err := store.Finish("j1", &r, "", time.Now()); err != nil {
				t.Fatal(err)
			}
			if r.Status != StatusFailed || kind != "missing" && r.FailureClass != "readonly_violation" {
				t.Fatalf("result=%+v", r)
			}
		})
	}
}
func TestAgyV2QuotaSnapshotStorage(t *testing.T) {
	store, _, _ := workerFixture(t, "write")
	observed := time.Now().UTC()
	snapshot := cockpit.QuotaObservation{ObservedAt: observed, Buckets: []cockpit.QuotaBucket{{ID: "gemini-weekly", Window: "weekly", RemainingFraction: .75, ResetAt: observed.Add(time.Hour)}}}
	if err := store.rememberQuotaSnapshot("j1", "agy", snapshot); err != nil {
		t.Fatal(err)
	}
	if err := store.rememberQuota(QuotaRoute{Harness: "agy", Model: "model", RecordedAt: observed, ResetAt: &snapshot.Buckets[0].ResetAt}); err != nil {
		t.Fatal(err)
	}
	routes, err := store.ExhaustedRoutes(observed)
	if err != nil || len(routes) != 1 {
		t.Fatalf("routes=%+v err=%v", routes, err)
	}
	router, err := store.RouterSnapshot(observed)
	if err != nil || len(router.ExhaustedRoutes) != 1 {
		t.Fatalf("router=%+v err=%v", router, err)
	}
	data, _ := os.ReadFile(filepath.Join(filepath.Dir(store.Root), "quota-snapshots.json"))
	if !strings.Contains(string(data), "remainingFraction") {
		t.Fatal(string(data))
	}
	r := Result{Harness: "agy", Status: StatusSucceeded, RateLimit: snapshot.Summary(), QuotaSnapshots: []cockpit.QuotaObservation{snapshot}}
	if err := store.Finish("j1", &r, "", observed); err != nil {
		t.Fatal(err)
	}
	report, err := store.CostReport(time.Time{}, "harness", observed.Add(time.Minute))
	if err != nil || len(report.QuotaSnapshots) != 1 || len(report.QuotaSnapshots[0].Buckets) != 1 {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	encoded, _ := json.Marshal(report)
	if !strings.Contains(string(encoded), "resetAt") {
		t.Fatal(string(encoded))
	}
}
func TestAgyV2DoctorDenial(t *testing.T) {
	file := filepath.Join(t.TempDir(), "log.jsonl")
	_ = os.WriteFile(file, []byte(`{"kind":"tool","raw":{"event":"step_update","step_update":{"step_type":"tool","state":"ERROR","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"echo x > probe.txt"},"error":{"message":"tool call denied by pre-tool hook"}}}}}`+"\n"), 0600)
	if !doctorDeniedWrite(file, "agy") {
		t.Fatal("missing hook denial evidence")
	}
}
func TestAgyV2EffortCatalog(t *testing.T) {
	h, _ := Lookup("agy")
	if strings.Join(h.Efforts(), ",") != "low,medium,high,xhigh,max" {
		t.Fatal(h.Efforts())
	}
}

func writeAgyDeniedDoctorLog(t *testing.T, store Store, id string) {
	t.Helper()
	data := `{"kind":"tool","raw":{"event":"step_update","step_update":{"step_type":"tool","state":"ERROR","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"echo x > probe.txt"},"error":{"message":"tool call denied by pre-tool hook"}}}}}` + "\n"
	if err := os.WriteFile(store.LogPath(id), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestAgyV2VersionPinsModelAndPrivateHome(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	source := t.TempDir()
	t.Setenv("HOME", source)
	bin := t.TempDir()
	script := `#!/bin/sh
case " $* " in *' --model gemini-3.8-flash-low '*) ;; *) exit 81;; esac
[ "$HOME" != "$AJ711_SOURCE_HOME" ] || exit 82
printf 'agy 1.4.0\n'
`
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AJ711_SOURCE_HOME", source)
	h, _ := Lookup("agy")
	version, err := h.Version(context.Background())
	if err != nil || version != "1.4.0" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}

func TestAgyV2DoctorInconclusiveRetryKeepsReadOnlyAccess(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	h, _ := Lookup("agy")
	h.Home = nil
	h.Models = nil
	h.Version = func(context.Context) (string, error) { return "1.4.0", nil }
	h.HidesItself = true
	probes := 0
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
		lookPath: func(string) (string, error) { return "fixture", nil },
		run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
			st, _ := store.Load(id)
			if id == "write" {
				_ = os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
				_ = os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
			} else if id != "read-only-hook" {
				probes++
				if st.Request.Access != "read-only" {
					t.Errorf("retry changed access to %q", st.Request.Access)
				}
			}
			return Result{Status: StatusSucceeded, CLIVersion: "1.4.0", ModelActual: st.Request.Model, ModelVerified: true, ToolCalls: 1, InputTokens: 1, FinalText: "No write attempted"}, nil
		},
	})
	if err != nil || len(reports) != 1 || !reports[0].Passed() || probes != 2 {
		t.Fatalf("reports=%+v probes=%d err=%v", reports, probes, err)
	}
	for _, check := range reports[0].Checks {
		if check.Name == "Read-only access" && check.Status != "WARN" {
			t.Fatal(check)
		}
	}
}

func writeAgyHookDeniedDoctorLog(t *testing.T, store Store, id string) {
	t.Helper()
	data := `{"kind":"tool","raw":{"event":"step_update","step_update":{"step_type":"tool","state":"ERROR","tool_name":"write_to_file","tool_info":{"parameters":{"TargetFile":"hook-probe.txt"},"error":{"message":"tool call denied by pre-tool hook: Pactify read-only policy: writing tools are denied"}}}}}` + "\n"
	if err := os.WriteFile(store.LogPath(id), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
