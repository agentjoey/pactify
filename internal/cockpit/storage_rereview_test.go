package cockpit

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/testenv"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestStorageRealOpencode1193Path(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"share/opencode", "state/opencode", "config/opencode"} {
		os.MkdirAll(filepath.Join(root, p), 0700)
	}
	os.WriteFile(filepath.Join(root, "share/opencode/opencode.db"), nil, 0600)
	for _, v2 := range []bool{false, true} {
		for _, bad := range []bool{false, true} {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				state := filepath.Join(root, "state/opencode")
				if bad {
					state = t.TempDir()
				}
				paths := map[string]string{"home": root, "state": state, "config": filepath.Join(root, "config/opencode"), "worktree": root, "directory": root}
				if v2 {
					json.NewEncoder(w).Encode(map[string]any{"data": paths})
				} else {
					json.NewEncoder(w).Encode(paths)
				}
			}))
			s := &opencodeServer{url: server.URL, http: server.Client(), v2: v2}
			err := s.verifyStorageDatabase(context.Background(), storageTestOpencodeEnv(filepath.Join(root, "share")), filepath.Join(root, "share"))
			server.Close()
			if (err != nil) != bad {
				t.Fatalf("v2=%v bad=%v err=%v", v2, bad, err)
			}
		}
	}
}
func TestStorageOpencodeUserEnvironmentPreserved(t *testing.T) {
	t.Setenv("OPENCODE_DB", "custom.db")
	t.Setenv("OPENCODE_CONFIG_DIR", "/user/config")
	env := OpencodeCommandEnv(nil)
	values := map[string]string{}
	for _, e := range env {
		for i := range e {
			if e[i] == '=' {
				values[e[:i]] = e[i+1:]
				break
			}
		}
	}
	if values["OPENCODE_DB"] != "custom.db" || values["OPENCODE_CONFIG_DIR"] != "/user/config" {
		t.Fatal("user configuration stripped")
	}
}

func storageTestOpencodeEnv(data string) []string {
	return []string{"XDG_DATA_HOME=" + data, "XDG_STATE_HOME=" + filepath.Join(filepath.Dir(data), "state"), "XDG_CONFIG_HOME=" + filepath.Join(filepath.Dir(data), "config"), "OPENCODE_DB=opencode.db", "OPENCODE_DISABLE_CHANNEL_DB=1"}
}

func TestStorageAlreadyDeletedSessionNotCounted(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			w.WriteHeader(404)
		} else {
			w.Write([]byte(`[]`))
		}
	}))
	defer server.Close()
	s := &opencodeServer{url: server.URL, http: server.Client()}
	n, e := s.pruneStorageSessions(context.Background(), []string{"gone"}, nil)
	if e != nil || n != 0 {
		t.Fatalf("already-gone session counted: %d %v", n, e)
	}
}

func TestStorageOpencodeServerPreservesUserSelection(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		t.Run(fmt.Sprint(v2), func(t *testing.T) {
			if v2 {
				testenv.InstallOpencode2(t)
			} else {
				testenv.InstallOpencodeServe(t)
			}
			t.Setenv("OPENCODE_DB", "custom.db")
			t.Setenv("OPENCODE_CONFIG_DIR", "custom-config")
			binary, e := exec.LookPath("opencode")
			if e != nil {
				t.Fatal(e)
			}
			raw, e := os.ReadFile(binary)
			if e != nil {
				t.Fatal(e)
			}
			raw = []byte(strings.Replace(string(raw), "#!/bin/sh\n", "#!/bin/sh\n[ \"$OPENCODE_DB\" = custom.db ] || exit 61\n[ \"$OPENCODE_CONFIG_DIR\" = custom-config ] || exit 62\n", 1))
			os.WriteFile(binary, raw, 0700)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			opts := StartOpts{RepoDir: t.TempDir(), MinimalConfig: false}
			var server *opencodeServer
			if v2 {
				server, e = startOpencode2Server(ctx, opts)
			} else {
				server, e = startOpencodeServer(ctx, opts)
			}
			if e != nil {
				t.Fatal(e)
			}
			server.close()
		})
	}
}

func TestStorageDatabaseRequiresPinnedActualEnvironment(t *testing.T) {
	root := t.TempDir()
	for _, p := range []string{"share/opencode", "state/opencode", "config/opencode"} {
		os.MkdirAll(filepath.Join(root, p), 0700)
	}
	os.WriteFile(filepath.Join(root, "share/opencode/opencode.db"), nil, 0600)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"state": filepath.Join(root, "state/opencode"), "config": filepath.Join(root, "config/opencode")})
	}))
	defer server.Close()
	s := &opencodeServer{url: server.URL, http: server.Client()}
	for _, override := range []string{"OPENCODE_DB=/outside/user.db", "OPENCODE_DB=:memory:", "OPENCODE_DISABLE_CHANNEL_DB=0"} {
		env := append(storageTestOpencodeEnv(filepath.Join(root, "share")), override)
		if e := s.verifyStorageDatabase(context.Background(), env, filepath.Join(root, "share")); e == nil {
			t.Fatalf("unverified database selection accepted: %s", override)
		}
	}
}

func TestStorageRepeatedOpencodeSessionDeletedOnce(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; w.Write([]byte(`true`)) }))
	defer server.Close()
	s := &opencodeServer{url: server.URL, http: server.Client()}
	n, e := s.pruneStorageSessions(context.Background(), []string{"same", "same"}, nil)
	if e != nil || n != 1 || calls != 1 {
		t.Fatalf("duplicate deletion: count=%d calls=%d err=%v", n, calls, e)
	}
}
