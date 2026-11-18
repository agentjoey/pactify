package sub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func TestCursorRegistryContract(t *testing.T) {
	h, ok := Lookup("cursor")
	if !ok {
		t.Fatal("cursor is not registered")
	}
	if h.Experimental == "" || !strings.Contains(h.Experimental, "no OS sandbox") {
		t.Fatalf("cursor must be labelled experimental and unsandboxed: %q", h.Experimental)
	}
	if !h.MinimalConfig || h.Home == nil || h.Home.Env != "CURSOR_CONFIG_DIR" || h.Conceal == nil {
		t.Fatalf("private config contract: %+v", h)
	}
	if len(h.Efforts()) != 0 || h.NoReadOnly != "" {
		t.Fatalf("effort values %v, no-read-only %q", h.Efforts(), h.NoReadOnly)
	}
	if !requiresReadOnlyBaseline(Request{Harness: "cursor", Access: "read-only"}) || requiresReadOnlyBaseline(Request{Harness: "cursor", Access: "write"}) {
		t.Fatal("cursor read-only jobs must be verified against a workspace baseline")
	}
	if err := h.Conceal(ConcealContext{}); err == nil {
		t.Fatal("user-config sessions cannot be concealed")
	}
}

func TestCursorExecutableLookupPrefersCursorAgent(t *testing.T) {
	found := map[string]bool{"cursor-agent": true, "agent": true}
	look := func(name string) (string, error) {
		if found[name] {
			return "/bin/" + name, nil
		}
		return "", errors.New("missing")
	}
	if p, err := lookupHarnessPath("cursor", look); err != nil || p != "/bin/cursor-agent" {
		t.Fatalf("%q %v", p, err)
	}
	found["cursor-agent"] = false
	if p, err := lookupHarnessPath("cursor", look); err != nil || p != "/bin/agent" {
		t.Fatalf("%q %v", p, err)
	}
	found["agent"] = false
	if err := harnessInstalled("cursor", look); err == nil {
		t.Fatal("missing cursor reported installed")
	}
	if p, err := lookupHarnessPath("codex", func(n string) (string, error) { return "/bin/" + n, nil }); err != nil || p != "/bin/codex" {
		t.Fatalf("other harnesses keep their own name: %q %v", p, err)
	}
}

func TestCursorConfigDirFollowsCursorLookup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", "")
	t.Setenv("CURSOR_CONFIG_DIR", "")
	if got, _ := cursorConfigDir(); got != filepath.Join(home, ".cursor") {
		t.Fatalf("default %q", got)
	}
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if got, _ := cursorConfigDir(); got != filepath.Join(xdg, "cursor") {
		t.Fatalf("xdg %q", got)
	}
	explicit := t.TempDir()
	t.Setenv("CURSOR_CONFIG_DIR", explicit)
	if got, _ := cursorConfigDir(); got != explicit {
		t.Fatalf("explicit %q", got)
	}
}

// The managed home links nothing from the user's directory: cli-config.json
// holds allowlists and team selection that delegated jobs must not inherit.
func TestCursorManagedHomeLinksNothing(t *testing.T) {
	source := t.TempDir()
	for _, name := range []string{"cli-config.json", "auth.json", "mcp.json"} {
		if err := os.WriteFile(filepath.Join(source, name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	home := filepath.Join(t.TempDir(), "managed")
	layout := cursorHome(home, source)
	if len(layout.Links) != 0 {
		t.Fatalf("links: %v", layout.Links)
	}
	if err := layout.Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			t.Fatalf("user file leaked into the private home: %s", e.Name())
		}
	}
	if err := layout.Check(); err != nil {
		t.Fatal(err)
	}
	// A user without a config directory yet still gets a working private home.
	if err := cursorHome(filepath.Join(t.TempDir(), "m2"), filepath.Join(t.TempDir(), "absent")).Prepare(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCursorVerifyRequiresReportedModel(t *testing.T) {
	if v := verifyCursor(context.Background(), VerificationInput{}); v.Verified || !v.UsagePartial {
		t.Fatalf("%+v", v)
	}
	if v := verifyCursor(context.Background(), VerificationInput{Info: cockpit.SessionInfo{Model: "composer-2"}}); !v.Verified || v.Model != "composer-2" {
		t.Fatalf("%+v", v)
	}
}

func TestCursorDoctorFailureClassificationOffline(t *testing.T) {
	if c := doctorFailureClassification("cursor"); c.Status != "PASS" {
		t.Fatalf("%+v", c)
	}
}
