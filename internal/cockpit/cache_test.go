package cockpit

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestCodexCacheRootsStartAndResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		for _, access := range []Access{AccessWrite, AccessReadOnly, AccessDefault} {
			t.Run(string(access)+map[bool]string{false: "/start", true: "/resume"}[resume], func(t *testing.T) {
				s := newFakeCodexServer(t)
				defer s.Close()
				var got map[string]any
				s.handle("config/read", func(m wireMessage) any { return map[string]any{"config": map[string]any{}} })
				method := "thread/start"
				if resume {
					method = "thread/resume"
				}
				s.handle(method, func(m wireMessage) any {
					_ = json.Unmarshal(m.Params, &got)
					return map[string]any{"thread": map[string]any{"id": "cache-thread"}}
				})
				opts := StartOpts{RepoDir: "/repo", Access: access, WritableRoots: []string{"/cache/go-build", "/cache/go-mod", "/cache/npm", "/job/tmp"}}
				var err error
				if resume {
					_, _, err = s.client.threadResumeOpts(context.Background(), "old", opts)
				} else {
					_, _, err = s.client.threadStartOpts(context.Background(), opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				if access != AccessWrite {
					if _, ok := got["config"]; ok {
						t.Fatalf("cache roots widened %q: %v", access, got)
					}
					return
				}
				config := got["config"].(map[string]any)["sandbox_workspace_write"].(map[string]any)
				want := []any{"/cache/go-build", "/cache/go-mod", "/cache/npm", "/job/tmp"}
				if !reflect.DeepEqual(config["writable_roots"], want) || config["network_access"] != true {
					t.Fatalf("sandbox config=%v", config)
				}
			})
		}
	}
}

func TestCodexCacheRootsPreserveConfiguredRoots(t *testing.T) {
	s := newFakeCodexServer(t)
	defer s.Close()
	s.handle("config/read", func(m wireMessage) any {
		return map[string]any{"config": map[string]any{"sandbox_workspace_write": map[string]any{"writable_roots": []string{"/user/cache", "/cache/go-build"}}}}
	})
	var got map[string]any
	s.handle("thread/start", func(m wireMessage) any {
		_ = json.Unmarshal(m.Params, &got)
		return map[string]any{"thread": map[string]any{"id": "cache-thread"}}
	})
	_, _, err := s.client.threadStartOpts(context.Background(), StartOpts{RepoDir: "/repo", Access: AccessWrite, WritableRoots: []string{"/cache/go-build", "/cache/npm"}})
	if err != nil {
		t.Fatal(err)
	}
	roots := got["config"].(map[string]any)["sandbox_workspace_write"].(map[string]any)["writable_roots"]
	if !reflect.DeepEqual(roots, []any{"/user/cache", "/cache/go-build", "/cache/npm"}) {
		t.Fatalf("lost configured roots: %v", roots)
	}
}
