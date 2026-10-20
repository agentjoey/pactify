package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/agentjoey/pactify/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDoctorOpencodeWritePermissionEvidence(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		t.Run(map[bool]string{true: "safe", false: "unsafe"}[allowed], func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			h, _ := Lookup("opencode")
			h.Version = func(context.Context) (string, error) { return "1.19.0", nil }
			checks := 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
				model: "opencode/test-free", lookPath: func(string) (string, error) { return "synthetic", nil },
				run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
					st, err := store.Load(id)
					if err != nil {
						t.Fatal(err)
					}
					if id == "write" {
						if err := os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600); err != nil {
							t.Fatal(err)
						}
					}
					return Result{JobID: id, Status: StatusSucceeded, SessionID: "own-" + id, CLIVersion: "1.19.0", InputTokens: 1, FinalText: "done", ModelActual: "opencode/test-free", ModelVerified: true}, nil
				},
				readonlyCheck: func(context.Context, Store, string, Result) error { return nil },
				writeCheck: func(_ context.Context, _ Store, id string, r Result) error {
					checks++
					if id != "write" || r.SessionID != "own-write" {
						t.Fatal("wrong session")
					}
					if !allowed {
						return errors.New("unsafe external_directory policy")
					}
					return nil
				},
			})
			if err != nil || len(reports) != 1 || checks != 1 {
				t.Fatalf("reports=%+v checks=%d err=%v", reports, checks, err)
			}
			for _, c := range reports[0].Checks {
				if c.Name == "Write access" {
					want := "PASS"
					if !allowed {
						want = "FAIL"
					}
					if c.Status != want || !strings.Contains(c.Reason, "external_directory") {
						t.Fatalf("check=%+v want=%s", c, want)
					}
				}
			}
		})
	}
}

func TestOpencodeDeniedDirectoryResultWarning(t *testing.T) {
	for _, major := range []int{1, 2} {
		t.Run(fmt.Sprint(major), func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			if major == 1 {
				testenv.InstallOpencodeServe(t)
				t.Setenv("COCKPIT_OC_SCENARIO", "permission-policy")
			} else {
				testenv.InstallOpencode2(t)
				t.Setenv("COCKPIT_OC2_SCENARIO", "permission-policy")
			}
			outside := t.TempDir()
			payload := map[string]any{"id": "per-1", "sessionID": "sid", "permission": "external_directory", "patterns": []string{outside + "/*"}}
			if major == 2 {
				payload = map[string]any{"id": "per_1", "sessionID": "ses_grandchild", "action": "external_directory", "resources": []string{outside + "/*"}}
			}
			raw, _ := json.Marshal(payload)
			t.Setenv("COCKPIT_OC_PERMISSION", string(raw))
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			if _, err := store.Create("denied", Request{Harness: "opencode", Access: "write", Cwd: repo, Brief: "test", Model: "opencode/test-free"}); err != nil {
				t.Fatal(err)
			}
			h, _ := Lookup("opencode")
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			r, err := RunJob(ctx, store, "denied", h, WorkerConfig{})
			if err != nil || r.Status != StatusSucceeded {
				t.Fatalf("result=%+v err=%v", r, err)
			}
			b, err := os.ReadFile(store.ResultPath("denied"))
			if err != nil {
				t.Fatal(err)
			}
			var saved Result
			if err := json.Unmarshal(b, &saved); err != nil {
				t.Fatal(err)
			}
			if !contains(saved.Warnings, "pactify denied external directory access: "+outside) {
				t.Fatalf("warnings=%v", saved.Warnings)
			}
		})
	}
}
