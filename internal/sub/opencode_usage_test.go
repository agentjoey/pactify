package sub

import (
	"context"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOpencodeDoctorDynamicModelAndIsolation(t *testing.T) {
	for _, fault := range []string{"", "models", "missing-db", "linked-db", "user-index", "copy", "config-copy"} {
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
						if id == "write" {
							os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
							os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
						}
						s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start"}}
						if id == "read-only" {
							s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "end", Name: "bash"}, Raw: deniedDoctorRaw}
						}
						s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 1}}
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
