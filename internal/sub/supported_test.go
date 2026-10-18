package sub

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestOpencodeSupportedRejectsMajorThree(t *testing.T) {
	for _, version := range []string{"1.19.0", "1.19.3", "v1.20.0", "v2.0.32", "2.0.0", "0.9.1", "dev", ""} {
		if err := opencodeSupported(version); err != nil {
			t.Fatalf("%q rejected: %v", version, err)
		}
	}
	for _, version := range []string{"v3.0.32", "3.0.0", "3.1.4"} {
		err := opencodeSupported(version)
		if err == nil || !strings.Contains(err.Error(), "opencode "+version+" is not supported yet") || !strings.Contains(err.Error(), "opencode-ai@1") {
			t.Fatalf("%q: err=%v", version, err)
		}
	}
}

// Future major versions must be rejected before the backend starts.
func TestRunJobRejectsUnsupportedVersionBeforeStart(t *testing.T) {
	store, s, b := workerFixture(t, "write")
	s.script = func(s *fakeSession) {
		s.events <- cockpit.Event{Kind: cockpit.EventState, State: cockpit.StateTurnCompleted}
	}
	h := newFakeHarness(b)
	h.Name = "opencode"
	h.Version = func(context.Context) (string, error) { return "v3.0.32", nil }
	h.Supported = opencodeSupported
	r, err := RunJob(context.Background(), store, "j1", h, WorkerConfig{})
	st, _ := store.Load("j1")
	if err == nil || !strings.Contains(err.Error(), "opencode v3.0.32 is not supported yet") || r.Status == StatusSucceeded || st.Status != StatusFailed || b.gotOpts.Env != nil {
		t.Fatalf("result=%+v state=%+v err=%v started=%v", r, st, err, b.gotOpts.Env != nil)
	}
}

func TestDoctorReportsUnsupportedVersionAsNotInstalled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	h, _ := Lookup("opencode")
	h.Version = func(context.Context) (string, error) { return "v3.0.32", nil }
	h.Backend = func() cockpit.Backend { t.Fatal("backend started for an unsupported version"); return nil }
	reports, err := runDoctor(context.Background(), Store{Root: filepath.Join(home, "jobs")}, []Harness{h}, doctorDeps{lookPath: func(string) (string, error) { return "test", nil }})
	if err != nil || len(reports) != 1 || reports[0].Passed() {
		t.Fatalf("reports=%+v err=%v", reports, err)
	}
	installed := reports[0].Checks[0]
	if installed.Name != "Installed" || !strings.Contains(installed.Reason, "not supported yet") {
		t.Fatalf("checks=%+v", reports[0].Checks)
	}
	if table := readVerifiedVersions(filepath.Join(home, "verified-versions.json")); table["opencode"].Version != "" {
		t.Fatalf("unsupported version recorded as verified: %+v", table)
	}
}

func TestCatalogSkipsDiscoveryForUnsupportedVersion(t *testing.T) {
	home := t.TempDir()
	h, _ := Lookup("opencode")
	h.Version = func(context.Context) (string, error) { return "v3.0.32", nil }
	h.Models = func(context.Context, cockpit.StartOpts) ([]string, error) {
		t.Fatal("model discovery started for an unsupported version")
		return nil, nil
	}
	c := newHarnessCatalog()
	c.now = func() time.Time { return time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC) }
	c.lookupPath = func(string) (string, error) { return "test", nil }
	row := c.harness(context.Background(), Store{Root: filepath.Join(home, "jobs")}, h, HarnessDefault{}, true)
	if row.Version != "v3.0.32" || !strings.Contains(row.Models.Error, "not supported yet") {
		t.Fatalf("row=%+v", row)
	}
}
