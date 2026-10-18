package e2e

import (
	"context"
	"github.com/agentjoey/pactify/internal/sub"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestOpencode2FixtureSelectionAndEnvironment(t *testing.T) {
	for _, version := range []string{"1.19.0", "v2.0.32", "3.0.0"} {
		t.Run(version, func(t *testing.T) {
			h, _ := sub.Lookup("opencode")
			calls := 0
			h.Version = func(context.Context) (string, error) { calls++; return version, nil }
			selected, gotVersion, err := fixtureHarness(context.Background(), h)
			if version == "3.0.0" {
				if err == nil {
					t.Fatal("unsupported version accepted")
				}
				return
			}
			if err != nil || gotVersion != version || calls != 1 {
				t.Fatalf("version=%s calls=%d err=%v", gotVersion, calls, err)
			}
			wantV2 := version == "v2.0.32"
			if selected.Opencode2 != wantV2 || reflect.TypeOf(selected.Backend()) != reflect.TypeOf(h.ForVersion(version).Backend()) {
				t.Fatal("fixture diverged from product selection")
			}
			root := t.TempDir()
			inherited := []string{"PATH=/bin", "HOME=/real-home", "XDG_DATA_HOME=/real-share", "OPENCODE_CONFIG=/host.json", "OPENCODE_CONFIG_CONTENT=host-config", "OPENCODE_DB=/host.db", "OPENAI_API_KEY=synthetic-secret", "PACTIFY_HOME=/host-pactify"}
			env, err := isolatedFixtureEnv(root, inherited)
			if err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_CONTENT", "OPENCODE_DB", "OPENAI_API_KEY"} {
				if value := envValue(env, key); value != "" {
					t.Fatal("inherited forbidden variable", key)
				}
			}
			for _, key := range []string{"HOME", "PACTIFY_HOME", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
				if !strings.HasPrefix(envValue(env, key), root+string(filepath.Separator)) {
					t.Fatal("unisolated environment", key)
				}
			}
			discovery, err := opencodeFixtureEnv(selected, env)
			if err != nil {
				t.Fatal(err)
			}
			if wantV2 {
				for key, want := range sub.Opencode2ManagedHomes(filepath.Join(root, "pactify")) {
					if got := envValue(discovery, key); got != want {
						t.Fatalf("%s=%s want=%s", key, got, want)
					}
				}
				if envValue(discovery, "OPENCODE_CONFIG_DIR") != filepath.Join(envValue(discovery, "XDG_CONFIG_HOME"), "opencode") || envValue(discovery, "OPENCODE_DISABLE_PROJECT_CONFIG") != "1" || envValue(discovery, "OPENCODE_DISABLE_CLAUDE_CODE") != "1" {
					t.Fatal("discovery config isolation missing")
				}
			} else if !reflect.DeepEqual(discovery, env) {
				t.Fatal("v1 fixture environment changed")
			}
			if err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.Type()&os.ModeSymlink != 0 || d.Name() == "auth.json" || d.Name() == "opencode.db" {
					t.Errorf("fixture construction touched credentials: %s", path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestOpencode2UserMetadataRootsAndDatabase(t *testing.T) {
	env := []string{"XDG_DATA_HOME=/user/share", "XDG_CONFIG_HOME=/user/config", "XDG_STATE_HOME=/user/state", "XDG_CACHE_HOME=/user/cache"}
	want := []string{"/user/share/opencode", "/user/config/opencode", "/user/share/opencode/opencode.db"}
	if got := opencodeUserRoots("/user/home", env); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	fallback := opencodeUserRoots("/user/home", nil)
	if fallback[0] != "/user/home/.local/share/opencode" || fallback[1] != "/user/home/.config/opencode" {
		t.Fatal(fallback)
	}
	db := "/user/share/opencode/opencode.db"
	before := map[string]meta{db: {Mode: 0600, Size: 1}}
	after := map[string]meta{db: {Mode: 0600, Size: 2}}
	changed, refresh := compareMetadata(before, after, "opencode2")
	if len(changed) != 1 || len(refresh) != 0 {
		t.Fatal("user DB changes were exempted", changed, refresh)
	}
}

func TestOpencode2ShellSpawnEvidence(t *testing.T) {
	rows := [][]byte{[]byte(`{"kind":"system","state":"shell.created","raw":{"info":{"command":"sleep 600","metadata":{"sessionID":"ses_probe"}}}}`)}
	if !toolRunningEvidence(rows, true) || toolRunningEvidence(rows, false) {
		t.Fatal("shell spawn must count only for v2")
	}
	if toolRunningEvidence([][]byte{[]byte(`{"kind":"message","text":"shell.created"}`)}, true) {
		t.Fatal("verbal spawn claim accepted")
	}
}
