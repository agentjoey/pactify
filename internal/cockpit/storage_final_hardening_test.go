package cockpit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStorageDatabaseMatchesExpectedPrivateRoot(t *testing.T) {
	for _, v2 := range []bool{false, true} {
		for _, selection := range []string{"expected", "alias", "outside", "reported-outside"} {
			t.Run(strings.Join([]string{map[bool]string{false: "v1", true: "v2"}[v2], selection}, "/"), func(t *testing.T) {
				root, outside := t.TempDir(), t.TempDir()
				data := filepath.Join(root, "share")
				for _, base := range []string{root, outside} {
					for _, sub := range []string{"share/opencode", "state/opencode", "config/opencode"} {
						if err := os.MkdirAll(filepath.Join(base, sub), 0700); err != nil {
							t.Fatal(err)
						}
					}
					if err := os.WriteFile(filepath.Join(base, "share/opencode/opencode.db"), nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				actual := data
				if selection == "outside" {
					actual = filepath.Join(outside, "share")
				} else if selection == "alias" {
					actual = filepath.Join(t.TempDir(), "alias")
					if err := os.Symlink(data, actual); err != nil {
						t.Fatal(err)
					}
				}
				reportedRoot := root
				if selection == "outside" {
					reportedRoot = outside
				}
				env := storageTestOpencodeEnv(actual)
				env = append(env, "XDG_STATE_HOME="+filepath.Join(reportedRoot, "state"), "XDG_CONFIG_HOME="+filepath.Join(reportedRoot, "config"))
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					paths := map[string]string{"home": root, "state": filepath.Join(reportedRoot, "state/opencode"), "config": filepath.Join(reportedRoot, "config/opencode"), "worktree": root, "directory": root}
					if v2 {
						paths["data"] = filepath.Join(actual, "opencode")
						if selection == "reported-outside" {
							paths["data"] = filepath.Join(outside, "share/opencode")
						}
						json.NewEncoder(w).Encode(map[string]any{"data": paths})
					} else {
						json.NewEncoder(w).Encode(paths)
					}
				}))
				defer server.Close()
				s := &opencodeServer{url: server.URL, http: server.Client(), v2: v2}
				err := s.verifyStorageDatabase(context.Background(), env, data)
				wantError := selection == "outside" || v2 && selection == "reported-outside"
				if (err != nil) != wantError {
					t.Fatalf("expected root %q, actual %q: %v (want error %v)", data, actual, err, wantError)
				}
				if selection == "outside" && err != nil && (!strings.Contains(err.Error(), actual) || !strings.Contains(err.Error(), data)) {
					t.Fatalf("root mismatch must report both paths: %v", err)
				}
			})
		}
	}
}
