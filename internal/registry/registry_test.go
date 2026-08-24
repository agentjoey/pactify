package registry

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestMissing(t *testing.T) {
	if !Missing("") {
		t.Errorf("Missing(\"\") = false, want true")
	}

	d := t.TempDir()
	pactDir := filepath.Join(d, ".pact")
	if err := os.MkdirAll(pactDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if Missing(d) {
		t.Errorf("Missing(%q) = true, want false (has .pact dir)", d)
	}

	d2 := t.TempDir()
	if !Missing(d2) {
		t.Errorf("Missing(%q) = false, want true (no .pact dir)", d2)
	}

	notExist := filepath.Join(d, "not-exist")
	if !Missing(notExist) {
		t.Errorf("Missing(%q) = false, want true (directory completely missing)", notExist)
	}

	d3 := t.TempDir()
	pactFile := filepath.Join(d3, ".pact")
	if err := os.WriteFile(pactFile, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Missing(d3) {
		t.Errorf("Missing(%q) = false, want true (.pact is a file)", d3)
	}
}

func TestSlug(t *testing.T) {
	cases := map[string]string{"Pactify": "pactify", "my repo!": "my-repo", "TradeLinks": "tradelinks"}
	for in, want := range cases {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q)=%q want %q", in, got, want)
		}
	}
}

func TestAddListRemoveRoundtrip(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("pactify", "/abs/pactify", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("", "/abs/TradeLinks", ""); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2, _ := Load()
	if len(r2.Projects) != 2 || r2.Projects[1].Name != "tradelinks" {
		t.Fatalf("bad reload: %+v", r2.Projects)
	}
	if err := r2.Add("pactify", "/other", ""); err == nil {
		t.Fatal("duplicate name must error")
	}
	if err := r2.Remove("pactify"); err != nil {
		t.Fatal(err)
	}
	if len(r2.Projects) != 1 || r2.Projects[0].Name != "tradelinks" {
		t.Fatalf("remove failed: %+v", r2.Projects)
	}
}

func TestLoadMissingIsEmpty(t *testing.T) {
	t.Setenv("PACTIFY_HOME", filepath.Join(t.TempDir(), "nope"))
	r, err := Load()
	if err != nil || len(r.Projects) != 0 {
		t.Fatalf("want empty,nil got %+v,%v", r.Projects, err)
	}
}

func TestAddGroupPersists(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	r, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Add("proj", "/abs/proj", "mygroup"); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	r2, _ := Load()
	if len(r2.Projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(r2.Projects))
	}
	if r2.Projects[0].Group != "mygroup" {
		t.Fatalf("group=%q want %q", r2.Projects[0].Group, "mygroup")
	}
}

func TestRenamePreservesPathAndGroup(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	if err := r.Add("old-name", "/tmp/proj", "team-a"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := r.Rename("old-name", "new-name"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if len(r.Projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(r.Projects))
	}
	p := r.Projects[0]
	if p.Name != "new-name" {
		t.Errorf("name = %q, want new-name", p.Name)
	}
	if p.Group != "team-a" {
		t.Errorf("group = %q, want team-a (preserved)", p.Group)
	}
	if p.Path != "/tmp/proj" {
		t.Errorf("path = %q, want /tmp/proj (preserved)", p.Path)
	}
}

func TestRenameEmptyNewNameIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	_ = r.Add("a", "/tmp/a", "")
	if err := r.Rename("a", "!!!"); err == nil {
		t.Fatal("new name that slugs to empty must error")
	}
}

func TestRenameUnknownIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	if err := r.Rename("ghost", "x"); err == nil {
		t.Fatal("renaming an unknown project must error")
	}
}

func TestRenameToExistingNameIsError(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	var r Registry
	_ = r.Add("a", "/tmp/a", "")
	_ = r.Add("b", "/tmp/b", "")
	if err := r.Rename("a", "b"); err == nil {
		t.Fatal("renaming onto an existing name must error")
	}
}

// EnsureRegistered idempotently registers a path (spec: auto-register at init/
// orchestrate so agent-started projects are visible without a manual step).
func TestEnsureRegisteredIdempotent(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	// t.TempDir() is under os.TempDir(), which auto-register now refuses (see
	// TestEnsureRegisteredSkipsSystemTempPath). Opt back in: this test is about
	// idempotence, not about the temp guard.
	t.Setenv("PACTIFY_ALLOW_TEMP_REGISTER", "1")
	dir := t.TempDir()

	name, added, err := EnsureRegistered(dir)
	if err != nil || !added || name != Slug(filepath.Base(dir)) {
		t.Fatalf("first call must register: name=%q added=%v err=%v", name, added, err)
	}
	// Second call on the SAME path is a no-op (already registered), same name.
	name2, added2, err := EnsureRegistered(dir)
	if err != nil || added2 || name2 != name {
		t.Fatalf("re-register must be a no-op: name=%q added=%v err=%v", name2, added2, err)
	}
	// Only one entry.
	r, _ := Load()
	if len(r.Projects) != 1 {
		t.Fatalf("expected exactly one registered project, got %d", len(r.Projects))
	}
}

// A different path whose basename slugs to an already-taken name is a real
// conflict — EnsureRegistered surfaces the error (the caller decides whether to
// warn or fail; init must warn, not block).
func TestEnsureRegisteredNameConflict(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv("PACTIFY_ALLOW_TEMP_REGISTER", "1") // see TestEnsureRegisteredIdempotent
	base := t.TempDir()
	a := filepath.Join(base, "proj")
	b := filepath.Join(base, "sub", "proj") // same basename "proj", different path
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	if _, _, err := EnsureRegistered(a); err != nil {
		t.Fatal(err)
	}
	if _, added, err := EnsureRegistered(b); err == nil || added {
		t.Fatalf("same-basename different-path must conflict, got added=%v err=%v", added, err)
	}
}

// [REGISTRY-2] Auto-registration must refuse throwaway temp paths. Every one-off
// `mktemp -d` experiment used to be written permanently into the user's real
// ~/.pactify/projects.json; a project called "agyproj" actually ended up there,
// and picking it on the dashboard showed a blank board that read as a broken
// product rather than a dead registration.
func TestEnsureRegisteredSkipsSystemTempPath(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir() // under os.TempDir() by construction

	name, added, err := EnsureRegistered(dir)
	if added {
		t.Errorf("a path under %s must not be auto-registered", os.TempDir())
	}
	if name != "" {
		t.Errorf("skipped path must not report a name, got %q", name)
	}
	if err == nil {
		t.Fatal("the skip must be reported to the caller so it can tell the user why the project is not on the dashboard")
	}
	// A deliberate skip must be distinguishable from a real failure.
	if !errors.Is(err, ErrTempPath) {
		t.Errorf("err = %v, want errors.Is(err, ErrTempPath)", err)
	}

	r, lerr := Load()
	if lerr != nil {
		t.Fatal(lerr)
	}
	if len(r.Projects) != 0 {
		t.Fatalf("registry must stay empty, got %+v", r.Projects)
	}
}

// macOS spells the same temp directory two ways: /tmp is a symlink to
// /private/tmp, and $TMPDIR lives under /var/folders which is really
// /private/var/folders. A guard that compares raw strings catches whichever
// spelling it happens to be handed and misses the other, so both must be refused.
func TestEnsureRegisteredSkipsTmpSymlinkAliasBothSpellings(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())

	dir, err := os.MkdirTemp("/tmp", "pactify-tempguard-")
	if err != nil {
		t.Skipf("no writable /tmp on this platform: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{dir, resolved} {
		if _, added, err := EnsureRegistered(p); added || !errors.Is(err, ErrTempPath) {
			t.Errorf("EnsureRegistered(%q) = added:%v err:%v, want ErrTempPath", p, added, err)
		}
		if !TempPath(p) {
			t.Errorf("TempPath(%q) = false, want true", p)
		}
	}
	if r, _ := Load(); len(r.Projects) != 0 {
		t.Fatalf("registry must stay empty, got %+v", r.Projects)
	}
}

// A non-temp path still auto-registers: the feature (agent-started projects show
// up on the dashboard without a manual step) is wanted and must not regress.
func TestEnsureRegisteredStillRegistersNonTempPath(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := filepath.FromSlash("/pactify-test-not-a-temp-path/demo-project")

	name, added, err := EnsureRegistered(dir)
	if err != nil || !added || name != "demo-project" {
		t.Fatalf("non-temp path must register: name=%q added=%v err=%v", name, added, err)
	}
}

func TestTempPath(t *testing.T) {
	tmp := t.TempDir()
	cases := map[string]bool{
		tmp:                        true,
		filepath.Join(tmp, "proj"): true, // need not exist
		os.TempDir():               true, // the root itself
		"/tmp":                     true,
		"/tmp/agyproj":             true,
		"/pactify-not-temp/demo":   false,
		"/Users":                   false,
		// A path that merely starts with the same characters as a temp root is
		// NOT under it — the check is on path components, not on a raw prefix.
		"/tmp-not-really":      false,
		"/tmp-not-really/proj": false,
	}
	// "/private/tmp" is only the same directory as "/tmp" where the OS makes
	// /tmp a symlink to it (macOS). On Linux (the CI runner) /tmp is a real
	// directory and /private/tmp is not a temp root at all, so asserting this
	// case unconditionally fails there — it must track the OS, not be a
	// universal truth. Confirmed as an actual CI failure (PR #47), not a
	// hypothetical: TempPath("/private/tmp/agyproj") on ubuntu-latest is
	// correctly false, and a portable test must expect that.
	cases["/private/tmp/agyproj"] = runtime.GOOS == "darwin"
	for path, want := range cases {
		if got := TempPath(path); got != want {
			t.Errorf("TempPath(%q) = %v, want %v", path, got, want)
		}
	}
}

// The opt-out exists for test harnesses, which must run whole projects out of
// temp dirs. Without it there is no way to exercise auto-registration at all.
func TestEnsureRegisteredTempOptOut(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	t.Setenv(allowTempEnv, "1")
	dir := t.TempDir()

	if _, added, err := EnsureRegistered(dir); err != nil || !added {
		t.Fatalf("%s=1 must re-enable temp auto-register: added=%v err=%v", allowTempEnv, added, err)
	}
}

// The guard belongs to the AUTOMATIC path only. `pactify register <path>` goes
// through Add, and an explicit command is a deliberate act (debugging a scratch
// repo, a genuinely temp-rooted checkout) — so Add must keep accepting it.
func TestAddAcceptsTempPathExplicitly(t *testing.T) {
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := t.TempDir()

	var r Registry
	if err := r.Add("", dir, ""); err != nil {
		t.Fatalf("explicit registration of a temp path must be allowed: %v", err)
	}
	if len(r.Projects) != 1 {
		t.Fatalf("want 1 project, got %+v", r.Projects)
	}
}
