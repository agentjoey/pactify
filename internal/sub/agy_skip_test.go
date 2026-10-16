package sub

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestAgyDoctorRunsReadOnly(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	fakeAgyUserHome(t)
	h, _ := Lookup("agy")
	h.Version = func(context.Context) (string, error) { return "test", nil }
	h.Models = nil
	calls := 0
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, []Harness{h}, doctorDeps{
		lookPath: func(string) (string, error) { return "fake", nil },
		run: func(_ context.Context, store Store, id string, _ Harness, _ WorkerConfig) (Result, error) {
			calls++
			st, err := store.Load(id)
			if err != nil {
				t.Fatal(err)
			}
			if st.Request.Access != "write" {
				writeAgyDeniedDoctorLog(t, store, id)
			} else if err := os.WriteFile(filepath.Join(st.Request.Cwd, "out.txt"), []byte("ok"), 0600); err != nil {
				t.Fatal(err)
			}
			return Result{ToolCalls: 1, Status: StatusSucceeded, CLIVersion: "test", ModelVerified: true, InputTokens: 1, FinalText: "done"}, nil
		},
	})
	if err != nil || len(reports) != 1 {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	found := false
	for _, c := range reports[0].Checks {
		if c.Name == "Read-only access" {
			found = true
			if c.Status != "PASS" || h.NoReadOnly != "" {
				t.Errorf("check=%+v", c)
			}
		}
	}
	if !found || calls != 3 {
		t.Fatalf("found=%t calls=%d", found, calls)
	}
}
