package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestDoctorChecksAndIsolation(t *testing.T) {
	for _, name := range []string{"codex", "kimi"} {
		for _, fault := range []string{"", "missing", "version", "run", "write", "reply", "tokens", "model", "unverified", "readonly-write", "readonly-no-tool", "readonly-failed", "network-bind", "network-external", "network-missing", "conceal", "archive", "version-drift"} {
			if name == "kimi" && (strings.HasPrefix(fault, "readonly") || strings.HasPrefix(fault, "network")) {
				continue
			}
			t.Run(name+"/"+fault, func(t *testing.T) {
				home := t.TempDir()
				t.Setenv("PACTIFY_HOME", home)
				if name == "kimi" {
					fakeKimiUserHome(t)
				}
				store := Store{Root: filepath.Join(home, "jobs")}
				tempParent := t.TempDir()
				h, _ := Lookup(name)
				h.Version = func(context.Context) (string, error) {
					if fault == "version" {
						return "", errors.New("no version")
					}
					return "1.2.3", nil
				}
				calls := 0
				d := doctorDeps{tempParent: tempParent, lookPath: func(string) (string, error) {
					if fault == "missing" {
						return "", errors.New("not found")
					}
					return "/test/cli", nil
				}, now: func() time.Time { return time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC) }}
				d.run = func(ctx context.Context, s Store, id string, h Harness, cfg WorkerConfig) (Result, error) {
					calls++
					st, err := s.Load(id)
					if err != nil {
						t.Fatal(err)
					}
					req := st.Request
					if !strings.HasPrefix(s.Root, tempParent+string(os.PathSeparator)) || !strings.HasPrefix(req.Cwd, tempParent+string(os.PathSeparator)) || s.Root == store.Root {
						t.Fatalf("not isolated: %+v %+v", s, req)
					}
					if req.Model == "" || req.Resume != "" {
						t.Fatalf("request=%+v", req)
					}
					if _, err := runGit(req.Cwd, "rev-parse", "HEAD"); err != nil {
						t.Fatal(err)
					}
					r := Result{Status: StatusSucceeded, CLIVersion: "1.2.3", ModelVerified: true, ModelActual: req.Model, FinalText: "done", InputTokens: 10, ToolCalls: 1}
					if name == "kimi" {
						// AJ-643 requires independent managed-home evidence as
						// well as the synthetic worker result.
						managed, err := managedKimiHome()
						if err != nil {
							t.Fatal(err)
						}
						source, err := kimiHomeDir()
						if err != nil {
							t.Fatal(err)
						}
						if err := prepareKimiHome(ctx, managed, source); err != nil {
							t.Fatal(err)
						}
						st.HarnessHome = managed
						if err := s.Save(st); err != nil {
							t.Fatal(err)
						}
						r.SessionID = "probe"
						writeKimiJobRecord(t, managed, r.SessionID, "")
					}
					if req.Access == "write" {
						if fault != "write" {
							if err := os.WriteFile(filepath.Join(req.Cwd, "out.txt"), []byte("ok"), 0600); err != nil {
								t.Fatal(err)
							}
						}
						if name == "codex" && fault != "network-missing" {
							data, _ := json.Marshal(map[string]bool{"bind": fault != "network-bind", "external": fault != "network-external"})
							if err := os.WriteFile(filepath.Join(req.Cwd, "doctor-network.json"), data, 0600); err != nil {
								t.Fatal(err)
							}
							if !strings.Contains(req.Brief, "python3 doctor-network.py") {
								t.Fatalf("missing network command: %s", req.Brief)
							}
						}
						switch fault {
						case "run":
							return r, errors.New("start failed")
						case "reply":
							r.FinalText = " "
						case "tokens":
							r.InputTokens = 0
						case "model":
							r.ModelActual = "wrong"
						case "unverified":
							r.ModelVerified = false
						case "version-drift":
							r.CLIVersion = "2"
						}
					} else {
						if _, err := os.Stat(filepath.Join(req.Cwd, "out.txt")); !os.IsNotExist(err) {
							t.Fatalf("read-only repo not fresh: %v", err)
						}
						switch fault {
						case "readonly-write":
							_ = os.WriteFile(filepath.Join(req.Cwd, "out.txt"), []byte("bad"), 0600)
						case "readonly-no-tool":
							r.ToolCalls = 0
						case "readonly-failed":
							r.Status = StatusFailed
						}
					}
					switch fault {
					case "conceal":
						r.Warnings = []string{"Session hiding failed: test"}
					case "archive":
						r.Warnings = []string{"Session archiving failed: test"}
					}
					return r, nil
				}
				reports, err := runDoctor(context.Background(), store, []Harness{h}, d)
				if err != nil {
					t.Fatal(err)
				}
				if len(reports) != 1 || reports[0].Passed() != (fault == "") {
					t.Fatalf("fault=%s reports=%+v", fault, reports)
				}
				wantChecks := 6
				if name == "kimi" {
					wantChecks = 7
				}
				if len(reports[0].Checks) != wantChecks {
					t.Fatalf("checks=%+v", reports[0].Checks)
				}
				if name == "kimi" && reports[0].Checks[3].Status != "SKIP" {
					t.Fatalf("kimi readonly=%+v", reports[0].Checks[3])
				}
				if fault == "missing" || fault == "version" {
					if calls != 0 {
						t.Fatalf("ran missing CLI %d times", calls)
					}
				} else if calls != map[string]int{"codex": 2, "kimi": 1}[name] {
					t.Fatalf("calls=%d", calls)
				}
				entries, err := os.ReadDir(tempParent)
				if err != nil || len(entries) != 0 {
					t.Fatalf("temporary files leaked: %v %v", entries, err)
				}
				if _, err := os.Stat(store.Root); !os.IsNotExist(err) {
					t.Fatalf("polluted jobs: %v", err)
				}
				table := readVerifiedVersions(filepath.Join(home, "verified-versions.json"))
				if (table[name].Version == "1.2.3") != (fault == "") {
					t.Fatalf("table=%+v", table)
				}
			})
		}
	}
}

func TestVerifiedVersionsMergeAndRecovery(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "verified-versions.json")
	if err := os.WriteFile(path, []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, name := range []string{"codex", "kimi"} {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			if err := recordVerifiedVersion(context.Background(), home, name, "1", time.Now()); err != nil {
				t.Error(err)
			}
		}(name)
	}
	wg.Wait()
	table := readVerifiedVersions(path)
	if len(table) != 2 || table["codex"].Version != "1" || table["kimi"].VerifiedAt.IsZero() {
		t.Fatalf("table=%+v", table)
	}
	if err := recordVerifiedVersion(context.Background(), home, "codex", "2", time.Now()); err != nil {
		t.Fatal(err)
	}
	table = readVerifiedVersions(path)
	if len(table) != 2 || table["codex"].Version != "2" {
		t.Fatalf("table=%+v", table)
	}
}

// Exercise orchestration through the actual worker, replacing only the external
// harness boundary. This catches lost Hidden/env options and skipped Conceal.
func TestDoctorUsesRunJobAndConcealsEveryProbe(t *testing.T) {
	for _, concealFails := range []bool{false, true} {
		t.Run(map[bool]string{false: "hidden", true: "conceal-failed"}[concealFails], func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			h, _ := Lookup("codex")
			h.Version = func(context.Context) (string, error) { return "test", nil }
			count := 0
			h.Backend = func() cockpit.Backend {
				s := &fakeSession{events: make(chan cockpit.Event, 8)}
				b := &fakeBackend{sess: s}
				s.script = func(s *fakeSession) {
					o := b.gotOpts
					if !o.Hidden || !contains(o.Env, "PACTIFY_HOME=") || contains(o.Env, "PACTIFY_HOME="+home) {
						t.Error("worker did not isolate hidden probe")
					}
					s.info.Model = o.Model
					if o.Access == cockpit.AccessWrite {
						if err := os.WriteFile(filepath.Join(o.RepoDir, "out.txt"), []byte("ok"), 0600); err != nil {
							t.Error(err)
						}
						if err := os.WriteFile(filepath.Join(o.RepoDir, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600); err != nil {
							t.Error(err)
						}
					}
					s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1, OutputTokens: 1}}
					s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start"}}
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "done"}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				return b
			}
			h.Conceal = func(ConcealContext) error {
				count++
				if concealFails {
					return errors.New("archive API failed")
				}
				return nil
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "test", nil }})
			if err != nil || len(reports) != 1 || reports[0].Passed() == concealFails || count != 2 {
				t.Fatalf("reports=%+v count=%d err=%v", reports, count, err)
			}
		})
	}
}

func TestDoctorNetworkScript(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 is unavailable")
	}
	for _, code := range []int{200, 503} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(code); _, _ = w.Write([]byte("ok")) }))
			defer server.Close()
			dir := t.TempDir()
			// Replace only the external HTTP endpoint; execute the actual probe's bind,
			// request, status validation, and evidence write with a real Python process.
			wrapper := "import sys, urllib.request\noriginal = urllib.request.urlopen\nurllib.request.urlopen = lambda url, **kw: original(sys.argv[2], **kw)\nexec(sys.argv[1])\n"
			cmd := exec.Command(python, "-c", wrapper, doctorNetworkScript, server.URL)
			cmd.Dir = dir
			output, err := cmd.CombinedOutput()
			data, fileErr := os.ReadFile(filepath.Join(dir, "doctor-network.json"))
			if code == 200 {
				if err != nil || fileErr != nil || !strings.Contains(string(data), `"bind": true`) || !strings.Contains(string(data), `"external": true`) {
					t.Fatalf("output=%s err=%v evidence=%s err=%v", output, err, data, fileErr)
				}
			} else if err == nil || !os.IsNotExist(fileErr) {
				t.Fatalf("failed request created evidence: %s %v", data, err)
			}
		})
	}
}

func TestDoctorFailureDoesNotOverwriteVerifiedVersion(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	if err := recordVerifiedVersion(context.Background(), home, "codex", "previous", time.Now()); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("codex")
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "", errors.New("missing") }})
	table := readVerifiedVersions(filepath.Join(home, "verified-versions.json"))
	if err != nil || reports[0].Passed() || table["codex"].Version != "previous" {
		t.Fatalf("reports=%+v table=%+v err=%v", reports, table, err)
	}
}

func TestDoctorCanceledProbesCleanUp(t *testing.T) {
	parent := t.TempDir()
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h, _ := Lookup("codex")
	h.Version = func(context.Context) (string, error) { return "1", nil }
	reports, err := runDoctor(ctx, Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{tempParent: parent, lookPath: func(string) (string, error) { return "test", nil }, run: func(ctx context.Context, _ Store, _ string, _ Harness, _ WorkerConfig) (Result, error) {
		return Result{Status: StatusCanceled}, ctx.Err()
	}})
	entries, _ := os.ReadDir(parent)
	table := readVerifiedVersions(filepath.Join(home, "verified-versions.json"))
	if err != nil || reports[0].Passed() || len(entries) != 0 || len(table) != 0 {
		t.Fatalf("reports=%+v files=%v table=%v err=%v", reports, entries, table, err)
	}
}

func TestDoctorReportsVersionTableWriteFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PACTIFY_SUB_FAKE", "1")
	if err := os.Mkdir(filepath.Join(home, "verified-versions.json"), 0700); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	passed, err := Doctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []string{"fake"}, &output)
	if passed || err == nil || !strings.Contains(err.Error(), "verified version") {
		t.Fatalf("passed=%t output=%s err=%v", passed, output.String(), err)
	}
	if _, err := os.Stat(filepath.Join(home, "jobs")); !os.IsNotExist(err) {
		t.Fatalf("polluted jobs: %v", err)
	}
}
