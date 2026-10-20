package cockpit

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/testenv"
)

func TestOpencodeWritePaths(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "wt")
	outside := filepath.Join(base, "outside")
	for _, p := range []string{root, outside} {
		if err := os.MkdirAll(p, 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(outside, "missing"), filepath.Join(root, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	roots := OpencodeWritableRoots(root, alias, root+"*", root+"?", root+"[x]", root+"{x}", "relative", "")
	for _, p := range roots {
		if strings.ContainsAny(p, "*?[]{}") || !filepath.IsAbs(p) {
			t.Fatalf("unsafe root %q", p)
		}
	}
	wantRoots := []string{root}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolvedRoot != root {
		wantRoots = append(wantRoots, resolvedRoot)
	}
	wantRoots = append(wantRoots, alias)
	if !reflect.DeepEqual(roots, wantRoots) {
		t.Fatalf("roots=%v", roots)
	}
	for _, tc := range []struct {
		name, path string
		roots      []string
		allow      bool
	}{
		{"inside", root + "/new/file", roots, true},
		{"exact", root, roots, true},
		{"sibling", root + "2/file", roots, false},
		{"dotdot", root + "/../outside/file", roots, false},
		{"clean", root + "/new/../file", roots, true},
		{"symlink escape", root + "/escape/new/file", roots, false},
		{"broken symlink", root + "/broken/file", roots, false},
		{"unsafe root", root + "/file", []string{root + "*"}, false},
		{"alias", alias + "/new/file", []string{root}, true},
		{"literal asterisk", root + "/new/*/file", roots, true},
		{"literal question mark", root + "/new/?/file", roots, true},
		{"literal brackets", root + "/app/blog/[slug]/page.tsx", roots, true},
		{"literal braces", root + "/new/{id}/file", roots, true},
		{"literal backslash", root + `/new/\/file`, roots, true},
		{"outside literal brackets", outside + "/[id].tsx", roots, false},
		{"empty list", root, nil, false},
		{"relative", "new/file", roots, false},
		{"empty", "", roots, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := opencodePathAllowed(tc.path, tc.roots); got != tc.allow {
				t.Fatalf("allowed=%t want %t", got, tc.allow)
			}
		})
	}
	if runtime.GOOS == "darwin" {
		aliases := OpencodeWritableRoots("/var")
		if !slices.Contains(aliases, "/var") || !slices.Contains(aliases, "/private/var") {
			t.Fatalf("missing macOS aliases: %v", aliases)
		}
		if !opencodePathAllowed("/private/var/pactify-nonexistent/file", []string{"/var"}) || !opencodePathAllowed("/var/pactify-nonexistent/file", []string{"/private/var"}) {
			t.Fatal("macOS /var aliases rejected")
		}
	}
	// Check any symlinks in the test temporary directory as well.
	if real, err := filepath.EvalSymlinks(base); err == nil && real != base {
		if !opencodePathAllowed(filepath.Join(real, "wt/new"), roots) {
			t.Fatal("resolved temp alias rejected")
		}
	}
}

func TestOpencodeWritePermissionAdapters(t *testing.T) {
	for _, major := range []int{1, 2} {
		for _, tc := range []struct {
			name           string
			paths          []string
			filepath       string
			allow, warning bool
			readonly       bool
			recovered      bool
			v1only, v2only bool
		}{
			{name: "external_directory", paths: []string{"INSIDE/**"}, allow: true},
			{name: "external_directory", paths: []string{"INSIDE/*", "OUTSIDE/*"}, warning: true},
			{name: "external_directory", paths: []string{"INSIDE*"}, warning: true},
			{name: "external_directory", paths: []string{"INSIDE/escape/*"}, warning: true},
			{name: "external_directory", paths: []string{"OUTSIDE/*"}, warning: true},
			{name: "external_directory", warning: true},
			{name: "external_directory", paths: []string{"relative/*"}, warning: true},
			{name: "doom_loop"}, {name: "question"}, {name: "surprise", warning: true},
			{name: "external_directory", paths: []string{"INSIDE/*"}, readonly: true},
			{name: "edit", readonly: true}, {name: "bash", readonly: true}, {name: "surprise", readonly: true},
			{name: "edit", warning: true}, {name: "write", warning: true}, {name: "patch", warning: true},
			{name: "edit", paths: []string{"INSIDE/file"}, allow: true},
			{name: "patch", paths: []string{"INSIDE/file"}, allow: true},
			{name: "edit", paths: []string{"INSIDE/escape/file"}, warning: true},
			{name: "read", paths: []string{"INSIDE/escape/.env"}, warning: true},
			{name: "write", paths: []string{"OUTSIDE/file"}, warning: true},
			{name: "patch", paths: []string{"INSIDE/file", "OUTSIDE/file"}, warning: true},
			{name: "edit", paths: []string{"file"}, allow: true},
			{name: "edit", paths: []string{"app/blog/[slug]/page.tsx"}, allow: true},
			{name: "write", paths: []string{"[id].tsx"}, allow: true},
			{name: "patch", paths: []string{"INSIDE/[id].tsx"}, allow: true},
			{name: "read", paths: []string{"[id].tsx"}, allow: true},
			{name: "edit", paths: []string{"OUTSIDE/[id].tsx"}, warning: true},
			{name: "edit", paths: []string{"INSIDE/escape/[id].tsx"}, warning: true},
			{name: "edit", paths: []string{"src/**"}, allow: true, v2only: true},
			{name: "write", paths: []string{"src/*"}, allow: true, v2only: true},
			{name: "edit", paths: []string{"OUTSIDE/**"}, warning: true, v2only: true},
			{name: "edit", paths: []string{"../../main-repo/wt/file"}, filepath: "INSIDE/file", allow: true, v1only: true},
			{name: "edit", paths: []string{"../../main-repo/wt/file"}, filepath: "OUTSIDE/file", warning: true, v1only: true},
			{name: "edit", paths: []string{"../../main-repo/wt/[id].tsx"}, filepath: "INSIDE/[id].tsx", allow: true, v1only: true},
			{name: "edit", paths: []string{"../outside/file"}, warning: true},
			{name: "read", paths: []string{"INSIDE/.env"}, allow: true},
			{name: "read", paths: []string{"OUTSIDE/.env"}, warning: true},
			{name: "read", paths: []string{".env"}, allow: true},
			{name: "read", warning: true},
			{name: "edit", paths: []string{"INSIDE/file"}, recovered: true, allow: true},
			{name: "edit", paths: []string{"OUTSIDE/file"}, recovered: true, warning: true},
			{name: "external_directory", paths: []string{"INSIDE/*"}, recovered: true, allow: true},
			{name: "edit", filepath: "OUTSIDE/file", warning: true},
			{name: "edit", paths: []string{"INSIDE/file"}, filepath: "OUTSIDE/file", warning: true},
			{name: "write", filepath: "INSIDE/file", allow: true},
			{name: "bash", allow: true}, {name: "shell", allow: true},
			{name: "webfetch", allow: true}, {name: "websearch", allow: true}, {name: "task", allow: true}, {name: "todowrite", allow: true},
		} {
			t.Run(strings.Join([]string{string(rune('0' + major)), tc.name, strings.Join(tc.paths, ","), tc.filepath}, "/"), func(t *testing.T) {
				if tc.v1only && major != 1 || tc.v2only && major != 2 {
					t.Skip("version-specific permission shape")
				}
				if tc.recovered && major == 2 {
					t.Skip("v1 recovery regression; v2 already preserves raw requests")
				}
				var data string
				if major == 1 {
					data = testenv.InstallOpencodeServe(t)
					t.Setenv("COCKPIT_OC_SCENARIO", "permission-policy")
					if tc.recovered {
						t.Setenv("COCKPIT_OC_SCENARIO", "recovered-permission-policy")
					}
				} else {
					testenv.InstallOpencode2(t)
					t.Setenv("COCKPIT_OC2_SCENARIO", "permission-policy")
				}
				root, outside := t.TempDir(), t.TempDir()
				if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
					t.Fatal(err)
				}
				replace := strings.NewReplacer("INSIDE", root, "OUTSIDE", outside)
				paths := []string{}
				for _, p := range tc.paths {
					paths = append(paths, replace.Replace(p))
				}
				payload := map[string]any{"id": "per-1", "sessionID": "sid", "permission": tc.name, "patterns": paths, "always": paths, "metadata": map[string]string{"filepath": replace.Replace(tc.filepath)}}
				if major == 2 {
					if tc.filepath != "" {
						paths = append(paths, replace.Replace(tc.filepath))
					}
					payload = map[string]any{"id": "per_1", "sessionID": "ses_grandchild", "action": tc.name, "resources": paths}
				}
				raw, _ := json.Marshal(payload)
				t.Setenv("COCKPIT_OC_PERMISSION", string(raw))
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				opts := StartOpts{RepoDir: root, Access: AccessWrite, WritableRoots: []string{root}, Model: "opencode/test-free", MinimalConfig: true}
				if tc.readonly {
					opts.Access = AccessReadOnly
				}
				var backend Backend = NewOpencodeBackend()
				if major == 2 {
					backend = NewOpencode2Backend()
				}
				s, err := backend.Start(ctx, opts)
				if err != nil {
					t.Fatal(err)
				}
				defer s.Close()
				done := make(chan error, 1)
				go func() { done <- s.Prompt(ctx, UserMessage{Text: "test"}) }()
				forwarded, warned := false, false
			loop:
				for {
					select {
					case a := <-s.Approvals():
						forwarded = true
						if tc.recovered && !strings.Contains(string(a.RawInput), paths[0]) {
							t.Errorf("recovered request lost path data: %s", a.RawInput)
						}
						if err := a.Respond(DecisionAllowForSession); err != nil {
							t.Fatal(err)
						}
					case e := <-s.Events():
						if e.State == "warning" {
							warned = true
							if tc.name == "external_directory" && !strings.HasPrefix(e.Text, "pactify denied external directory access: ") {
								t.Fatalf("warning=%q", e.Text)
							}
						}
						if e.State == StateTurnCompleted {
							break loop
						}
						if e.State == StateTurnFailed {
							t.Fatalf("turn failed: %+v", e)
						}
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					}
				}
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				want := "reject"
				if tc.allow {
					want = "once"
				}
				if major == 1 {
					b, err := os.ReadFile(filepath.Join(data, "reply"))
					if err != nil || string(b) != want {
						t.Fatalf("reply=%s want=%s err=%v", b, want, err)
					}
				} else {
					found := false
					for _, r := range oc2Requests(t) {
						if strings.HasSuffix(r.Path, "/reply") {
							found = true
							if r.Body["decision"] != want || !tc.allow && r.Body["message"] == "" {
								t.Fatalf("reply=%v want=%s", r.Body, want)
							}
						}
					}
					if !found {
						t.Fatal("no permission reply")
					}
				}
				if forwarded != tc.allow || warned != tc.warning {
					t.Fatalf("forwarded=%t warning=%t; want %t %t", forwarded, warned, tc.allow, tc.warning)
				}
			})
		}
	}
}

func TestOpencodeWriteSessionRules(t *testing.T) {
	for _, major := range []int{1, 2} {
		t.Run(string(rune('0'+major)), func(t *testing.T) {
			var data string
			if major == 1 {
				data = testenv.InstallOpencodeServe(t)
			} else {
				testenv.InstallOpencode2(t)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			root, tmp := t.TempDir(), t.TempDir()
			opts := StartOpts{RepoDir: root, Access: AccessWrite, WritableRoots: []string{root, tmp, root + "*"}, MinimalConfig: true}
			var b Backend = NewOpencodeBackend()
			if major == 2 {
				b = NewOpencode2Backend()
			}
			id := ""
			for round := 0; round < 2; round++ {
				var s Session
				var err error
				if round == 0 {
					s, err = b.Start(ctx, opts)
				} else {
					s, err = b.(OptsResumer).ResumeWith(ctx, id, opts)
				}
				if err != nil {
					t.Fatal(err)
				}
				id = s.ThreadID()
				s.Close()
				if major == 1 {
					raw, err := os.ReadFile(filepath.Join(data, "permissions.json"))
					if err != nil {
						t.Fatal(err)
					}
					var body struct{ Permission []opencodePermission }
					if err := json.Unmarshal(raw, &body); err != nil {
						t.Fatal(err)
					}
					for _, c := range []struct{ name, path, want string }{{"external_directory", root + "/*", "ask"}, {"external_directory", tmp + "/*", "ask"}, {"external_directory", root + "2/*", "deny"}, {"doom_loop", "x", "deny"}, {"edit", "x", "ask"}, {"bash", "x", "ask"}} {
						if got := opencodeLastAction(body.Permission, c.name, c.path); got != c.want {
							t.Errorf("round %d %s %s=%s want %s", round, c.name, c.path, got, c.want)
						}
					}
					// Replacing the persisted rules makes the resume assertion detect a missing PATCH.
					if round == 0 {
						if err := os.WriteFile(filepath.Join(data, "permissions.json"), []byte(`{"permission":[]}`), 0600); err != nil {
							t.Fatal(err)
						}
					}
				} else {
					found := false
					for _, r := range oc2Requests(t) {
						if r.Method == []string{"POST", "PATCH"}[round] && r.Path == []string{"/api/session", "/api/session/ses_root"}[round] {
							found = true
							raw, _ := json.Marshal(r.Body["permissions"])
							var rules []Opencode2Rule
							json.Unmarshal(raw, &rules)
							converted := []opencodePermission{}
							for _, r := range rules {
								converted = append(converted, opencodePermission{r.Action, r.Resource, r.Effect})
							}
							for _, c := range []struct{ path, want string }{{root + "/*", "ask"}, {tmp + "/*", "ask"}, {root + "2/*", "deny"}} {
								if got := opencodeLastAction(converted, "external_directory", c.path); got != c.want {
									t.Errorf("round %d %s=%s want %s", round, c.path, got, c.want)
								}
							}
						}
					}
					if !found {
						t.Fatal("session rules request absent")
					}
				}
			}
		})
	}
}

func TestOpencodeWriteContinuesOnDeny(t *testing.T) {
	env, err := OpencodeEnv(StartOpts{Access: AccessWrite, Env: []string{`OPENCODE_CONFIG_CONTENT={"experimental":{"existing":true}}`}})
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Experimental map[string]bool }
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
			config.Experimental = nil
			if err := json.Unmarshal([]byte(v), &config); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !config.Experimental["continue_loop_on_deny"] || !config.Experimental["existing"] {
		t.Fatalf("experimental=%v", config.Experimental)
	}
}

func TestCheckOpencodeWriteOwnSession(t *testing.T) {
	for _, major := range []int{1, 2} {
		for _, policy := range []string{"safe", "outside-allow", "inside-deny", "inside-allow", "empty"} {
			t.Run(string(rune('0'+major))+"/"+policy, func(t *testing.T) {
				var data string
				if major == 1 {
					data = testenv.InstallOpencodeServe(t)
				} else {
					testenv.InstallOpencode2(t)
				}
				root := t.TempDir()
				opts := StartOpts{RepoDir: root, Access: AccessWrite, WritableRoots: []string{root}, MinimalConfig: true}
				ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
				defer cancel()
				var b Backend = NewOpencodeBackend()
				if major == 2 {
					b = NewOpencode2Backend()
				}
				s, err := b.Start(ctx, opts)
				if err != nil {
					t.Fatal(err)
				}
				id := s.ThreadID()
				s.Close()
				if major == 1 && policy != "safe" {
					rules := []opencodePermission{}
					switch policy {
					case "outside-allow":
						rules = []opencodePermission{{"external_directory", "*", "allow"}}
					case "inside-allow":
						rules = []opencodePermission{{"external_directory", "*", "deny"}, {"external_directory", root + "/*", "allow"}}
					case "inside-deny":
						rules = []opencodePermission{{"external_directory", "*", "deny"}}
					}
					raw, _ := json.Marshal(map[string]any{"permission": rules})
					if err := os.WriteFile(filepath.Join(data, "permissions.json"), raw, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if major == 2 {
					t.Setenv("COCKPIT_OC2_WRITE_POLICY", policy)
				}
				if major == 1 {
					err = CheckOpencodeWrite(ctx, opts, id)
				} else {
					err = CheckOpencode2Write(ctx, opts, id)
				}
				if (err == nil) != (policy == "safe") {
					t.Fatalf("policy=%s err=%v", policy, err)
				}
				if major == 2 {
					calls := 0
					for _, r := range oc2Requests(t) {
						if r.Method == "POST" && r.Path == "/api/session/"+id+"/permission" {
							calls++
						}
						if r.Method == "PATCH" {
							t.Fatal("check replaced session policy")
						}
					}
					if calls == 0 {
						t.Fatal("no own-session evaluation")
					}
				}
			})
		}
	}
}

func TestOpencodeReadonlyExperimentalConfigUnchanged(t *testing.T) {
	env, err := OpencodeEnv(StartOpts{Access: AccessReadOnly, Env: []string{`OPENCODE_CONFIG_CONTENT={"experimental":{"existing":true}}`}})
	if err != nil {
		t.Fatal(err)
	}
	var config struct{ Experimental map[string]bool }
	for _, entry := range env {
		if raw, ok := strings.CutPrefix(entry, "OPENCODE_CONFIG_CONTENT="); ok {
			config.Experimental = nil
			if err := json.Unmarshal([]byte(raw), &config); err != nil {
				t.Fatal(err)
			}
		}
	}
	if !reflect.DeepEqual(config.Experimental, map[string]bool{"continue_loop_on_deny": true}) {
		t.Fatalf("read-only config changed: %v", config.Experimental)
	}
}

func TestOpencodeWriteLiteralCwdAndWarnings(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wt[1]")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	for _, v2 := range []bool{false, true} {
		for _, tc := range []struct {
			name, path string
			allow      bool
		}{
			{"edit", root + "/file", true}, {"read", ".env", true}, {"external_directory", root + "/*", false},
			{"external_directory", root + "*", false}, {"edit", "", false}, {"edit", "bad*/../file", true}, {"read", "/outside/secret\nFORGED", false},
			{"edit", "/outside/" + strings.Repeat("x", 500), false},
		} {
			t.Run(tc.name+"/"+tc.path+"/"+map[bool]string{true: "2", false: "1"}[v2], func(t *testing.T) {
				key := "patterns"
				if v2 {
					key = "resources"
				}
				raw, _ := json.Marshal(map[string]any{key: []string{tc.path}})
				s := &opencodeSession{opts: StartOpts{Access: AccessWrite, RepoDir: root, WritableRoots: OpencodeWritableRoots(root)}, server: &opencodeServer{}, events: make(chan Event, 4)}
				if got := s.allowWritePermission(tc.name, raw, v2); got != tc.allow {
					t.Fatalf("allowed=%t want=%t", got, tc.allow)
				}
				if !tc.allow {
					e := <-s.events
					if strings.ContainsAny(e.Text, "\r\n") || len([]rune(e.Text)) > 300 {
						t.Fatalf("unsafe warning %q", e.Text)
					}
					if tc.path == "" && !strings.HasSuffix(e.Text, "(empty)") {
						t.Fatalf("missing empty placeholder: %q", e.Text)
					}
					if tc.name == "read" && !strings.HasPrefix(e.Text, "pactify denied read access: ") {
						t.Fatalf("misleading read warning %q", e.Text)
					}
				}
			})
		}
	}
}

func TestOpencodeWriteCwdAliasWithGlobName(t *testing.T) {
	for _, symlinkParent := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain_parent", true: "symlinked_parent"}[symlinkParent], func(t *testing.T) {
			base := t.TempDir()
			if symlinkParent {
				parent := filepath.Join(base, "real")
				if err := os.Mkdir(parent, 0700); err != nil {
					t.Fatal(err)
				}
				link := filepath.Join(base, "link")
				if err := os.Symlink(parent, link); err != nil {
					t.Fatal(err)
				}
				base = link
			}
			real := filepath.Join(base, "wt[1]")
			alias := filepath.Join(base, "alias")
			if err := os.Mkdir(real, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(real, alias); err != nil {
				t.Fatal(err)
			}
			for _, v2 := range []bool{false, true} {
				key := "patterns"
				if v2 {
					key = "resources"
				}
				s := &opencodeSession{opts: StartOpts{Access: AccessWrite, RepoDir: alias, WritableRoots: OpencodeWritableRoots(alias)}, server: &opencodeServer{}, events: make(chan Event, 4)}
				raw, _ := json.Marshal(map[string]any{key: []string{filepath.Join(real, "file")}})
				if !s.allowWritePermission("edit", raw, v2) {
					t.Fatalf("v2=%t: symlink-resolved job cwd containing glob characters was rejected", v2)
				}
			}
		})
	}
}

func TestOpencodeWriteExternalSymlinks(t *testing.T) {
	base := t.TempDir()
	cwd := filepath.Join(base, "wt")
	tmp := filepath.Join(base, "tmp")
	cache := filepath.Join(base, "cache")
	outside := filepath.Join(base, "outside")
	for _, root := range []string{cwd, tmp, cache, outside} {
		if err := os.Mkdir(root, 0700); err != nil {
			t.Fatal(err)
		}
	}
	roots := OpencodeWritableRoots(cwd, tmp, cache)
	for _, root := range []string{tmp, cache} {
		if err := os.Symlink(outside, filepath.Join(root, "e")); err != nil {
			t.Fatal(err)
		}
		for _, v2 := range []bool{false, true} {
			t.Run(filepath.Base(root)+map[bool]string{true: "/2", false: "/1"}[v2], func(t *testing.T) {
				opts := StartOpts{Access: AccessWrite, RepoDir: cwd, WritableRoots: roots}
				pattern := filepath.Join(root, "e") + "/*"
				rules := opencodePermissions(opts)
				key := "patterns"
				if v2 {
					key = "resources"
					rules = nil
					for _, r := range Opencode2Permissions(AccessWrite, roots...) {
						rules = append(rules, opencodePermission{r.Action, r.Resource, r.Effect})
					}
				}
				if action := opencodeLastAction(rules, "external_directory", pattern); action != "ask" {
					t.Fatalf("symlink path bypasses adapter: %s", action)
				}
				s := &opencodeSession{opts: opts, server: &opencodeServer{}, events: make(chan Event, 4)}
				raw, _ := json.Marshal(map[string]any{key: []string{pattern}})
				if s.allowWritePermission("external_directory", raw, v2) {
					t.Fatal("symlink under allowed root escaped")
				}
			})
		}
	}
}
