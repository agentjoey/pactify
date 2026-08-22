package roles

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/agent"
)

func TestSetProfileBindLookupRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	c, err := Load()
	if err != nil {
		t.Fatalf("Load on a missing file must be empty, not an error: %v", err)
	}
	if err := c.SetProfile("frontend", Profile{Kind: "claude-code", Model: "claude-opus-4-8", Fallback: []string{"cheap"}}); err != nil {
		t.Fatal(err)
	}
	if err := c.SetProfile("cheap", Profile{Kind: "kimi-cli", Model: "kimi-code/kimi-for-coding"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Bind("w2", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	p, role, ok := got.Lookup("w2")
	if !ok || role != "frontend" || p.Kind != "claude-code" || p.Model != "claude-opus-4-8" {
		t.Fatalf("Lookup(w2) = (%+v, %q, %v)", p, role, ok)
	}
	if len(p.Fallback) != 1 || p.Fallback[0] != "cheap" {
		t.Fatalf("fallback chain lost in round-trip: %+v", p.Fallback)
	}
	if _, _, ok := got.Lookup("nobody"); ok {
		t.Fatal("an unbound seat must report ok=false")
	}
}

func TestBindUnknownRoleRejected(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	c, _ := Load()
	if err := c.Bind("w2", "nope"); err == nil {
		t.Fatal("binding a seat to an undefined role must error")
	}
}

// docPath locates docs/operations.md relative to this test's package dir
// (internal/roles → repo root is two levels up), so it works from any cwd `go
// test` is invoked with.
func docPath(t *testing.T) string {
	t.Helper()
	p := filepath.Join("..", "..", "docs", "operations.md")
	if _, err := os.Stat(p); err != nil {
		t.Skipf("docs/operations.md not readable from %s (%v) — skipping the doc/code drift check", p, err)
	}
	return p
}

// recommendedSection returns the body of the "Recommended profiles —
// antigravity" section of docs/operations.md (from its heading line to the next
// "## " heading or EOF). It FAILS if the section is absent: the whole agy-roles
// deliverable is that doc section, so its disappearance must break the build,
// not silently pass.
func recommendedSection(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	lines := strings.Split(string(b), "\n")
	start := -1
	for i, ln := range lines {
		if strings.Contains(ln, "Recommended profiles") && strings.Contains(ln, "antigravity") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf(`%s has no "Recommended profiles — antigravity" section; the agy-roles deliverable IS that section — restore it or update this test`, path)
	}
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			return strings.Join(lines[start:i], "\n")
		}
	}
	return strings.Join(lines[start:], "\n")
}

var (
	// `pactify role set <role> --kind <kind> --model <model>` (the doc's
	// copy-pasteable commands).
	docCmdRe = regexp.MustCompile(`(?m)^pactify role set\s+(\S+)\s+--kind\s+(\S+)\s+--model\s+(\S+)\s*$`)
	// `"<role>": {"kind": "<kind>", "model": "<model>"}` (the doc's equivalent
	// roles.json fragment).
	docJSONRe = regexp.MustCompile(`(?m)^\s*"([\w-]+)":\s*\{"kind":\s*"([^"]+)",\s*"model":\s*"([^"]+)"\}`)
)

// parseDocProfiles extracts role→Profile rows from one of the section's two
// representations and asserts each row uses values that actually exist in the
// code: a registered agent kind, and a model from that kind's curated
// RunnerProfile.Models list (read through agent.CandidateModels — never a
// hardcoded duplicate of it here).
func parseDocProfiles(t *testing.T, label, section string, re *regexp.Regexp) map[string]Profile {
	t.Helper()
	ms := re.FindAllStringSubmatch(section, -1)
	if len(ms) == 0 {
		t.Fatalf("no %s rows found in the recommended-profiles section:\n%s", label, section)
	}
	out := make(map[string]Profile, len(ms))
	for _, m := range ms {
		role, kind, model := m[1], m[2], m[3]
		if _, ok := agent.Get(kind); !ok {
			t.Errorf("%s: role %q documents kind %q, which is not a registered agent kind (%v)", label, role, kind, agent.Kinds())
		}
		cand := agent.CandidateModels(kind)
		if len(cand) == 0 {
			t.Errorf("%s: role %q documents kind %q, which has no curated model list (no runner profile?)", label, role, kind)
		} else if !slices.Contains(cand, model) {
			t.Errorf("%s: role %q documents model %q, which is not in %s's curated models %v — doc and internal/agent have drifted", label, role, model, kind, cand)
		}
		if prev, dup := out[role]; dup {
			t.Errorf("%s: role %q documented twice (%+v then kind=%s model=%s)", label, role, prev, kind, model)
		}
		out[role] = Profile{Kind: kind, Model: model}
	}
	return out
}

// TestAntigravityRecommendedProfiles reads the recommended antigravity role
// catalog OUT of docs/operations.md ("Recommended profiles — antigravity") and
// checks it against the code, then round-trips it through this package. The doc
// section is the entire agy-roles deliverable, so it is the input here rather
// than a hand-copied fixture: delete or edit the section and this test fails
// instead of staying green against its own copy.
//
// Three things are pinned: (1) the section exists and declares role rows; (2)
// every row's kind is registered and its model is in that kind's curated
// agent.CandidateModels list (doc↔code drift); (3) the CLI commands and the
// equivalent roles.json fragment in the doc agree with each other (doc↔doc
// drift).
//
// Effort MUST stay empty for every one of these profiles: agy-kind's
// empirical finding is that `agy --model <tier-suffixed> --effort
// <mismatched-tier>` hard-errors (exit 1, "conflicts with"), so
// RunnerProfileFor("antigravity").EffortArgs is nil — tier is expressed only
// via the model name's -medium/-low suffix. A non-empty Profile.Effort here
// would risk agentcfg re-injecting a conflicting --effort flag the moment any
// future code path starts honoring Profile.Effort for a nil-EffortArgs kind.
func TestAntigravityRecommendedProfiles(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	section := recommendedSection(t, docPath(t))
	want := parseDocProfiles(t, "`pactify role set` command", section, docCmdRe)
	fragment := parseDocProfiles(t, "roles.json fragment", section, docJSONRe)
	if !reflect.DeepEqual(want, fragment) {
		t.Fatalf("the doc's commands and its roles.json fragment disagree:\ncommands = %+v\nfragment = %+v", want, fragment)
	}
	// The section is antigravity's; every row must actually be that kind.
	for role, p := range want {
		if p.Kind != "antigravity" {
			t.Errorf("role %q in the antigravity section documents kind %q", role, p.Kind)
		}
		if p.Effort != "" {
			t.Errorf("role %q documents effort %q; agy hard-errors on a --model/--effort tier mismatch, so effort must stay unset", role, p.Effort)
		}
	}
	// A lead role the prose calls out by name, as a floor on what got parsed.
	if _, ok := want["frontend"]; !ok {
		t.Fatalf("the documented catalog lost its \"frontend\" role: %+v", want)
	}

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, p := range want {
		if err := c.SetProfile(name, p); err != nil {
			t.Fatalf("SetProfile(%q): %v", name, err)
		}
	}
	if err := c.Bind("frontend-seat", "frontend"); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}

	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for name, wantP := range want {
		p, ok := got.Profiles[name]
		if !ok {
			t.Fatalf("profile %q missing after round-trip", name)
		}
		if p.Kind != "antigravity" {
			t.Errorf("profile %q Kind = %q, want antigravity", name, p.Kind)
		}
		if p.Model != wantP.Model {
			t.Errorf("profile %q Model = %q, want %q", name, p.Model, wantP.Model)
		}
		if p.Effort != "" {
			t.Errorf("profile %q Effort = %q, want empty (agy hard-errors on mismatched --model/--effort)", name, p.Effort)
		}
	}

	// The Lookup path a bound seat actually resolves through.
	p, role, ok := got.Lookup("frontend-seat")
	if !ok || role != "frontend" || p.Kind != "antigravity" || p.Model != want["frontend"].Model {
		t.Fatalf("Lookup(frontend-seat) = (%+v, %q, %v), want the documented frontend profile %+v", p, role, ok, want["frontend"])
	}
}

func TestPathHonorsPactifyHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	if want := filepath.Join(home, "roles.json"); Path() != want {
		t.Fatalf("Path() = %q, want %q", Path(), want)
	}
}
