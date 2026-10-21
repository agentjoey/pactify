package cockpit

import (
	"context"
	"encoding/json"
	"github.com/agentjoey/pactify/internal/testenv"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestStorageReviewOpencodeInheritedDatabase(t *testing.T) {
	t.Setenv("OPENCODE_DB", "/outside/user.db")
	t.Setenv("OPENCODE_CONFIG_DIR", "/outside/config")
	env, err := OpencodeEnv(StartOpts{MinimalConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range OpencodeCommandEnv(env, true) {
		if strings.Contains(e, "/outside/") {
			t.Fatalf("inherited unsafe override: %s", e)
		}
	}
}
func TestStorageReviewOpencodeMissingKeepAndDelete404(t *testing.T) {
	for _, present := range []bool{true, false} {
		t.Run(map[bool]string{true: "still present", false: "gone"}[present], func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == "DELETE" {
					w.WriteHeader(404)
					return
				}
				if present {
					w.Write([]byte(`[{"id":"orphan"}]`))
				} else {
					w.Write([]byte(`[]`))
				}
			}))
			defer server.Close()
			s := &opencodeServer{url: server.URL, http: server.Client()}
			if err := s.deleteStorageSession(context.Background(), "orphan"); (err != nil) != present {
				t.Fatalf("present=%v err=%v", present, err)
			}
			if !present {
				if _, err := s.pruneStorageSessions(context.Background(), nil, map[string]bool{"missing": true}); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestStorageReviewOpencodeRequiresPrivateDatabase(t *testing.T) {
	for _, data := range []string{"", "/outside", t.TempDir()} {
		expected := t.TempDir()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"data":"` + data + `"}`)) }))
		s := &opencodeServer{url: server.URL, http: server.Client()}
		if err := s.verifyStorageDatabase(context.Background(), storageTestOpencodeEnv(expected), expected); err == nil {
			t.Fatal("unverified database accepted")
		}
		server.Close()
	}
}

func TestStorageReviewExactCodexThreadIDs(t *testing.T) {
	server := newFakeCodexServer(t)
	defer server.Close()
	ids := []string{}
	server.handle("thread/delete", func(m wireMessage) any {
		var p struct {
			ThreadID string `json:"threadId"`
		}
		json.Unmarshal(m.Params, &p)
		ids = append(ids, p.ThreadID)
		return map[string]any{}
	})
	if e := deleteCodexThreads(context.Background(), server.client, []string{"original", "resumed", "replacement"}); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(ids, []string{"original", "resumed", "replacement"}) {
		t.Fatalf("deleted threads: %v", ids)
	}
}

func TestStorageReviewOpencodePrivateDatabaseConfirmation(t *testing.T) {
	data := t.TempDir()
	os.Mkdir(filepath.Join(data, "opencode"), 0700)
	os.WriteFile(filepath.Join(data, "opencode/opencode.db"), nil, 0600)
	for _, v2 := range []bool{false, true} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p := "/path"
			if v2 {
				p = "/api/path"
			}
			if r.URL.Path != p {
				t.Errorf("path %s", r.URL.Path)
			}
			os.MkdirAll(filepath.Join(filepath.Dir(data), "state/opencode"), 0700)
			os.MkdirAll(filepath.Join(filepath.Dir(data), "config/opencode"), 0700)
			paths := map[string]string{"state": filepath.Join(filepath.Dir(data), "state/opencode"), "config": filepath.Join(filepath.Dir(data), "config/opencode"), "home": filepath.Dir(data), "worktree": data, "directory": data}
			if v2 {
				paths["data"] = filepath.Join(data, "opencode")
				json.NewEncoder(w).Encode(map[string]any{"data": paths})
			} else {
				json.NewEncoder(w).Encode(paths)
			}
		}))
		s := &opencodeServer{url: server.URL, http: server.Client(), v2: v2}
		if e := s.verifyStorageDatabase(context.Background(), storageTestOpencodeEnv(data), data); e != nil {
			t.Fatal(e)
		}
		server.Close()
	}
}
func TestStorageReviewKimiDeleteRetriesBindingWindow(t *testing.T) {
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	address := listener.Addr().String()
	listener.Close()
	ready := make(chan *http.Server, 1)
	go func() {
		time.Sleep(100 * time.Millisecond)
		l, e := net.Listen("tcp", address)
		if e != nil {
			ready <- nil
			return
		}
		s := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })}
		ready <- s
		s.Serve(l)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := deleteKimiSessionHTTP(ctx, "http://"+address, "fake-token", "session")
	server := <-ready
	if server == nil {
		t.Fatal("test server failed to bind")
	}
	defer server.Close()
	if err != nil {
		t.Fatal(err)
	}
}

func TestStorageReviewOrphanSweepRefusesUnconfirmedServer(t *testing.T) {
	t.Setenv("COCKPIT_OC_SCENARIO", "storage-unconfirmed")
	data := testenv.InstallOpencodeServe(t)
	requests := filepath.Join(t.TempDir(), "requests")
	t.Setenv("COCKPIT_OC_REQUESTS", requests)
	t.Setenv("OPENCODE_DB", "/outside/user.db")
	t.Setenv("OPENCODE_CONFIG_DIR", "/outside/config")
	binary, e := exec.LookPath("opencode")
	if e != nil {
		t.Fatal(e)
	}
	wrapper, e := os.ReadFile(binary)
	if e != nil {
		t.Fatal(e)
	}
	wrapper = []byte(strings.Replace(string(wrapper), "#!/bin/sh\n", "#!/bin/sh\n[ \"$OPENCODE_DB\" = opencode.db ] || exit 66\n[ -z \"$OPENCODE_CONFIG_DIR\" ] || exit 67\n", 1))
	if e = os.WriteFile(binary, wrapper, 0700); e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, e = PruneOpencodeOrphans(ctx, StartOpts{RepoDir: t.TempDir(), MinimalConfig: true, Env: []string{"XDG_DATA_HOME=" + data}}, false, nil)
	if e == nil || !strings.Contains(e.Error(), "cannot confirm private opencode database") {
		t.Fatalf("unconfirmed sweep: %v", e)
	}
	rows, e := os.ReadFile(requests)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(string(rows), "/path") || strings.Contains(string(rows), "/session") {
		t.Fatalf("session API used before database confirmation: %s", rows)
	}
}
