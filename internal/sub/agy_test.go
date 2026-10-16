package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

func fakeAgyUserHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".gemini", "antigravity-cli"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, agyTokenPath), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "projects"), 0700); err != nil {
		t.Fatal(err)
	}
	return home
}
func TestAgyWorkerHomeAndResume(t *testing.T) {
	for _, user := range []bool{false, true} {
		t.Run(fmt.Sprint(user), func(t *testing.T) {
			store, s, b := workerFixture(t, "write")
			source := fakeAgyUserHome(t)
			st, _ := store.Load("j1")
			st.Request.Harness = "agy"
			st.Request.WithUserConfig = user
			st.ConfigMode = "minimal"
			if user {
				st.ConfigMode = "user"
			}
			store.Save(st)
			h, _ := Lookup("agy")
			h.Version = nil
			h.Backend = func() cockpit.Backend { return b }
			want := source
			if !user {
				want, _ = managedAgyHome()
			}
			s.script = func(s *fakeSession) {
				if !contains(b.gotOpts.Env, "HOME="+want) {
					t.Error("wrong child HOME", b.gotOpts.Env)
				}
				for _, entry := range orchestrate.CrossVendorStrip("antigravity") {
					if !contains(b.gotOpts.Env, entry) {
						t.Error("missing vendor key override", strings.SplitN(entry, "=", 2)[0])
					}
				}
				if !user {
					if err := agyHome(want, source).Check(); err != nil {
						t.Error(err)
					}
				}
				s.info.Model = "model-a"
				s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 200, OutputTokens: 7}}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil || r.Status != StatusSucceeded || r.SessionID != "th-fake" || !r.ModelVerified || r.InputTokens != 200 {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			if user && !contains(r.Warnings, "user's session list") {
				t.Fatal("missing user-config visibility warning", r.Warnings)
			}
			st, _ = store.Load("j1")
			if st.HarnessHome != want {
				t.Fatal("home not persisted", st)
			}
			st.Request.WithUserConfig = user
			next, err := store.CreateRun("next", st.Request, false, true)
			if err != nil || next.HarnessHome != want || next.Request.Resume != "th-fake" {
				t.Fatalf("resume=%+v err=%v", next, err)
			}
		})
	}
}

func TestAgyDoctorEvidence(t *testing.T) {
	for _, fault := range []string{"", "missing-db", "linked-db", "user-index", "link", "models", "model-copy"} {
		t.Run(fault, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			source := fakeAgyUserHome(t)
			h, _ := Lookup("agy")
			h.Version = func(context.Context) (string, error) { return "1.3.5", nil }
			h.Models = func(_ context.Context, o cockpit.StartOpts) ([]string, error) {
				home, _ := managedAgyHome()
				if !contains(o.Env, "HOME="+home) {
					t.Error("model listing used user HOME")
				}
				for _, entry := range orchestrate.CrossVendorStrip("antigravity") {
					if !contains(o.Env, entry) {
						t.Error("model listing inherited vendor API key", strings.SplitN(entry, "=", 2)[0])
					}
				}
				if fault == "model-copy" {
					os.Remove(filepath.Join(home, agyTokenPath))
					os.WriteFile(filepath.Join(home, agyTokenPath), []byte("fixture copy"), 0600)
					return nil, fmt.Errorf("model table unavailable after replacing login link")
				}
				if fault == "models" {
					return nil, fmt.Errorf("model table unavailable")
				}
				return []string{cockpit.AgyDefaultModel}, nil
			}
			h.Backend = func() cockpit.Backend {
				s := &fakeSession{events: make(chan cockpit.Event, 8)}
				b := &fakeBackend{sess: s}
				s.script = func(s *fakeSession) {
					home := ""
					for _, e := range b.gotOpts.Env {
						if v, ok := strings.CutPrefix(e, "HOME="); ok {
							home = v
						}
					}
					if b.gotOpts.ReadOnlyPolicy {
						writeAgyHookDeniedDoctorLog(t, Store{Root: filepath.Join(filepath.Dir(b.gotOpts.RepoDir), "home", "jobs")}, "read-only-hook")
					} else if b.gotOpts.Access == cockpit.AccessWrite {
						if err := os.WriteFile(filepath.Join(b.gotOpts.RepoDir, "out.txt"), []byte("ok"), 0600); err != nil {
							t.Error(err)
						}
						_ = os.WriteFile(filepath.Join(b.gotOpts.RepoDir, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
					} else {
						writeAgyDeniedDoctorLog(t, Store{Root: filepath.Join(filepath.Dir(b.gotOpts.RepoDir), "home", "jobs")}, "read-only")
					}
					db := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
					if fault != "missing-db" {
						if err := os.WriteFile(db, []byte("fixture session database"), 0600); err != nil {
							t.Error(err)
						}
					}
					if fault == "linked-db" {
						os.Remove(db)
						os.Symlink(filepath.Join(source, ".gemini", "antigravity-cli", "conversation_summaries.db"), db)
					}
					if fault == "user-index" {
						os.WriteFile(filepath.Join(source, ".gemini", "antigravity-cli", "conversation_summaries.db"), []byte("unexpected"), 0600)
					}
					if fault == "link" {
						os.Remove(filepath.Join(home, agyTokenPath))
						os.Symlink("/missing", filepath.Join(home, agyTokenPath))
					}
					s.info.Model = b.gotOpts.Model
					s.events <- cockpit.Event{Kind: cockpit.EventTool, Tool: &cockpit.ToolEvent{Phase: "start"}}
					s.events <- cockpit.Event{Kind: cockpit.EventUsage, Usage: &cockpit.Usage{InputTokens: 200, OutputTokens: 7}}
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				return b
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }})
			if err != nil || len(reports) != 1 || reports[0].Passed() != (fault == "") {
				t.Fatalf("fault=%s reports=%+v err=%v", fault, reports, err)
			}
			if len(reports[0].Checks) != 8 {
				t.Fatal(reports)
			}
			if fault == "model-copy" {
				home, _ := managedAgyHome()
				if _, e := os.Lstat(filepath.Join(home, agyTokenPath)); !os.IsNotExist(e) {
					t.Error("model probe retained a private credential copy")
				}
			}
			table := readVerifiedVersions(filepath.Join(os.Getenv("PACTIFY_HOME"), "verified-versions.json"))
			if (table["agy"].Version == "1.3.5") != (fault == "") {
				t.Fatal("bad verified-version record", table)
			}
		})
	}
}

func TestAgyResumeRejectsConfigChange(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	req := Request{Harness: "agy", Cwd: t.TempDir(), Access: "write"}
	st, err := store.Create("first", req)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Finish(st.ID, &Result{Status: StatusSucceeded, SessionID: "session-a"}, "", st.CreatedAt); err != nil {
		t.Fatal(err)
	}
	req.WithUserConfig = true
	if _, err = store.CreateRun("second", req, false, true); err == nil {
		t.Fatal("changed config mode accepted for an agy resume")
	}
}

func TestAgyVersionDoesNotInheritVendorKeys(t *testing.T) {
	fakeAgyUserHome(t)
	bin := t.TempDir()
	script := "#!/bin/sh\n"
	for _, entry := range orchestrate.CrossVendorStrip("antigravity") {
		key, _, _ := strings.Cut(entry, "=")
		t.Setenv(key, "fixture-value")
		script += "[ -z \"$" + key + "\" ] || exit 23\n"
	}
	script += "echo 'agy 1.3.5'\n"
	if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h, _ := Lookup("agy")
	version, err := h.Version(context.Background())
	if err != nil || version != "1.3.5" {
		t.Fatalf("version=%q err=%v", version, err)
	}
}

func TestCheapestDoctorModelPrefersLowTier(t *testing.T) {
	if got := cheapestDoctorModel([]string{"gemini-3.8-flash-high", "gemini-3.8-flash-medium", "gemini-3.8-flash-low", "claude-opus-5-5-low"}); got != "gemini-3.8-flash-low" {
		t.Fatalf("got %q", got)
	}
	if got := cheapestDoctorModel([]string{"only-model"}); got != "only-model" {
		t.Fatalf("got %q", got)
	}
}
