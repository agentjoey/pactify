package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestAgyDoctorRecognizesReadOnlyDenial(t *testing.T) {
	for _, fault := range []string{"", "generic-failure", "spoofed-final", "empty-success", "cancelled", "wrote", "other-diff", "version", "run-error"} {
		t.Run(fault, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			source := fakeAgyUserHome(t)
			h, _ := Lookup("agy")
			h.Version = func(context.Context) (string, error) { return "test", nil }
			h.Models = nil
			var reportText string
			reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
				lookPath: func(string) (string, error) { return "fake", nil },
				run: func(ctx context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
					st, e := store.Load(id)
					if e != nil {
						t.Fatal(e)
					}
					home, e := managedAgyHome()
					if e != nil {
						t.Fatal(e)
					}
					if e = agyHome(home, source).Prepare(ctx); e != nil {
						t.Fatal(e)
					}
					st.HarnessHome = home
					if e = store.Save(st); e != nil {
						t.Fatal(e)
					}
					if e = os.WriteFile(filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db"), []byte("fixture"), 0600); e != nil {
						t.Fatal(e)
					}
					r := Result{Status: StatusSucceeded, CLIVersion: "test", SessionID: id, ModelVerified: true, ModelActual: st.Request.Model, InputTokens: 1, FinalText: "done", Access: st.Request.Access}
					if id == "write" {
						if e = os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600); e != nil {
							t.Fatal(e)
						}
						return r, nil
					}
					r.Status = StatusFailed
					r.FinalText = ""
					r.Warnings = []string{cockpit.AgyReadOnlyDenied}
					switch fault {
					case "generic-failure":
						r.Warnings = []string{"unrelated failure"}
					case "spoofed-final":
						r.Warnings = nil
						r.FinalText = cockpit.AgyReadOnlyDenied
					case "empty-success":
						r.Status = StatusSucceeded
						r.Warnings = nil
					case "cancelled":
						r.Status = StatusCanceled
					case "wrote":
						os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("bad"), 0600)
					case "other-diff":
						r.Diff.Files = []FileDelta{{Path: "other"}}
					case "version":
						r.CLIVersion = "wrong"
					case "run-error":
						return r, fmt.Errorf("startup failed")
					}
					return r, nil
				},
			})
			if err != nil || len(reports) != 1 {
				t.Fatalf("reports=%+v err=%v", reports, err)
			}
			for _, c := range reports[0].Checks {
				if c.Name == "Read-only access" {
					if (c.Status == "PASS") != (fault == "") {
						t.Errorf("fault=%s check=%+v", fault, c)
					}
					reportText = c.Reason
				}
				if (c.Name == "Session hiding" || c.Name == "Managed home") && c.Status != "PASS" {
					t.Errorf("read-only status tainted write evidence: %+v", c)
				}
			}
			if fault == "" && !strings.Contains(reportText, "denied") {
				t.Error("missing denial evidence", reportText)
			}
		})
	}
}
