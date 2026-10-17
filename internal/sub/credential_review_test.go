package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestKimiCreatesMissingOAuthDirectory(t *testing.T) {
	source := fakeKimiUserHome(t)
	if err := os.Remove(filepath.Join(source, "oauth")); err != nil {
		t.Fatal(err)
	}
	m := kimiHome(filepath.Join(t.TempDir(), "managed"), source)
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(source, "oauth"))
	if err != nil || !info.IsDir() || info.Mode().Perm() != 0700 {
		t.Fatalf("shared oauth directory must be created as 0700: %v %v", info, err)
	}
	target, err := os.Readlink(filepath.Join(m.Home, "oauth"))
	if err != nil || target != filepath.Join(source, "oauth") {
		t.Fatal("refresh lock directory is not shared", err)
	}
}

func TestKimiPrivateConfigRewriteDoesNotReachUser(t *testing.T) {
	for _, cleanup := range []bool{true, false} {
		t.Run(map[bool]string{true: "cleanup", false: "next_prepare"}[cleanup], func(t *testing.T) {
			source := fakeKimiUserHome(t)
			m := kimiHome(filepath.Join(t.TempDir(), "managed"), source)
			if err := m.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"config.toml", "device_id"} {
				path := filepath.Join(m.Home, name)
				if err := os.WriteFile(path+".tmp", []byte("synthetic private rewrite"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(path+".tmp", path); err != nil {
					t.Fatal(err)
				}
			}
			if cleanup {
				m.Cleanup()
			} else if err := m.Prepare(context.Background()); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"config.toml", "device_id"} {
				data, err := os.ReadFile(filepath.Join(source, name)) // Synthetic configuration only.
				if err != nil || string(data) != "fixture\n" {
					t.Errorf("user %s was overwritten", name)
				}
			}
		})
	}
}

func TestDoctorReportsCredentialCleanupWarnings(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	source := t.TempDir()
	managed := filepath.Join(home, "managed")
	if err := os.Mkdir(managed, 0700); err != nil {
		t.Fatal(err)
	}
	// A damaged metadata sidecar fails before prepare installs cleanup defers.
	if err := os.WriteFile(managed+".sources.json", []byte("invalid metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	h := Harness{Name: "fake", Version: func(context.Context) (string, error) { return "test", nil }, Models: func(context.Context, cockpit.StartOpts) ([]string, error) {
		t.Error("discovery must not run")
		return nil, nil
	}, Home: &harnessHome{Env: "HOME", Source: func() (string, error) { return source, nil }, Managed: func() (string, error) { return managed, nil }, Layout: func(home, source string) *managedHome { return &managedHome{Name: "fake", Home: home, Source: source} }}}
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "fake", nil }})
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%v err=%v", reports, err)
	}
	var reasons []string
	for _, check := range reports[0].Checks {
		reasons = append(reasons, check.Reason)
	}
	if !strings.Contains(strings.Join(reasons, ";"), "private refreshes retained") {
		t.Fatalf("cleanup warning hidden: %v", reasons)
	}
}

func TestHarnessCredentialLinkDeclarations(t *testing.T) {
	for _, tc := range []struct {
		layout     *managedHome
		name       string
		credential bool
	}{
		{kimiHome("/managed", "/source"), "config.toml", false},
		{kimiHome("/managed", "/source"), "device_id", false},
		{kimiHome("/managed", "/source"), "oauth", false},
		{kimiHome("/managed", "/source"), "credentials", true},
		{opencodeData("/managed", "/source"), "opencode/auth.json", true},
		{agyHome("/managed", "/source"), agyTokenPath, true},
	} {
		if tc.layout.Links[tc.name].Credential != tc.credential {
			t.Errorf("%s %s credential=%t", tc.layout.Name, tc.name, tc.layout.Links[tc.name].Credential)
		}
	}
	h, _ := Lookup("codex")
	if h.Home != nil {
		t.Fatal("Codex home declaration changed; audit its credential links")
	}
}

func TestManagedCredentialRetentionExplainsRecovery(t *testing.T) {
	source, home := t.TempDir(), filepath.Join(t.TempDir(), "managed")
	target := filepath.Join(source, "auth.json")
	if err := os.WriteFile(target, []byte("fake original"), 0600); err != nil {
		t.Fatal(err)
	}
	unsafe := filepath.Join(t.TempDir(), "quarantine-link")
	if err := os.Symlink(t.TempDir(), unsafe); err != nil {
		t.Fatal(err)
	}
	m := &managedHome{Name: "fake", Source: source, Home: home, QuarantineDir: unsafe, Links: map[string]homeLink{"auth.json": {Target: target, Credential: true}}}
	if err := m.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	private := filepath.Join(home, "auth.json")
	for _, path := range []string{private, target} {
		if err := os.WriteFile(path+".tmp", []byte("fake new"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Fatal(err)
		}
	}
	err := m.Prepare(context.Background())
	if err == nil {
		t.Fatal("unsafe preservation succeeded")
	}
	for _, phrase := range []string{private, "stop jobs", "same-volume rename", "do not delete"} {
		if !strings.Contains(err.Error(), phrase) {
			t.Errorf("recovery diagnostic missing %q: %v", phrase, err)
		}
	}
	if _, err := os.Lstat(private); err != nil {
		t.Fatal("failed preservation discarded the refresh", err)
	}
}

func TestKimiConfigRewriteWarningDoesNotBecomeJobError(t *testing.T) {
	store, session, _, h, _, home := kimiJobFixture(t, false)
	session.script = func(s *fakeSession) {
		writeKimiJobRecord(t, home, s.ThreadID(), "")
		path := filepath.Join(home, "config.toml")
		if err := os.WriteFile(path+".tmp", []byte("synthetic normalization"), 0600); err != nil {
			t.Error(err)
			return
		}
		if err := os.Rename(path+".tmp", path); err != nil {
			t.Error(err)
			return
		}
		s.events <- cockpit.Event{Kind: cockpit.EventMessage, Text: "done", Final: true}
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Load("j1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != StatusSucceeded || st.Error != "" {
		t.Fatalf("configuration normalization became a job error: status=%s error=%q", r.Status, st.Error)
	}
}
