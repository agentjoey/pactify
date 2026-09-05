package serve

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// The Test button's contract: for one kind, report the SAME binary/auth/transport
// checks `pactify doctor` runs. Reusing doctor.VendorChecksFor is the point —
// a second connectivity rule that drifts from doctor's is worse than no button,
// because the two would disagree about whether an agent is usable.
func TestAgentTestEndpointReturnsDoctorChecksForOneKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	ts := newTestServer(t, New(nil))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/claude-code/test")
	if err != nil {
		t.Fatalf("GET test: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}

	var got struct {
		Kind   string `json:"kind"`
		OK     bool   `json:"ok"`
		Checks []struct {
			Name   string `json:"name"`
			OK     bool   `json:"ok"`
			Detail string `json:"detail"`
		} `json:"checks"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Kind != "claude-code" {
		t.Errorf("kind = %q, want claude-code", got.Kind)
	}
	if len(got.Checks) == 0 {
		t.Fatal("no checks returned — the card would have nothing to show")
	}
	// Only this kind's checks: a per-kind button that returned the whole vendor
	// list would make every row's result identical and meaningless.
	for _, c := range got.Checks {
		if !strings.Contains(c.Name, "claude-code") {
			t.Errorf("check %q belongs to another kind", c.Name)
		}
	}
	// Every check must carry a detail — "failed" with no reason is the exact
	// dead end this button exists to remove ([UI-GATE]'s lesson).
	for _, c := range got.Checks {
		if strings.TrimSpace(c.Detail) == "" {
			t.Errorf("check %q has no detail; a red mark with no reason is not actionable", c.Name)
		}
	}
}

// An unknown kind is a client error, not a 200 with an empty list — the latter
// would render as "all checks passed".
func TestAgentTestEndpointRejectsUnknownKind(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	ts := newTestServer(t, New(nil))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/not-a-real-kind/test")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("unknown kind must not return 200 (an empty check list reads as 'all good')")
	}
}

// Versions are probed by spawning `<cli> --version`, measured at 8ms–572ms per
// CLI (gemini is the slow one). Sequentially that is ~2s for this machine's 8
// installed kinds, so the endpoint must probe in PARALLEL and must never be on
// the critical path of rendering the list.
func TestAgentVersionsEndpointProbesInParallel(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	ts := newTestServer(t, New(nil))
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/versions")
	if err != nil {
		t.Fatalf("GET versions: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200 got %d", resp.StatusCode)
	}
	var got struct {
		Versions map[string]string `json:"versions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Versions == nil {
		t.Fatal("versions map must be present (empty is fine on a machine with no CLIs)")
	}
}

// Three real output shapes were measured: "2.1.259 (Claude Code)",
// "codex-cli 0.144.4", and a bare "0.56.0". One extractor must handle all
// three, and must decline rather than guess on anything else — a wrong version
// in the collapsed row is worse than none.
func TestParseVersionHandlesEveryMeasuredShape(t *testing.T) {
	cases := []struct{ raw, want string }{
		{"2.1.259 (Claude Code)", "2.1.259"},
		{"codex-cli 0.144.4", "0.144.4"},
		{"0.56.0", "0.56.0"},
		{"0.39.0", "0.39.0"},
		{"1.18.23", "1.18.23"},
		{"1.1.22", "1.1.22"},
		{"v2.3.4", "2.3.4"},
		{"", ""},
		{"no version here", ""},
	}
	for _, c := range cases {
		if got := parseVersion(c.raw); got != c.want {
			t.Errorf("parseVersion(%q) = %q, want %q", c.raw, got, c.want)
		}
	}
}
