package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func fakeKimiUserHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, name := range []string{"config.toml", "device_id"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("fixture\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(home, "credentials"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KIMI_CODE_HOME", home)
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte("#!/bin/sh\nexit 77\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return home
}

func kimiJobFixture(t *testing.T, user bool) (Store, *fakeSession, *fakeBackend, Harness, string, string) {
	t.Helper()
	store, sess, backend := workerFixture(t, "write")
	userHome := fakeKimiUserHome(t)
	home := filepath.Join(os.Getenv("PACTIFY_HOME"), "harness-homes", "kimi")
	st, _ := store.Load("j1")
	st.Request.Harness, st.Request.WithUserConfig = "kimi", user
	st.ConfigMode = "minimal"
	if user {
		st.ConfigMode = "user"
	}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("kimi")
	h.Version, h.Conceal = nil, nil
	h.Backend = func() cockpit.Backend { return backend }
	sess.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	return store, sess, backend, h, userHome, home
}

func writeKimiJobRecord(t *testing.T, home, sid, extra string) {
	t.Helper()
	dir := filepath.Join(home, "sessions", sid)
	if err := os.MkdirAll(filepath.Join(dir, "agents", "main"), 0700); err != nil {
		t.Fatal(err)
	}
	index, _ := json.Marshal(map[string]string{"sessionId": sid, "sessionDir": dir})
	f, err := os.OpenFile(filepath.Join(home, "session_index.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.Write(append(index, '\n'))
	_ = f.Close()
	if err != nil {
		t.Fatal(err)
	}
	wire := "{\"type\":\"llm.request\",\"model\":\"kimi-code/kimi-for-coding\"}\n{\"type\":\"usage.record\",\"usageScope\":\"turn\",\"usage\":{\"inputOther\":17,\"inputCacheRead\":100,\"output\":9}}\n" + extra + "{\"type\":\"prompt.completed\"}\n"
	f, err = os.OpenFile(filepath.Join(dir, "agents", "main", "wire.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(wire)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		t.Fatal(err)
	}
}

func assertKimiLinks(t *testing.T, home, userHome string) {
	t.Helper()
	info, err := os.Lstat(home)
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("managed home must be a 0700 directory: %v %v", info, err)
	}
	for _, name := range []string{"config.toml", "credentials", "device_id"} {
		path := filepath.Join(home, name)
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			t.Fatalf("%s must be a symlink: %v", path, err)
		}
		target, err := os.Readlink(path)
		if err != nil || target != filepath.Join(userHome, name) {
			t.Fatalf("%s target=%q err=%v", path, target, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, "mcp.json")); !os.IsNotExist(err) {
		t.Fatalf("mcp.json present: %v", err)
	}
}

func TestKimiManagedHomeLifecycle(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			store, s, b, h, userHome, home := kimiJobFixture(t, false)
			st, _ := store.Load("j1")
			if resume {
				st.Request.Resume = "previous"
				_ = store.Save(st)
			}
			concealed := false
			h.Conceal = func(ConcealContext) error { concealed = true; return nil }
			before, _ := os.Lstat(filepath.Join(userHome, "config.toml"))
			s.script = func(s *fakeSession) {
				if !contains(b.gotOpts.Env, "KIMI_CODE_HOME="+home) {
					t.Error("child did not receive managed KIMI_CODE_HOME")
				}
				assertKimiLinks(t, home, userHome)
				writeKimiJobRecord(t, home, s.ThreadID(), "")
				// A conflicting record in user home must never supply model/token evidence.
				writeKimiJobRecord(t, userHome, s.ThreadID(), `{"type":"llm.request","model":"wrong"}`+"\n")
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil || r.Status != StatusSucceeded || !r.ModelVerified || r.ModelActual != "kimi-code/kimi-for-coding" || r.InputTokens != 117 || r.OutputTokens != 9 || concealed {
				t.Fatalf("result=%+v concealed=%v err=%v", r, concealed, err)
			}
			after, _ := os.Lstat(filepath.Join(userHome, "config.toml"))
			if !after.ModTime().Equal(before.ModTime()) {
				t.Fatal("user config modified")
			}
			assertKimiLinks(t, home, userHome)
			saved, err := store.Load("j1")
			if err != nil || saved.HarnessHome != home {
				t.Fatalf("actual home not persisted: %+v %v", saved, err)
			}
			if resume && b.resumed != "previous" {
				t.Fatal("resume lost")
			}
		})
	}
}

func TestKimiManagedHomeMissingSource(t *testing.T) {
	for _, name := range []string{"config.toml", "credentials", "device_id"} {
		t.Run(name, func(t *testing.T) {
			store, _, b, h, userHome, _ := kimiJobFixture(t, false)
			if err := os.RemoveAll(filepath.Join(userHome, name)); err != nil {
				t.Fatal(err)
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err == nil || !strings.Contains(err.Error(), "kimi login") || r.Status != StatusFailed || b.gotOpts.RepoDir != "" {
				t.Fatalf("missing source: result=%+v err=%v", r, err)
			}
		})
	}
}

func TestKimiManagedHomeRepairsLinksAndRemovesCopies(t *testing.T) {
	for _, outcome := range []string{"succeeded", "failed", "startup-failed", "canceled", "panic"} {
		for _, directory := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/dir=%v", outcome, directory), func(t *testing.T) {
				store, s, b, h, userHome, home := kimiJobFixture(t, false)
				if err := os.MkdirAll(home, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("/wrong-target", filepath.Join(home, "config.toml")); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(filepath.Join(home, "credentials"), 0700); err != nil {
					t.Fatal(err)
				}
				_ = os.WriteFile(filepath.Join(home, "device_id"), []byte("copy"), 0600)
				_ = os.WriteFile(filepath.Join(home, "mcp.json"), []byte("{}"), 0600)
				replace := func() {
					assertKimiLinks(t, home, userHome)
					writeKimiJobRecord(t, home, s.ThreadID(), "")
					for _, name := range []string{"config.toml", "credentials", "device_id"} {
						path := filepath.Join(home, name)
						if err := os.Remove(path); err != nil {
							t.Fatal(err)
						}
						if directory {
							if err := os.Mkdir(path, 0700); err != nil {
								t.Fatal(err)
							}
							_ = os.WriteFile(filepath.Join(path, "copy"), []byte("fixture"), 0600)
						} else {
							_ = os.WriteFile(path, []byte("fixture"), 0600)
						}
					}
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				h.Conceal = func(ConcealContext) error { t.Error("minimal session archived"); return nil }
				h.Backend = func() cockpit.Backend {
					if outcome == "startup-failed" || outcome == "panic" {
						replace()
					}
					if outcome == "panic" {
						panic("fixture")
					}
					return b
				}
				if outcome == "startup-failed" {
					b.startErr = fmt.Errorf("fixture start failure")
				}
				s.script = func(s *fakeSession) {
					replace()
					if outcome == "canceled" {
						cancel()
						return
					}
					state := cockpit.StateTurnCompleted
					if outcome == "failed" {
						state = cockpit.StateTurnFailed
					}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: state}
				}
				func() {
					defer func() {
						if p := recover(); p != nil && outcome != "panic" {
							panic(p)
						}
					}()
					_, _ = RunJob(ctx, store, "j1", h, WorkerConfig{})
				}()
				data, err := os.ReadFile(store.ResultPath("j1"))
				if err != nil {
					t.Fatal(err)
				}
				var r Result
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"config.toml", "credentials", "device_id"} {
					if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
						t.Errorf("copy remains at %s: %v", name, err)
					}
					if !contains(r.Warnings, name) {
						t.Errorf("missing replacement warning for %s: %v", name, r.Warnings)
					}
					if _, err := os.Lstat(filepath.Join(userHome, name)); err != nil {
						t.Errorf("source removed: %v", err)
					}
				}
			})
		}
	}
}

func TestKimiDefaultsToMinimalConfig(t *testing.T) {
	h, _ := Lookup("kimi")
	if !h.MinimalConfig || h.configMode(false) != "minimal" || h.configMode(true) != "user" {
		t.Fatal("kimi must default to minimal config")
	}
}

func TestKimiWithUserConfigKeepsHomeAndConceal(t *testing.T) {
	store, s, b, h, userHome, home := kimiJobFixture(t, true)
	calls := 0
	h.Conceal = func(ConcealContext) error { calls++; return nil }
	s.script = func(s *fakeSession) {
		if !contains(b.gotOpts.Env, "KIMI_CODE_HOME="+userHome) {
			t.Error("user home not explicit in child environment")
		}
		writeKimiJobRecord(t, userHome, s.ThreadID(), "")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || !r.ModelVerified || calls != 1 || r.ConfigMode != "user" {
		t.Fatalf("result=%+v calls=%d err=%v", r, calls, err)
	}
	if _, err := os.Lstat(home); !os.IsNotExist(err) {
		t.Fatalf("user-config job touched managed home: %v", err)
	}
}

func TestKimiResumePreservesActualHome(t *testing.T) {
	for _, isolated := range []bool{false, true} {
		for _, user := range []bool{false, true} {
			t.Run(fmt.Sprintf("isolated=%v/user=%v", isolated, user), func(t *testing.T) {
				t.Setenv("PACTIFY_HOME", t.TempDir())
				fakeKimiUserHome(t)
				store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
				req := Request{Harness: "kimi", Cwd: gitRepo(t), Access: "write", WithUserConfig: user}
				st, err := store.CreateRun("first", req, isolated, false)
				if err != nil {
					t.Fatal(err)
				}
				if isolated {
					t.Cleanup(func() { _, _ = runGit(req.Cwd, "worktree", "remove", "--force", st.Worktree.Path) })
				}
				// A completed job's recorded home must survive changes in the launch environment.
				st.HarnessHome = filepath.Join(t.TempDir(), "previous-home")
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				if err := store.Finish("first", &Result{Status: StatusSucceeded, SessionID: "previous-session"}, "", st.CreatedAt); err != nil {
					t.Fatal(err)
				}
				next, err := store.CreateRun("next", req, false, true)
				if err != nil {
					t.Fatal(err)
				}
				if next.HarnessHome != st.HarnessHome || next.Request.Resume != "previous-session" {
					t.Fatalf("resume lost actual home: %+v", next)
				}
			})
		}
	}
}

func TestKimiResumeRejectsChangingConfigMode(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprint(legacy), func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			store := Store{Root: filepath.Join(t.TempDir(), "jobs")}
			req := Request{Harness: "kimi", Cwd: "/repo", Access: "write", WithUserConfig: legacy}
			st, err := store.Create("first", req)
			if err != nil {
				t.Fatal(err)
			}
			if legacy {
				st.ConfigMode = ""
				_ = store.Save(st)
			}
			if err := store.Finish("first", &Result{Status: StatusSucceeded, SessionID: "previous-session"}, "", st.CreatedAt); err != nil {
				t.Fatal(err)
			}
			req.WithUserConfig = !legacy
			_, err = store.CreateRun("next", req, false, true)
			if err == nil || !strings.Contains(err.Error(), "config") {
				t.Fatalf("changed kimi home accepted: %v", err)
			}
			if _, err := store.Load("next"); !os.IsNotExist(err) {
				t.Fatalf("invalid resume created job: %v", err)
			}
		})
	}
}

func TestKimiDoctorChecksManagedHomeEvidence(t *testing.T) {
	for _, fault := range []string{"", "mcp", "user-index", "missing-wire", "broken-link", "wrong-home", "failed-job"} {
		t.Run(fault, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			userHome := fakeKimiUserHome(t)
			h, _ := Lookup("kimi")
			h.Version = func(context.Context) (string, error) { return "test", nil }
			h.Conceal = func(ConcealContext) error { t.Error("doctor must not archive minimal sessions"); return nil }
			h.Backend = func() cockpit.Backend {
				s := &fakeSession{events: make(chan cockpit.Event, 8)}
				b := &fakeBackend{sess: s}
				s.script = func(s *fakeSession) {
					home := ""
					for _, env := range b.gotOpts.Env {
						if value, ok := strings.CutPrefix(env, "KIMI_CODE_HOME="); ok {
							home = value
						}
					}
					if home == "" {
						t.Error("probe home missing")
						home = t.TempDir()
					}
					extra := ""
					if fault == "mcp" {
						extra = `{"type":"mcp.tools_discovered","tools":[]}` + "\n"
					}
					if fault == "wrong-home" {
						home = t.TempDir()
					}
					writeKimiJobRecord(t, home, s.ThreadID(), extra)
					if fault == "user-index" {
						writeKimiJobRecord(t, userHome, s.ThreadID(), "")
					}
					if fault == "missing-wire" {
						_ = os.Remove(filepath.Join(home, "sessions", s.ThreadID(), "agents", "main", "wire.jsonl"))
					}
					if fault == "broken-link" {
						_ = os.Remove(filepath.Join(home, "config.toml"))
						_ = os.Symlink("/wrong", filepath.Join(home, "config.toml"))
					}
					_ = os.WriteFile(filepath.Join(b.gotOpts.RepoDir, "out.txt"), []byte("doctor-ok\n"), 0600)
					s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
					state := cockpit.StateTurnCompleted
					if fault == "failed-job" {
						state = cockpit.StateTurnFailed
					}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: state}
				}
				return b
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }})
			if err != nil || len(reports) != 1 {
				t.Fatalf("reports=%+v err=%v", reports, err)
			}
			found := false
			for _, check := range reports[0].Checks {
				if check.Name == "Managed home" {
					found = true
					if (check.Status == "PASS") != (fault == "") {
						t.Fatalf("fault=%s check=%+v", fault, check)
					}
				}
			}
			if !found {
				t.Fatalf("doctor has no managed home check: %+v", reports[0])
			}
			if reports[0].Passed() != (fault == "") {
				t.Fatalf("fault=%s reports=%+v", fault, reports)
			}
		})
	}
}

func TestKimiSetupFailureRemovesExistingCopies(t *testing.T) {
	for _, missing := range []string{"device_id", "user-home"} {
		t.Run(missing, func(t *testing.T) {
			store, _, _, h, userHome, home := kimiJobFixture(t, false)
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(home, "config.toml"), []byte("fixture copy"), 0600); err != nil {
				t.Fatal(err)
			}
			absent := filepath.Join(userHome, missing)
			if missing == "user-home" {
				absent = userHome
			}
			if err := os.RemoveAll(absent); err != nil {
				t.Fatal(err)
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err == nil || !contains(r.Warnings, "config.toml") {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			if _, err := os.Lstat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
				t.Fatalf("setup failure retained config copy: %v", err)
			}
		})
	}
}

func TestKimiManagedHomeRejectsUnsafeLocations(t *testing.T) {
	for _, kind := range []string{"same-home", "nested-source", "symlink-home"} {
		t.Run(kind, func(t *testing.T) {
			store, _, b, h, userHome, home := kimiJobFixture(t, false)
			if err := os.MkdirAll(filepath.Dir(home), 0700); err != nil {
				t.Fatal(err)
			}
			source := home
			if kind == "nested-source" {
				source = filepath.Join(home, "credentials")
				if err := os.Mkdir(home, 0700); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "symlink-home" {
				source = userHome
				if err := os.Symlink(source, home); err != nil {
					t.Fatal(err)
				}
			} else if err := os.Rename(userHome, source); err != nil {
				t.Fatal(err)
			}
			t.Setenv("KIMI_CODE_HOME", source)
			_, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err == nil || b.gotOpts.RepoDir != "" {
				t.Fatalf("unsafe source accepted: %v", err)
			}
			for _, name := range []string{"config.toml", "credentials", "device_id"} {
				if _, err := os.Lstat(filepath.Join(source, name)); err != nil {
					t.Errorf("source damaged: %v", err)
				}
			}
		})
	}
}

func TestKimiListUsesJobHome(t *testing.T) {
	inherited := fakeKimiUserHome(t)
	actual := t.TempDir()
	bin, record := t.TempDir(), filepath.Join(t.TempDir(), "home")
	script := "#!/bin/sh\nprintf '%s' \"$KIMI_CODE_HOME\" > \"$PACTIFY_TEST_KIMI_HOME_RECORD\"\nprintf '%s\\n' '[{\"id\":\"probe\"}]'\n"
	if err := os.WriteFile(filepath.Join(bin, "kimi"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("PACTIFY_TEST_KIMI_HOME_RECORD", record)
	if err := kimiListHidesInHome(actual, "/repo", "probe"); err == nil {
		t.Fatal("listed probe was treated as hidden")
	}
	data, err := os.ReadFile(record)
	if err != nil || string(data) != actual || os.Getenv("KIMI_CODE_HOME") != inherited {
		t.Fatalf("list home=%q err=%v", data, err)
	}
}

func TestKimiCleanupNeverFollowsReplacedHome(t *testing.T) {
	store, s, _, h, userHome, home := kimiJobFixture(t, false)
	s.script = func(s *fakeSession) {
		if err := os.Rename(home, home+"-original"); err != nil {
			t.Error(err)
		}
		if err := os.Symlink(userHome, home); err != nil {
			t.Error(err)
		}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	if !contains(r.Warnings, "home") {
		t.Errorf("missing unsafe home warning: %v", r.Warnings)
	}
	for _, name := range []string{"config.toml", "credentials", "device_id"} {
		if _, err := os.Lstat(filepath.Join(userHome, name)); err != nil {
			t.Errorf("cleanup followed replaced home and removed source %s: %v", name, err)
		}
	}
}

func TestKimiManagedHomeSharedAcrossJobs(t *testing.T) {
	store, s, b, h, userHome, home := kimiJobFixture(t, false)
	calls := 0
	s.script = func(s *fakeSession) {
		calls++
		assertKimiLinks(t, home, userHome)
		writeKimiJobRecord(t, home, s.ThreadID(), "")
		if calls == 1 {
			path := filepath.Join(home, "config.toml")
			if err := os.Remove(path); err != nil {
				t.Error(err)
			}
			if err := os.WriteFile(path, []byte("fixture copy"), 0600); err != nil {
				t.Error(err)
			}
		}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	if r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{}); err != nil || r.Status != StatusSucceeded {
		t.Fatalf("first job=%+v err=%v", r, err)
	}
	if _, err := os.Lstat(filepath.Join(home, "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("first job kept copy: %v", err)
	}
	prior, _ := store.Load("j1")
	if _, err := store.CreateRun("second", prior.Request, false, true); err != nil {
		t.Fatal(err)
	}
	r, err := RunJob(context.Background(), store, "second", h, WorkerConfig{})
	if err != nil || r.Status != StatusSucceeded || b.resumed != "th-fake" || !contains(b.gotOpts.Env, "KIMI_CODE_HOME="+home) {
		t.Fatalf("second job=%+v resumed=%q err=%v", r, b.resumed, err)
	}
	assertKimiLinks(t, home, userHome)
	if listed, err := cockpit.KimiSessionListed(userHome, "th-fake"); err != nil || listed {
		t.Fatalf("delegation entered user list: %v %v", listed, err)
	}
}

func TestKimiManagedHomeUsesDefaultUserHome(t *testing.T) {
	store, s, b, h, userHome, _ := kimiJobFixture(t, false)
	fakeHome := t.TempDir()
	if err := os.Symlink(userHome, filepath.Join(fakeHome, ".kimi-code")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", fakeHome)
	t.Setenv("PACTIFY_HOME", "")
	t.Setenv("KIMI_CODE_HOME", "")
	expected := filepath.Join(fakeHome, ".pactify", "harness-homes", "kimi")
	s.script = func(s *fakeSession) {
		assertKimiLinks(t, expected, filepath.Join(fakeHome, ".kimi-code"))
		if !contains(b.gotOpts.Env, "KIMI_CODE_HOME="+expected) {
			t.Error("wrong default managed home")
		}
		writeKimiJobRecord(t, expected, s.ThreadID(), "")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || !r.ModelVerified || r.Status != StatusSucceeded {
		t.Fatalf("result=%+v err=%v", r, err)
	}
}
