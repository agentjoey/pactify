package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func fakeOpencodeExport(t *testing.T, data, config string) string {
	t.Helper()
	bin := t.TempDir()
	record := filepath.Join(t.TempDir(), "export.json")
	script := fmt.Sprintf("#!/bin/sh\n[ \"$XDG_DATA_HOME\" = '%s' ] || exit 21\n[ \"$XDG_CONFIG_HOME\" = '%s' ] || exit 22\n[ \"$1\" = export ] || exit 23\ncat '%s'\n", data, config, record)
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return record
}

func TestOpencodeIndependentVerificationAndResumeCost(t *testing.T) {
	for _, fault := range []string{"", "mismatch", "export-failed", "no-assistant", "baseline-failed"} {
		t.Run(fault, func(t *testing.T) {
			fakeOpencodeUserHome(t)
			store, s, b := workerFixture(t, "write")
			root, _ := managedHarnessHome("opencode")
			record := fakeOpencodeExport(t, filepath.Join(root, "share"), filepath.Join(root, "config"))
			before := `{"messages":[{"info":{"role":"assistant","providerID":"old","modelID":"old-model","cost":9}}]}`
			if fault != "baseline-failed" {
				os.WriteFile(record, []byte(before), 0600)
			}
			h, ok := Lookup("opencode")
			if !ok {
				t.Fatal("opencode not registered")
			}
			h.Version = nil
			h.Backend = func() cockpit.Backend { return b }
			st, _ := store.Load("j1")
			st.Request.Harness = "opencode"
			st.Request.Model = "test/flash"
			st.Request.Resume = "sid"
			st.ConfigMode = "minimal"
			store.Save(st)
			s.script = func(s *fakeSession) {
				model := "flash"
				if fault == "mismatch" {
					model = "other"
				}
				after := fmt.Sprintf(`{"messages":[{"info":{"role":"assistant","providerID":"old","modelID":"old-model","cost":9}},{"info":{"role":"user"}},{"info":{"role":"assistant","providerID":"test","modelID":%q,"cost":0.25}}]}`, model)
				if fault == "export-failed" {
					os.Remove(record)
				} else if fault == "no-assistant" {
					os.WriteFile(record, []byte(before), 0600)
				} else {
					os.WriteFile(record, []byte(after), 0600)
				}
				s.info.Model = "test/flash"
				s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 300, OutputTokens: 7, CostUSD: 9.25}}
				s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if r.InputTokens != 300 || r.OutputTokens != 7 {
				t.Fatalf("tokens=%+v", r)
			}
			if fault == "" || fault == "mismatch" {
				if !r.ModelVerified || r.CostUSD != 0.25 {
					t.Fatalf("verification=%+v", r)
				}
				if fault == "mismatch" && !contains(r.Warnings, "Actual model does not match") {
					t.Fatal("mismatch warning missing")
				}
			} else if r.ModelVerified || r.CostUSD != 0 || !contains(r.Warnings, "opencode") {
				t.Fatalf("unverified result=%+v", r)
			}
		})
	}
}

func TestOpencodeDoctorDynamicModelAndDenial(t *testing.T) {
	for _, fault := range []string{"", "models", "missing-db", "linked-db", "user-index", "read-only-write", "no-denial", "copy", "config-copy"} {
		t.Run(fault, func(t *testing.T) {
			data, config := fakeOpencodeUserHome(t)
			h, ok := Lookup("opencode")
			if !ok {
				t.Fatal("not registered")
			}
			h.Version = func(context.Context) (string, error) { return "1.19.0", nil }
			root, _ := managedHarnessHome("opencode")
			h.Models = func(_ context.Context, o cockpit.StartOpts) ([]string, error) {
				if o.RepoDir == "" {
					t.Error("model discovery needs an absolute cwd")
				}
				for _, e := range []string{"XDG_DATA_HOME=" + filepath.Join(root, "share"), "XDG_CONFIG_HOME=" + filepath.Join(root, "config")} {
					if !contains(o.Env, e) {
						t.Fatal("model discovery not isolated")
					}
				}
				if fault == "models" {
					return nil, fmt.Errorf("no models")
				}
				return []string{"deepseek/deepseek-v4-pro", "test/flash-fast", "test/large"}, nil
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}, []Harness{h}, doctorDeps{
				lookPath: func(string) (string, error) { return "fake", nil },
				run: func(ctx context.Context, store Store, id string, h Harness, _ WorkerConfig) (Result, error) {
					st, _ := store.Load(id)
					if st.Request.Model != "test/flash-fast" {
						t.Error("doctor did not select flash model", st.Request.Model)
					}
					// Exercise actual directory preparation without invoking a real harness.
					sess := &fakeSession{events: make(chan cockpit.Event, 8)}
					b := &fakeBackend{sess: sess}
					h.Backend = func() cockpit.Backend { return b }
					h.Verify = nil
					h.Version = func(context.Context) (string, error) { return "1.19.0", nil }
					sess.script = func(s *fakeSession) {
						db := filepath.Join(root, "share/opencode/opencode.db")
						if fault != "missing-db" {
							os.WriteFile(db, []byte("fixture"), 0600)
						}
						if fault == "linked-db" {
							os.Remove(db)
							os.Symlink(filepath.Join(data, "opencode/opencode.db"), db)
						}
						if fault == "user-index" {
							os.WriteFile(filepath.Join(data, "opencode/opencode.db"), []byte("unexpected"), 0600)
						}
						if fault == "copy" {
							p := filepath.Join(root, "share/opencode/auth.json")
							os.Remove(p)
							os.WriteFile(p, []byte("fixture copy"), 0600)
						}
						if fault == "config-copy" {
							p := filepath.Join(root, "config/git")
							os.Remove(p)
							os.Mkdir(p, 0700)
						}
						if id == "write" || fault == "read-only-write" {
							os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
						}
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1}}
						if id == "read-only" && fault != "no-denial" {
							s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnFailed, Err: "opencode read-only access cannot run shell commands; use read tools only"}
							return
						}
						s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
						s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
					}
					r, e := RunJob(ctx, store, id, h, WorkerConfig{IdleTimeout: time.Second})
					r.ModelVerified = true
					r.ModelActual = st.Request.Model
					return r, e
				},
			})
			_ = config
			if err != nil || len(reports) != 1 || reports[0].Passed() != (fault == "") {
				t.Fatalf("reports=%+v err=%v", reports, err)
			}
			table := readVerifiedVersions(filepath.Join(os.Getenv("PACTIFY_HOME"), "verified-versions.json"))
			if (table["opencode"].Version == "1.19.0") != (fault == "") {
				t.Fatal("bad version record", table)
			}
		})
	}
}

func TestOpencodeResumeKeepsBothDirectories(t *testing.T) {
	fakeOpencodeUserHome(t)
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	st, err := store.Create("one", Request{Harness: "opencode", Cwd: t.TempDir(), Access: "write"})
	if err != nil {
		t.Fatal(err)
	}
	st.HarnessHome = "/old/share"
	st.HarnessHomes = map[string]string{"XDG_CONFIG_HOME": "/old/config"}
	store.Save(st)
	store.Finish(st.ID, &Result{Status: StatusSucceeded, SessionID: "sid"}, "", time.Now())
	next, err := store.CreateRun("two", st.Request, false, true)
	if err != nil || next.HarnessHome != st.HarnessHome || next.HarnessHomes["XDG_CONFIG_HOME"] != "/old/config" {
		t.Fatalf("resume=%+v %v", next, err)
	}
	st.Request.WithUserConfig = true
	if _, err = store.CreateRun("three", st.Request, false, true); err == nil || !strings.Contains(err.Error(), "config mode") {
		t.Fatal("resume changed isolation", err)
	}
}

func TestOpencodeExportUsesRuntimeEnvironment(t *testing.T) {
	fakeOpencodeUserHome(t)
	t.Setenv("PACT_RELAY_TOKEN", "fixture-secret")
	t.Setenv("PACTIFY_PRIVATE_TEST", "fixture-secret")
	bin := t.TempDir()
	script := `#!/bin/sh
[ -z "$PACT_RELAY_TOKEN" ] || exit 31
[ -z "$PACTIFY_PRIVATE_TEST" ] || exit 32
[ "$OPENCODE_PURE" = 1 ] || exit 33
[ "$OPENCODE_DISABLE_CLAUDE_CODE" = 1 ] || exit 34
[ "$OPENCODE_DISABLE_EXTERNAL_SKILLS" = 1 ] || exit 35
[ -z "$OPENAI_API_KEY" ] || exit 36
printf '{"messages":[]}'
`
	os.WriteFile(filepath.Join(bin, "opencode"), []byte(script), 0700)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("OPENAI_API_KEY", "fixture-key")
	env, err := cockpit.OpencodeEnv(cockpit.StartOpts{Access: cockpit.AccessWrite, MinimalConfig: true, Env: opencodeVendorStrip()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = opencodeExport(context.Background(), VerificationInput{SessionID: "sid", Cwd: t.TempDir(), Env: env}); err != nil {
		t.Fatal(err)
	}
}
