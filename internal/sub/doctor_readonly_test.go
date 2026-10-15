package sub

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDoctorReadOnlyProbeRequiresExecutedWriteCommand(t *testing.T) {
	for _, tc := range []struct {
		name      string
		toolCalls int
		status    string
	}{
		{"claims-blocked-without-tool", 0, "FAIL"},
		{"tool-executed-without-file", 1, "PASS"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			h, _ := Lookup("codex")
			h.Version = func(context.Context) (string, error) { return "test", nil }
			readOnlyRan := false
			d := doctorDeps{
				lookPath: func(string) (string, error) { return "/test/codex", nil },
				run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
					st, err := store.Load(id)
					if err != nil {
						t.Fatal(err)
					}
					req := st.Request
					r := Result{Status: StatusSucceeded, CLIVersion: "test", ModelActual: req.Model, ModelVerified: true, InputTokens: 1, FinalText: "done", ToolCalls: 1}
					if req.Access == "write" {
						for name, content := range map[string]string{"out.txt": "ok", "doctor-network.json": `{"bind":true,"external":true}`} {
							if err := os.WriteFile(filepath.Join(req.Cwd, name), []byte(content), 0600); err != nil {
								t.Fatal(err)
							}
						}
						return r, nil
					}
					readOnlyRan = true
					// Assert the request at the harness boundary: the agent must get
					// an executable command and must return its actual output.
					if !strings.Contains(req.Brief, "sh -c 'echo hi > out.txt'; echo EXIT=$?") || !strings.Contains(req.Brief, "verbatim") {
						t.Errorf("read-only probe must specify a shell write and verbatim output: %s", req.Brief)
					}
					if _, err := os.Lstat(filepath.Join(req.Cwd, "out.txt")); !os.IsNotExist(err) {
						t.Fatalf("read-only target must start absent: %v", err)
					}
					// Identical claimed denial, with only actual tool usage differing.
					r.FinalText = "sh: out.txt: Read-only file system\nEXIT=2"
					r.ToolCalls = tc.toolCalls
					return r, nil
				},
			}
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, d)
			if err != nil || len(reports) != 1 || !readOnlyRan {
				t.Fatalf("reports=%+v readOnlyRan=%t err=%v", reports, readOnlyRan, err)
			}
			var status string
			for _, check := range reports[0].Checks {
				if check.Name == "Read-only access" {
					status = check.Status
				}
			}
			if status != tc.status || reports[0].Passed() != (tc.status == "PASS") {
				t.Fatalf("read-only status=%s, want %s: %+v", status, tc.status, reports[0])
			}
		})
	}
}
