package sub

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/pelletier/go-toml/v2"
)

var expectedKimiReadTools = []string{"Read", "Grep", "Glob", "ReadMediaFile", "TodoList", "TaskList", "TaskOutput", "WaitFor"}

const kimiROConfigFixture = `default_model = "subscription"
default_plan_mode = true
default_permission_mode = "yolo"
[models.subscription]
provider = "subscription"
model = "kimi-for-coding"
[providers.subscription]
type = "kimi"
api_key = "fixture-secret"
[tools]
enabled = ["Bash"]
[[permission.rules]]
decision = "allow"
pattern = "*"
[[hooks]]
event = "PreToolUse"
command = "false"
`

func TestKimiReadonlyPrivateConfig(t *testing.T) {
	store, sess, backend, h, source, writeHome := kimiJobFixture(t, false)
	h.Verify = nil
	st, _ := store.Load("j1")
	st.Request.Access = "read-only"
	store.Save(st)
	config := filepath.Join(source, "config.toml")
	if err := os.WriteFile(config, []byte(kimiROConfigFixture), 0600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(filepath.Dir(writeHome), "kimi-readonly")
	// A stale link must be replaced atomically without modifying its target.
	os.MkdirAll(home, 0700)
	os.Symlink(config, filepath.Join(home, "config.toml"))
	sess.script = func(s *fakeSession) {
		if !contains(backend.gotOpts.Env, "KIMI_CODE_HOME="+home) {
			t.Error("readonly home not selected")
		}
		info, err := os.Lstat(filepath.Join(home, "config.toml"))
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			t.Errorf("private config metadata: %v %v", info, err)
		}
		data, err := os.ReadFile(filepath.Join(home, "config.toml"))
		if err != nil {
			t.Error(err)
		}
		var cfg map[string]any
		if err := toml.Unmarshal(data, &cfg); err != nil {
			t.Error("generated config is invalid TOML")
		}
		if cfg["default_permission_mode"] != "manual" || cfg["default_plan_mode"] != nil || cfg["hooks"] != nil {
			t.Error("unsafe user defaults retained")
		}
		if cfg["default_model"] != "subscription" || !reflect.DeepEqual(cfg["models"], map[string]any{"subscription": map[string]any{"provider": "subscription", "model": "kimi-for-coding"}}) {
			t.Error("model settings lost")
		}
		if cfg["providers"].(map[string]any)["subscription"].(map[string]any)["api_key"] != "fixture-secret" {
			t.Error("provider settings lost")
		}
		tools := cfg["tools"].(map[string]any)["enabled"].([]any)
		var names []string
		for _, v := range tools {
			names = append(names, v.(string))
		}
		if !reflect.DeepEqual(names, expectedKimiReadTools) {
			t.Error("incorrect tool allowlist")
		}
		rules := cfg["permission"].(map[string]any)["rules"].([]any)
		var denied []string
		for _, r := range rules {
			rule := r.(map[string]any)
			if rule["decision"] != "deny" {
				t.Error("user permission retained")
			}
			denied = append(denied, rule["pattern"].(string))
		}
		if !reflect.DeepEqual(denied, []string{"Write", "Edit", "Bash", "Agent*", "Cron*", "TaskStop"}) {
			t.Error("deny rules missing")
		}
		for _, name := range []string{"credentials", "oauth", "device_id"} {
			link, err := os.Readlink(filepath.Join(home, name))
			if err != nil || link != filepath.Join(source, name) {
				t.Errorf("%s link: %v", name, err)
			}
		}
		writeKimiJobRecord(t, home, s.ThreadID(), `{"type":"llm.tools_snapshot","tools":[{"name":"Read","description":"read","parameters":{}}]}`+"\n")
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil || r.Status != StatusSucceeded {
		t.Fatalf("readonly start: status=%s err=%v", r.Status, err)
	}
	data, _ := os.ReadFile(config)
	if string(data) != kimiROConfigFixture {
		t.Error("user config changed")
	}
}

func TestKimiReadonlyHomeRegenerationAndLeases(t *testing.T) {
	source := fakeKimiUserHome(t)
	config := filepath.Join(source, "config.toml")
	if err := os.WriteFile(config, []byte(kimiROConfigFixture), 0600); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "kimi-readonly")
	first := kimiReadOnlyHome(home, source)
	if err := first.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer first.Cleanup()
	before, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	updated := strings.ReplaceAll(kimiROConfigFixture, "subscription", "updated")
	if err := os.WriteFile(config, []byte(updated), 0600); err != nil {
		t.Fatal(err)
	}
	second := kimiReadOnlyHome(home, source)
	if err := second.Prepare(context.Background()); err == nil || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("active home was reconfigured: %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(home, "config.toml"))
	if string(after) != string(before) {
		t.Fatal("config changed under active lease")
	}
	// Write and read-only homes never contend for the same usage or setup lock.
	writer := kimiHome(filepath.Join(filepath.Dir(home), "kimi"), source)
	if err := writer.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer writer.Cleanup()
	if warnings := first.Cleanup(); len(warnings) != 0 {
		t.Fatal(warnings)
	}
	if err := second.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	after, _ = os.ReadFile(filepath.Join(home, "config.toml"))
	var cfg map[string]any
	if toml.Unmarshal(after, &cfg) != nil || cfg["default_model"] != "updated" {
		t.Error("config was not regenerated")
	}
	info, err := os.Lstat(filepath.Join(home, "config.toml"))
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		t.Error("regenerated config is not private")
	}
}

func TestKimiReadonlyInvalidConfigDoesNotLeak(t *testing.T) {
	source := fakeKimiUserHome(t)
	if err := os.WriteFile(filepath.Join(source, "config.toml"), []byte("api_key = fixture-secret-invalid-toml"), 0600); err != nil {
		t.Fatal(err)
	}
	err := kimiReadOnlyHome(filepath.Join(t.TempDir(), "readonly"), source).Prepare(context.Background())
	if err == nil || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("unsafe config diagnostic: %v", err)
	}
}

func TestKimiReadonlyCredentialQuarantine(t *testing.T) {
	source := fakeKimiUserHome(t)
	os.WriteFile(filepath.Join(source, "config.toml"), []byte("default_permission_mode = \"manual\"\n"), 0600)
	m := kimiReadOnlyHome(filepath.Join(t.TempDir(), "readonly"), source)
	m.QuarantineDir = filepath.Join(t.TempDir(), "quarantine")
	if err := m.prepare(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	defer m.Cleanup()
	path := filepath.Join(m.Home, "credentials")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	warnings := m.Cleanup()
	if len(warnings) != 1 || !strings.Contains(warnings[0], "quarantined") {
		t.Fatalf("credential replacement not quarantined: %v", warnings)
	}
	if info, err := os.Stat(filepath.Join(source, "credentials")); err != nil || !info.IsDir() {
		t.Fatal("source directory changed")
	}
}

func TestKimiReadonlyBaselineAndToolEvidence(t *testing.T) {
	for _, scenario := range []string{"safe", "missing", "unsafe-main", "unsafe-child", "malformed", "ignored-modify", "lost-baseline", "escaped-child", "missing-child-wire"} {
		t.Run(scenario, func(t *testing.T) {
			store, _, _, _, _, _ := kimiJobFixture(t, false)
			home, err := managedKimiReadOnlyHome()
			if err != nil {
				t.Fatal(err)
			}
			st, _ := store.Load("j1")
			st.Request.Access = "read-only"
			st.HarnessHome = home
			write(t, st.Request.Cwd, ".gitignore", "victim\n")
			write(t, st.Request.Cwd, "victim", "before\n")
			st.ReadOnlyBaseline, _ = captureReadOnly(st.Request.Cwd)
			st.StartedAt = st.CreatedAt
			if scenario == "lost-baseline" {
				st.ReadOnlyBaseline = nil
			}
			store.Save(st)
			extra := `{"type":"llm.tools_snapshot","tools":[{"name":"Read","description":"read","parameters":{}}]}` + "\n"
			if scenario == "missing" {
				extra = ""
			}
			if scenario == "unsafe-main" {
				extra = strings.ReplaceAll(extra, "Read", "Bash")
			}
			if scenario == "malformed" {
				extra = `{"type":"llm.tools_snapshot","tools":null}` + "\n"
			}
			writeKimiJobRecord(t, home, "session", extra)
			if scenario == "unsafe-child" {
				dir := filepath.Join(home, "sessions/session/agents/child")
				os.MkdirAll(dir, 0700)
				os.WriteFile(filepath.Join(dir, "wire.jsonl"), []byte(strings.ReplaceAll(extra, "Read", "WebSearch")), 0600)
			}
			if scenario == "escaped-child" {
				outside := t.TempDir()
				os.WriteFile(filepath.Join(outside, "wire.jsonl"), []byte(strings.ReplaceAll(extra, "Read", "Bash")), 0600)
				os.Symlink(outside, filepath.Join(home, "sessions/session/agents/child"))
			}
			if scenario == "missing-child-wire" {
				os.MkdirAll(filepath.Join(home, "sessions/session/agents/child"), 0700)
			}
			if scenario == "ignored-modify" {
				write(t, st.Request.Cwd, "victim", "modified\n")
			}
			r := Result{Status: StatusSucceeded, SessionID: "session"}
			message, violation := store.verifyReadOnly(&st, &r)
			if scenario == "safe" || scenario == "missing-child-wire" {
				if message != "" {
					t.Error(message)
				}
				return
			}
			if message == "" {
				t.Fatal("unverified kimi read-only accepted")
			}
			if (scenario == "unsafe-main" || scenario == "unsafe-child" || scenario == "ignored-modify") && !violation {
				t.Error("violation not classified")
			}
		})
	}
}

func TestKimiReadonlyJobFailsClosed(t *testing.T) {
	for _, scenario := range []string{"safe", "unsafe", "missing", "modified"} {
		t.Run(scenario, func(t *testing.T) {
			store, sess, _, h, source, writeHome := kimiJobFixture(t, false)
			h.Verify = nil
			os.WriteFile(filepath.Join(source, "config.toml"), []byte("default_permission_mode = \"manual\"\n"), 0600)
			st, _ := store.Load("j1")
			st.Request.Access = "read-only"
			store.Save(st)
			home := filepath.Join(filepath.Dir(writeHome), "kimi-readonly")
			sess.script = func(s *fakeSession) {
				extra := `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}` + "\n"
				if scenario == "unsafe" {
					extra = strings.ReplaceAll(extra, "Read", "Bash")
				}
				if scenario == "missing" {
					extra = ""
				}
				writeKimiJobRecord(t, home, s.ThreadID(), extra)
				if scenario == "modified" {
					os.WriteFile(filepath.Join(st.Request.Cwd, "a.txt"), []byte("changed\n"), 0600)
				}
				s.events <- cockpit.Event{Kind: cockpit.EventMessage, Final: true, Text: "done"}
				s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
			}
			r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
			if err != nil {
				t.Fatal(err)
			}
			if scenario == "safe" {
				if r.Status != StatusSucceeded {
					t.Fatalf("safe status=%s warnings=%v", r.Status, r.Warnings)
				}
				return
			}
			if r.Status != StatusFailed {
				t.Fatalf("unverified job succeeded: %s", scenario)
			}
			if scenario != "missing" && r.FailureClass != "readonly_violation" {
				t.Fatalf("class=%s", r.FailureClass)
			}
			if scenario == "missing" && !strings.Contains(strings.Join(r.Warnings, " "), "Read-only verification unavailable") {
				t.Fatalf("missing verification diagnostic: %v", r.Warnings)
			}
		})
	}
}

func TestKimiReadonlyUserConfigRejected(t *testing.T) {
	s := Store{Root: filepath.Join(t.TempDir(), "jobs")}
	_, err := s.Create("bad", Request{Harness: "kimi", Access: "read-only", WithUserConfig: true})
	if err == nil || !strings.Contains(err.Error(), "--with-user-config") || !strings.Contains(err.Error(), "--read-only") {
		t.Fatalf("incompatible config accepted: %v", err)
	}
}

func TestKimiReadonlyRejectsLegacyUserHomeBeforeStart(t *testing.T) {
	store, _, backend, h, _, _ := kimiJobFixture(t, false)
	h.Verify = nil
	st, _ := store.Load("j1")
	st.Request.Access, st.ConfigMode = "read-only", "user"
	store.Save(st)
	_, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err == nil || backend.gotOpts.RepoDir != "" {
		t.Fatal("read-only started using a user-config home")
	}
}

func TestDoctorKimiReadonlySnapshot(t *testing.T) {
	for _, scenario := range []string{"safe", "unsafe", "missing", "write", "failed"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			h, _ := Lookup("kimi")
			h.Home = nil
			h.Models = nil
			h.Version = func(context.Context) (string, error) { return "test", nil }
			probes := 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
				lookPath: func(string) (string, error) { return "fake", nil },
				run: func(_ context.Context, s Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
					st, _ := s.Load(id)
					r := Result{Status: StatusSucceeded, SessionID: "probe", CLIVersion: "test", ModelActual: "native/default", ModelVerified: true, InputTokens: 1, FinalText: "done"}
					if id == "write" {
						os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
						os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
						return r, nil
					}
					probes++
					if strings.Contains(st.Request.Brief, "echo x >") {
						t.Error("diagnostic requests unavailable shell tool")
					}
					home, homeErr := managedKimiReadOnlyHome()
					if homeErr != nil {
						t.Fatal(homeErr)
					}
					st.HarnessHome = home
					s.Save(st)
					extra := `{"type":"llm.tools_snapshot","tools":[{"name":"Read"}]}` + "\n"
					if scenario == "unsafe" {
						extra = strings.ReplaceAll(extra, "Read", "Write")
					}
					if scenario == "missing" {
						extra = ""
					}
					writeKimiJobRecord(t, st.HarnessHome, r.SessionID, extra)
					if scenario == "write" {
						os.WriteFile(filepath.Join(st.Request.Cwd, "probe.txt"), []byte("x"), 0600)
					}
					if scenario == "failed" {
						r.Status = StatusFailed
					}
					return r, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			want := "FAIL"
			if scenario == "safe" {
				want = "PASS"
			}
			for _, check := range reports[0].Checks {
				if check.Name == "Read-only access" && check.Status != want {
					t.Errorf("check=%+v want=%s", check, want)
				}
			}
			if probes != 1 {
				t.Errorf("readonly probes=%d", probes)
			}
		})
	}
}

func TestKimiResumePreservesAccess(t *testing.T) {
	for _, original := range []string{"read-only", "write"} {
		for _, next := range []string{"read-only", "write"} {
			t.Run(original+"/"+next, func(t *testing.T) {
				store, _, _, _, _, _ := kimiJobFixture(t, false)
				previous, _ := store.Load("j1")
				previous.Request.Access = original
				previous.Status = StatusSucceeded
				store.Save(previous)
				req := previous.Request
				req.Access = next
				_, err := store.resumeRun("next", req, false, previous, "session")
				if original != next && (err == nil || !strings.Contains(err.Error(), "original access mode")) {
					t.Fatalf("mode switch accepted: %v", err)
				}
				if original == next && err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
