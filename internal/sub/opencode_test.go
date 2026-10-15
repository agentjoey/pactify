package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

func fakeOpencodeUserHome(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "opencode"), []byte("#!/bin/sh\nexit 99\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	data, config := t.TempDir(), t.TempDir()
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("XDG_CONFIG_HOME", config)
	for _, p := range []string{filepath.Join(data, "opencode"), filepath.Join(data, "other"), filepath.Join(config, "opencode"), filepath.Join(config, "git")} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	// Fixture only: no production code may open this file's contents.
	if err := os.WriteFile(filepath.Join(data, "opencode", "auth.json"), []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	return data, config
}

func TestOpencodeManagedDirectoriesAndPolicy(t *testing.T) {
	for _, user := range []bool{false, true} {
		for _, resume := range []bool{false, true} {
			t.Run(stringBool(user)+"/"+stringBool(resume), func(t *testing.T) {
				data, config := fakeOpencodeUserHome(t)
				store, s, b := workerFixture(t, "write")
				h, ok := Lookup("opencode")
				if !ok {
					t.Fatal("opencode not registered")
				}
				st, _ := store.Load("j1")
				st.Request.Harness = "opencode"
				st.Request.WithUserConfig = user
				if resume {
					st.Request.Resume = "sid"
				}
				st.ConfigMode = h.configMode(user)
				if err := store.Save(st); err != nil {
					t.Fatal(err)
				}
				h.Version = nil
				h.Verify = nil
				h.Backend = func() cockpit.Backend { return b }
				s.script = func(s *fakeSession) {
					expected := map[string]string{"XDG_DATA_HOME": data, "XDG_CONFIG_HOME": config}
					if !user {
						root, _ := managedHarnessHome("opencode")
						expected["XDG_DATA_HOME"] = filepath.Join(root, "share")
						expected["XDG_CONFIG_HOME"] = filepath.Join(root, "config")
						for _, item := range []struct{ path, target string }{{filepath.Join(expected["XDG_DATA_HOME"], "opencode/auth.json"), filepath.Join(data, "opencode/auth.json")}, {filepath.Join(expected["XDG_DATA_HOME"], "other"), filepath.Join(data, "other")}, {filepath.Join(expected["XDG_CONFIG_HOME"], "git"), filepath.Join(config, "git")}} {
							got, err := os.Readlink(item.path)
							if err != nil || got != item.target {
								t.Errorf("link %s: %s %v", item.path, got, err)
							}
						}
						if _, err := os.Lstat(filepath.Join(expected["XDG_CONFIG_HOME"], "opencode")); !os.IsNotExist(err) {
							t.Error("user opencode config mirrored")
						}
					}
					for k, v := range expected {
						if !contains(b.gotOpts.Env, k+"="+v) {
							t.Errorf("missing %s", k)
						}
					}
					for _, entry := range orchestrate.CrossVendorStrip("opencode") {
						if !contains(b.gotOpts.Env, entry) {
							t.Error("missing vendor strip")
						}
					}
					s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
				}
				r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
				if err != nil || r.Status != StatusSucceeded {
					t.Fatalf("result=%+v err=%v", r, err)
				}
				if user && !contains(r.Warnings, "user's session list") {
					t.Fatal("missing visibility warning")
				}
				if _, err = store.CreateRun("next", st.Request, false, true); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}
func stringBool(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func TestOpencodeMissingConfigDirectory(t *testing.T) {
	fakeOpencodeUserHome(t)
	config := filepath.Join(t.TempDir(), "not-created")
	t.Setenv("XDG_CONFIG_HOME", config)
	h, _ := Lookup("opencode")
	_, _, _, layouts, err := prepareHarnessHomes(context.Background(), h.Home, "", nil, true)
	defer func() {
		for _, m := range layouts {
			m.Cleanup()
		}
	}()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Lstat(config); !os.IsNotExist(err) {
		t.Fatal("created user config directory")
	}
}

func TestOpencodeXDGDefaultsAndUnreadableLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()
	data, err := opencodeDataSource()
	if err != nil || data != filepath.Join(home, ".local/share") {
		t.Fatal(data, err)
	}
	config, err := opencodeConfigSource()
	if err != nil || config != filepath.Join(home, ".config") {
		t.Fatal(config, err)
	}
	os.MkdirAll(filepath.Join(data, "opencode"), 0700)
	auth := filepath.Join(data, "opencode/auth.json")
	if err = os.WriteFile(auth, nil, 0000); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(auth)
	if err != nil {
		t.Fatal(err)
	}
	h, _ := Lookup("opencode")
	_, _, _, layouts, err := prepareHarnessHomes(context.Background(), h.Home, "", nil, true)
	defer func() {
		for _, m := range layouts {
			m.Cleanup()
		}
	}()
	if err != nil {
		t.Fatal("login link must use metadata only", err)
	}
	after, err := os.Lstat(auth)
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("login source modified")
	}
}
