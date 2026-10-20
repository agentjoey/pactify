package sub

import (
	"context"
	"github.com/agentjoey/pactify/internal/testenv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpencode2DoctorReadonlyPermissionEvidence(t *testing.T) {
	for _, scenario := range []string{"deny", "allow-shell", "allow-edit", "allow-external_directory", "target-exists", "job-failed"} {
		t.Run(scenario, func(t *testing.T) {
			testenv.InstallOpencode2(t)
			t.Setenv("PACTIFY_HOME", t.TempDir())
			if action, ok := strings.CutPrefix(scenario, "allow-"); ok {
				t.Setenv("COCKPIT_OC2_ALLOW_ACTION", action)
			}
			h, _ := Lookup("opencode")
			readonlyRuns := 0
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
				writeCheck: func(context.Context, Store, string, Result) error { return nil },
				model:      "opencode/test-free", lookPath: func(string) (string, error) { return "synthetic", nil },
				run: func(ctx context.Context, store Store, id string, h Harness, _ WorkerConfig) (Result, error) {
					st, err := store.Load(id)
					if err != nil {
						t.Fatal(err)
					}
					st.HarnessHome, st.HarnessHomes, _, _, err = prepareHarnessHomes(ctx, h.Home, "", nil, true, "")
					if err != nil {
						t.Fatal(err)
					}
					if err := store.Save(st); err != nil {
						t.Fatal(err)
					}
					result := Result{JobID: id, Status: StatusSucceeded, CLIVersion: "v2.0.32", ModelActual: st.Request.Model, ModelVerified: true, InputTokens: 1, FinalText: "done", SessionID: "ses_write"}
					if id == "write" {
						os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600)
						os.WriteFile(filepath.Join(st.Request.Cwd, "doctor-network.json"), []byte(`{"bind":true,"external":true}`), 0600)
					} else {
						readonlyRuns++
						result.SessionID = "ses_root"
						// No write tool is exposed: successful jobs with zero calls must pass
						// only when all three API evaluations deny on this exact probe session.
						if scenario == "target-exists" {
							os.WriteFile(filepath.Join(st.Request.Cwd, "probe.txt"), []byte("escaped"), 0600)
						}
						if scenario == "job-failed" {
							result.Status = StatusFailed
						}
					}
					return result, nil
				},
			})
			if err != nil || len(reports) != 1 {
				t.Fatalf("reports=%+v err=%v", reports, err)
			}
			found := false
			for _, check := range reports[0].Checks {
				if check.Name == "Read-only access" {
					found = true
					want := "FAIL"
					if scenario == "deny" {
						want = "PASS"
					}
					if check.Status != want || !strings.Contains(check.Reason, "permission evaluation") || !strings.Contains(check.Reason, "write tools removed") {
						t.Fatalf("check=%+v want=%s", check, want)
					}
				}
			}
			if !found || readonlyRuns != 1 {
				t.Fatalf("found=%t probe runs=%d", found, readonlyRuns)
			}
			rows, err := os.ReadFile(os.Getenv("COCKPIT_OC2_REQUESTS"))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(rows), `"method":"PATCH"`) || strings.Contains(string(rows), `"path":"/api/session"`) || strings.Contains(string(rows), `/prompt`) {
				t.Fatal("permission verification must not replace the policy or send prompts", string(rows))
			}
			if scenario == "deny" {
				for _, action := range []string{"shell", "edit", "external_directory"} {
					if !strings.Contains(string(rows), `"action":"`+action+`"`) {
						t.Fatal("missing evaluation", action)
					}
				}
			}
		})
	}
}

func TestOpencode2ManagedHomesPreserveAbsolutePaths(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("PACTIFY_HOME", "relative-home")
	for _, d := range opencode2Homes().directories() {
		path, err := d.Managed()
		if err != nil || !filepath.IsAbs(path) || !strings.HasPrefix(path, filepath.Join(root, "relative-home")+string(filepath.Separator)) {
			t.Fatalf("%s=%s err=%v", d.Env, path, err)
		}
	}
}
