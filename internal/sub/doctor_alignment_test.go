package sub

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
)

func TestDoctorUniformChecksWhenUnavailable(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var hs []Harness
	for _, name := range []string{"codex", "kimi", "opencode", "agy"} {
		h, _ := Lookup(name)
		hs = append(hs, h)
	}
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(t.TempDir(), "jobs")}, hs, doctorDeps{lookPath: func(string) (string, error) { return "", errors.New("unavailable") }})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Installed", "Write access", "Model", "Read-only access", "Read-only hook", "Network", "Sandbox", "Failure classification", "Session hiding", "Managed home"}
	for _, r := range reports {
		if len(r.Checks) != len(want) {
			t.Errorf("%s: got %d checks, want %d", r.Harness, len(r.Checks), len(want))
			continue
		}
		for _, c := range r.Checks {
			if c.Name == "Sandbox" {
				want := "WARN"
				if r.Harness == "codex" {
					want = "PASS"
				}
				if c.Status != want {
					t.Errorf("%s sandbox status=%s", r.Harness, c.Status)
				}
				if want == "WARN" && c.Reason != "not sandboxed: writes are not confined by the OS" {
					t.Errorf("sandbox declaration: %+v", c)
				}
			}
			if c.Name == "Read-only hook" && r.Harness != "agy" && c.Status != "SKIP" {
				t.Errorf("unexpected hook: %+v", c)
			}
		}
		for i, c := range r.Checks {
			if c.Name != want[i] {
				t.Errorf("%s: check %d = %s", r.Harness, i, c.Name)
			}
		}
	}
}

func TestDoctorOfflineFailureInjection(t *testing.T) {
	for _, name := range []string{"codex", "kimi", "opencode", "agy"} {
		c := doctorFailureClassification(name)
		if c.Status != "PASS" {
			t.Errorf("%s: %+v", name, c)
		}
	}
}
