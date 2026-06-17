# Custom-Agent Manifest API — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:subagent-driven-development or superpowers:executing-plans. Steps use checkbox (`- [ ]`) syntax. (This plan is being executed inline by claude, task-by-task, with a commit per task.)

**Goal:** Let users plug a custom agent into Pactify via a TOML manifest
(`~/.pactify/agents/*.toml`) — no Go edits — mapped add-only onto the existing
agent registry so orchestrate / agentcfg / scan / Settings work unchanged.

**Architecture:** New `internal/agentmanifest` parses+validates manifests and maps
each onto an `agent.External` DTO; `agent.RegisterExternal` inserts it into the
built-in `registry`/`runnerProfiles` maps (rejecting built-in collisions). A pure
`RenderArgs` replaces the per-kind `BuildArgs` closures (placeholder template). A
single load in `main()` covers CLI + serve. CLI + serve endpoints + a Settings form.

**Tech Stack:** Go + `github.com/pelletier/go-toml/v2` (new direct dep). React/Tailwind for the form.

**Spec:** `docs/superpowers/specs/2026-06-17-custom-agent-manifest-design.md`

---

## File Structure

**New:**
- `internal/agentmanifest/manifest.go` — `Manifest` (TOML), `Load`, `Validate`, `toExternal`, `LoadAndRegister`.
- `internal/agentmanifest/manifest_test.go`
- `internal/agentmanifest/render.go` — `RenderArgs` (placeholder template → argv).
- `internal/agentmanifest/render_test.go`
- `cmd/pactify/cmd_agent_manifest.go` — `agent manifest list/validate/show/add/remove`.
- `cmd/pactify/cmd_agent_manifest_test.go`
- `internal/serve/manifests.go` — GET/POST/DELETE `/api/agents/manifests`.
- `internal/serve/manifests_test.go`
- `web/src/components/ops/CustomAgentForm.tsx` + `.test.tsx`

**Modified:**
- `internal/agent/agent.go` — `External` DTO + `RegisterExternal`; `Format`/`Scope` string parsers.
- `internal/agent/launch.go` — `RunnerProfileFor`/`CandidateModels`/`Drivable` already read `runnerProfiles` (now mutated by RegisterExternal — no change needed beyond the map being writable).
- `internal/orchestrate/runner.go` — substitute `{seat}` token with `lc.Seat` at exec.
- `cmd/pactify/main.go` — `agentmanifest.LoadAndRegister()` before `Execute()`.
- `cmd/pactify/cmd_agent.go` — register `newAgentManifestCmd()`.
- `web/src/lib/api.ts` — manifest list/create/delete.
- `web/src/components/ops/OpsView.tsx` — mount the form.
- `go.mod` / `go.sum` — add go-toml/v2.

---

## Phase A — core (no UI)

### Task A1: add the go-toml/v2 dependency

**Files:** `go.mod`, `go.sum`

- [ ] **Step 1: Add the dep**

Run:
```bash
go get github.com/pelletier/go-toml/v2@latest
go mod tidy
```
Expected: `go.mod` gains `github.com/pelletier/go-toml/v2 vX.Y.Z` under `require`.

- [ ] **Step 2: Verify it builds**

Run: `go build ./...`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "build: add github.com/pelletier/go-toml/v2 (custom-agent manifest parsing)"
```

---

### Task A2: `agent.Format`/`Scope` string parsers + `External`/`RegisterExternal`

**Files:**
- Modify: `internal/agent/agent.go`
- Test: `internal/agent/external_test.go` (new)

- [ ] **Step 1: Write the failing test** (`internal/agent/external_test.go`)

```go
package agent

import "testing"

func TestParseFormatAndScope(t *testing.T) {
	cases := map[string]Format{"mcpServers": JSONMcpServers, "opencode": JSONOpencode, "toml": TOML}
	for s, want := range cases {
		got, ok := ParseFormat(s)
		if !ok || got != want {
			t.Errorf("ParseFormat(%q) = %v,%v want %v", s, got, ok, want)
		}
	}
	if _, ok := ParseFormat("bogus"); ok {
		t.Error("ParseFormat(bogus) should be !ok")
	}
	if sc, ok := ParseScope("global"); !ok || sc != Global {
		t.Errorf("ParseScope(global) = %v,%v", sc, ok)
	}
	if _, ok := ParseScope("nope"); ok {
		t.Error("ParseScope(nope) should be !ok")
	}
}

func TestRegisterExternalAddsKindAndRejectsBuiltinCollision(t *testing.T) {
	t.Cleanup(func() { delete(registry, "myagent"); delete(runnerProfiles, "myagent") })

	rp := RunnerProfile{Command: "myagent", DefaultModel: "m1", Models: []string{"m1"},
		BuildArgs: func(model string, _ PermPosture, briefing string) []string {
			return []string{"run", "-m", model, briefing}
		}}
	ext := External{Kind: "myagent", Entry: "AGENTS.md", Binary: "myagent",
		HasMCP: true, MCPConfigPath: ".myagent/mcp.json", MCPScope: Project, MCPFormat: JSONMcpServers,
		Runner: &rp}
	if err := RegisterExternal(ext); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := Get("myagent"); !ok {
		t.Fatal("myagent not in registry after register")
	}
	if !Drivable("myagent") {
		t.Fatal("myagent should be drivable (has runner)")
	}
	if got := CandidateModels("myagent"); len(got) != 1 || got[0] != "m1" {
		t.Fatalf("CandidateModels = %v", got)
	}
	// add-only: colliding with a built-in is rejected.
	if err := RegisterExternal(External{Kind: "opencode", Binary: "x"}); err == nil {
		t.Fatal("registering a built-in kind must be rejected")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agent/ -run "ParseFormatAndScope|RegisterExternal"`
Expected: FAIL — undefined ParseFormat/ParseScope/External/RegisterExternal.

- [ ] **Step 3: Write minimal implementation** (append to `internal/agent/agent.go`)

```go
// ParseFormat maps a manifest format string onto the Format enum.
func ParseFormat(s string) (Format, bool) {
	switch s {
	case "mcpServers":
		return JSONMcpServers, true
	case "opencode":
		return JSONOpencode, true
	case "toml":
		return TOML, true
	default:
		return 0, false
	}
}

// ParseScope maps a manifest scope string onto the Scope enum.
func ParseScope(s string) (Scope, bool) {
	switch s {
	case "project":
		return Project, true
	case "global":
		return Global, true
	default:
		return 0, false
	}
}

// External is a custom agent declared by a user manifest, mapped into the
// registry by RegisterExternal. Runner is nil for a non-drivable (manual) kind;
// HasMCP=false means no MCP wiring (cfgPath left empty).
type External struct {
	Kind, Entry, Binary string
	HasMCP              bool
	MCPConfigPath       string
	MCPScope            Scope
	MCPFormat           Format
	Desktop             bool
	Runner              *RunnerProfile
}

// RegisterExternal inserts a custom agent into the registry (add-only): a kind
// that collides with a built-in is rejected, so a manifest can never shadow a
// verified built-in. In-package, so it can build the unexported spec.
func RegisterExternal(e External) error {
	if _, builtin := registry[e.Kind]; builtin {
		return fmt.Errorf("custom agent %q collides with a built-in kind (manifests are add-only)", e.Kind)
	}
	cfg := ""
	if e.HasMCP {
		cfg = e.MCPConfigPath
	}
	registry[e.Kind] = spec{
		kind: e.Kind, entry: e.Entry, cfgPath: cfg,
		scope: e.MCPScope, format: e.MCPFormat, desktop: e.Desktop, detectBin: e.Binary,
	}
	if e.Runner != nil {
		runnerProfiles[e.Kind] = *e.Runner
	}
	return nil
}
```

Add `"fmt"` to the agent.go import block if absent.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agent/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agent/agent.go internal/agent/external_test.go
git commit -m "feat(agent): External + RegisterExternal (add-only) + Format/Scope parsers"
```

---

### Task A3: `RenderArgs` placeholder template

**Files:**
- Create: `internal/agentmanifest/render.go`
- Test: `internal/agentmanifest/render_test.go`

- [ ] **Step 1: Write the failing test** (`internal/agentmanifest/render_test.go`)

```go
package agentmanifest

import (
	"reflect"
	"testing"

	"github.com/agentjoey/pactify/internal/agent"
)

func TestRenderArgs(t *testing.T) {
	perm := Permission{Blanket: []string{"--yolo"}, Scoped: []string{"--allowed-tools", "{tools}"}}
	args := []string{"run", "-m", "{model}", "{permission}", "{briefing}"}

	// blanket posture, pinned model
	got := RenderArgs(args, perm, agent.PermPosture{}, "gpt-5", "BRIEF")
	want := []string{"run", "-m", "gpt-5", "--yolo", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("blanket = %v, want %v", got, want)
	}

	// empty model → drop {model} AND the preceding lone -m
	got = RenderArgs(args, perm, agent.PermPosture{}, "", "BRIEF")
	want = []string{"run", "--yolo", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("empty-model = %v, want %v", got, want)
	}

	// scoped posture → scoped fragment with {tools} joined
	got = RenderArgs(args, perm, agent.PermPosture{Scoped: true, AllowedTools: []string{"Read", "Edit"}}, "gpt-5", "BRIEF")
	want = []string{"run", "-m", "gpt-5", "--allowed-tools", "Read,Edit", "BRIEF"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("scoped = %v, want %v", got, want)
	}

	// no permission table → {permission} vanishes
	got = RenderArgs([]string{"run", "{permission}", "{briefing}"}, Permission{}, agent.PermPosture{}, "", "B")
	want = []string{"run", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("no-perm = %v, want %v", got, want)
	}

	// {seat} is left literal (the runner substitutes it at exec time)
	got = RenderArgs([]string{"run", "--id", "{seat}", "{briefing}"}, Permission{}, agent.PermPosture{}, "", "B")
	want = []string{"run", "--id", "{seat}", "B"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("seat = %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agentmanifest/ -run RenderArgs`
Expected: FAIL — package/func undefined.

- [ ] **Step 3: Write minimal implementation** (`internal/agentmanifest/render.go`)

```go
// Package agentmanifest loads user-declared custom-agent manifests
// (~/.pactify/agents/*.toml) and maps them onto the agent registry (add-only).
package agentmanifest

import (
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
)

// Permission is the manifest's [runner.permission] block: the arg fragments
// {permission} expands to per posture. Scoped fragments may contain {tools}.
type Permission struct {
	Blanket []string `toml:"blanket"`
	Scoped  []string `toml:"scoped"`
}

// RenderArgs substitutes the manifest argv placeholders, producing the same kind
// of []string the built-in RunnerProfile.BuildArgs closures produce:
//   {briefing} → briefing   {model} → model (or dropped, see below)
//   {permission} → the blanket OR scoped fragment (spliced; {tools} joined)
//   {seat} → left literal (the orchestrate runner substitutes lc.Seat at exec)
// When model == "", {model} is dropped along with a directly-preceding lone
// -m/--model flag (so a cleared/default model runs without an empty -m).
func RenderArgs(tmpl []string, perm Permission, posture agent.PermPosture, model, briefing string) []string {
	out := make([]string, 0, len(tmpl)+len(perm.Blanket)+len(perm.Scoped))
	for _, tok := range tmpl {
		switch tok {
		case "{briefing}":
			out = append(out, briefing)
		case "{model}":
			if model == "" {
				if n := len(out); n > 0 && (out[n-1] == "-m" || out[n-1] == "--model") {
					out = out[:n-1]
				}
				continue
			}
			out = append(out, model)
		case "{permission}":
			frag := perm.Blanket
			if posture.Scoped {
				frag = perm.Scoped
			}
			tools := strings.Join(posture.AllowedTools, ",")
			for _, f := range frag {
				out = append(out, strings.ReplaceAll(f, "{tools}", tools))
			}
		default:
			out = append(out, tok)
		}
	}
	return out
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agentmanifest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agentmanifest/render.go internal/agentmanifest/render_test.go
git commit -m "feat(agentmanifest): RenderArgs placeholder template (model drop-rule, permission, tools, seat)"
```

---

### Task A4: `Manifest` parse + `Validate`

**Files:**
- Create: `internal/agentmanifest/manifest.go`
- Test: `internal/agentmanifest/manifest_test.go`

- [ ] **Step 1: Write the failing test** (`internal/agentmanifest/manifest_test.go`)

```go
package agentmanifest

import (
	"strings"
	"testing"
)

const validTOML = `
kind = "myagent"
binary = "myagent"
entry = "AGENTS.md"

[mcp]
config_path = ".myagent/mcp.json"
scope = "project"
format = "mcpServers"

[runner]
args = ["run", "-m", "{model}", "{permission}", "{briefing}"]
default_model = "m1"
models = ["m1", "m2"]

[runner.permission]
blanket = ["--yolo"]
`

func TestParseAndValidateOK(t *testing.T) {
	m, err := Parse([]byte(validTOML))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if errs := Validate(m); len(errs) != 0 {
		t.Fatalf("validate errors: %v", errs)
	}
	if m.Kind != "myagent" || m.Runner.DefaultModel != "m1" || len(m.Runner.Models) != 2 {
		t.Fatalf("bad manifest: %+v", m)
	}
}

func TestParseRejectsUnknownKey(t *testing.T) {
	if _, err := Parse([]byte("kind=\"x\"\nbinary=\"x\"\nbogus=1\n")); err == nil {
		t.Fatal("unknown key must error (strict decode)")
	}
}

func TestValidateViolations(t *testing.T) {
	cases := []struct{ name, toml, wantSub string }{
		{"no kind", `binary="x"`, "kind"},
		{"bad kind chars", "kind=\"My_Agent\"\nbinary=\"x\"", "kind"},
		{"builtin kind", "kind=\"opencode\"\nbinary=\"x\"", "built-in"},
		{"no binary", `kind="x"`, "binary"},
		{"entry traversal", "kind=\"x\"\nbinary=\"x\"\nentry=\"../e\"", "entry"},
		{"bad format", "kind=\"x\"\nbinary=\"x\"\n[mcp]\nconfig_path=\"a\"\nscope=\"project\"\nformat=\"bad\"", "format"},
		{"bad scope", "kind=\"x\"\nbinary=\"x\"\n[mcp]\nconfig_path=\"a\"\nscope=\"bad\"\nformat=\"toml\"", "scope"},
		{"runner no briefing", "kind=\"x\"\nbinary=\"x\"\n[runner]\nargs=[\"run\"]", "briefing"},
		{"arg-identity needs seat", "kind=\"x\"\nbinary=\"x\"\n[identity]\nvia=\"arg\"\n[runner]\nargs=[\"run\",\"{briefing}\"]", "seat"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := Parse([]byte(c.toml))
			if err != nil { // some parse fine but fail validate
				if !strings.Contains(err.Error(), c.wantSub) {
					t.Fatalf("parse err %v, want sub %q", err, c.wantSub)
				}
				return
			}
			errs := Validate(m)
			joined := strings.Join(errs, "; ")
			if !strings.Contains(joined, c.wantSub) {
				t.Fatalf("validate errs %q, want sub %q", joined, c.wantSub)
			}
		})
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agentmanifest/ -run "Parse|Validate"`
Expected: FAIL — undefined Parse/Validate/Manifest.

- [ ] **Step 3: Write minimal implementation** (`internal/agentmanifest/manifest.go`)

```go
package agentmanifest

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/agentjoey/pactify/internal/agent"
	toml "github.com/pelletier/go-toml/v2"
)

// Manifest is a parsed custom-agent TOML manifest.
type Manifest struct {
	Kind     string `toml:"kind"`
	Binary   string `toml:"binary"`
	Entry    string `toml:"entry"`
	Identity struct {
		Via string `toml:"via"` // "" | "env" | "arg"
	} `toml:"identity"`
	MCP *struct {
		ConfigPath string `toml:"config_path"`
		Scope      string `toml:"scope"`
		Format     string `toml:"format"`
	} `toml:"mcp"`
	Runner *struct {
		Args         []string   `toml:"args"`
		DefaultModel string     `toml:"default_model"`
		Models       []string   `toml:"models"`
		Permission   Permission `toml:"permission"`
	} `toml:"runner"`
}

// Parse decodes manifest TOML with strict (unknown-key-rejecting) decoding.
func Parse(b []byte) (Manifest, error) {
	var m Manifest
	dec := toml.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return Manifest{}, fmt.Errorf("manifest TOML: %w", err)
	}
	return m, nil
}

var kindRe = regexp.MustCompile(`^[a-z0-9-]+$`)

func isBuiltinKind(kind string) bool {
	for _, k := range agent.Kinds() {
		if k == kind {
			// A kind is "built-in" only if it has no user-manifest origin. Since
			// RegisterExternal adds custom kinds to the same registry, treat any
			// kind present at validate time that we did NOT just load as built-in.
			// Simpler + safe: the built-in set is fixed; compare against it.
			return builtinSet[kind]
		}
	}
	return false
}

// builtinSet is the fixed set of compiled-in kinds (manifests may not shadow them).
var builtinSet = func() map[string]bool {
	m := map[string]bool{}
	for _, k := range agent.Kinds() {
		m[k] = true
	}
	return m
}()

// Validate returns all rule violations (empty = valid).
func Validate(m Manifest) []string {
	var errs []string
	add := func(s string) { errs = append(errs, s) }

	switch {
	case m.Kind == "":
		add("kind: required")
	case !kindRe.MatchString(m.Kind):
		add("kind: must match [a-z0-9-]+")
	case builtinSet[m.Kind]:
		add(fmt.Sprintf("kind %q: collides with a built-in kind (add-only)", m.Kind))
	}
	if m.Binary == "" {
		add("binary: required")
	}
	if strings.ContainsAny(m.Entry, "/") || strings.Contains(m.Entry, "..") {
		add("entry: must be a bare filename (no / or ..)")
	}
	if m.MCP != nil {
		if _, ok := agent.ParseFormat(m.MCP.Format); !ok {
			add("mcp.format: must be one of mcpServers|opencode|toml")
		}
		if _, ok := agent.ParseScope(m.MCP.Scope); !ok {
			add("mcp.scope: must be project|global")
		}
		if m.MCP.ConfigPath == "" {
			add("mcp.config_path: required when [mcp] is present")
		}
	}
	if m.Runner != nil {
		if n := count(m.Runner.Args, "{briefing}"); n != 1 {
			add("runner.args: must contain exactly one {briefing}")
		}
		if m.Identity.Via == "arg" && count(m.Runner.Args, "{seat}") == 0 {
			add("runner.args: must contain {seat} when identity.via=arg")
		}
	} else if m.Identity.Via == "arg" {
		add("identity.via=arg requires a [runner] with {seat}")
	}
	return errs
}

func count(xs []string, want string) int {
	n := 0
	for _, x := range xs {
		if x == want {
			n++
		}
	}
	return n
}
```

> Note: the `isBuiltinKind` helper above is illustrative; `Validate` uses
> `builtinSet` directly. Drop `isBuiltinKind` if unused (keep the file lint-clean).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agentmanifest/`
Expected: PASS. (Remove the unused `isBuiltinKind` if `go vet`/build complains.)

- [ ] **Step 5: Commit**

```bash
git add internal/agentmanifest/manifest.go internal/agentmanifest/manifest_test.go
git commit -m "feat(agentmanifest): Manifest parse (strict) + Validate (all violations)"
```

---

### Task A5: `toExternal` + `Load` + `LoadAndRegister`

**Files:**
- Modify: `internal/agentmanifest/manifest.go`
- Test: `internal/agentmanifest/manifest_test.go`

- [ ] **Step 1: Write the failing test** (append)

```go
import "os"
import "path/filepath"

func TestLoadAndRegisterFromHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	dir := filepath.Join(home, ".pactify", "agents")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "myagent.toml"), []byte(validTOML), 0o644)
	// a bogus manifest → reported as a warning, not fatal, and not registered.
	os.WriteFile(filepath.Join(dir, "bad.toml"), []byte("kind=\"opencode\"\nbinary=\"x\"\n"), 0o644)

	t.Cleanup(func() { delete(registryRef(), "myagent") })
	warns := LoadAndRegister()
	if len(warns) == 0 {
		t.Fatal("expected a warning for the built-in-colliding manifest")
	}
	if _, ok := agent.Get("myagent"); !ok {
		t.Fatal("myagent should be registered")
	}
	rp, ok := agent.RunnerProfileFor("myagent")
	if !ok {
		t.Fatal("myagent runner profile missing")
	}
	// the rendered args use RenderArgs (placeholder template).
	got := rp.BuildArgs("m1", agent.PermPosture{}, "{briefing}")
	want := []string{"run", "-m", "m1", "--yolo", "{briefing}"}
	if !equal(got, want) {
		t.Fatalf("BuildArgs = %v, want %v", got, want)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

> `registryRef()` is a tiny test helper exported from the agent package for
> cleanup; if you prefer not to export it, instead delete via a fresh
> `agent.RegisterExternal` guard or skip the cleanup (t.TempDir + PACTIFY_HOME make
> the load hermetic, but the agent registry is process-global — so DO clean up).
> Add to `internal/agent/agent.go`: `func UnregisterExternal(kind string) { if !builtinKinds[kind] { delete(registry, kind); delete(runnerProfiles, kind) } }` and call `agent.UnregisterExternal("myagent")` in cleanup instead of `registryRef()`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agentmanifest/ -run LoadAndRegister`
Expected: FAIL — undefined Load/LoadAndRegister/toExternal.

- [ ] **Step 3: Write minimal implementation**

Add `agent.UnregisterExternal` (in `internal/agent/agent.go`):
```go
// builtinKinds is the fixed compiled-in set (guards UnregisterExternal so it can
// never drop a built-in). Captured before any RegisterExternal runs.
var builtinKinds = func() map[string]bool {
	m := map[string]bool{}
	for k := range registry {
		m[k] = true
	}
	return m
}()

// UnregisterExternal removes a custom kind (no-op for built-ins). For test cleanup
// and `agent manifest remove`.
func UnregisterExternal(kind string) {
	if builtinKinds[kind] {
		return
	}
	delete(registry, kind)
	delete(runnerProfiles, kind)
}
```

Add to `internal/agentmanifest/manifest.go`:
```go
import (
	"os"
	"path/filepath"
)

// home resolves ~/.pactify honoring PACTIFY_HOME (mirrors internal/registry).
func agentsDir() (string, error) {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, ".pactify", "agents"), nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".pactify", "agents"), nil
}

// toExternal maps a validated manifest onto the agent.External DTO.
func (m Manifest) toExternal() agent.External {
	e := agent.External{Kind: m.Kind, Entry: m.Entry, Binary: m.Binary}
	if m.MCP != nil {
		e.HasMCP = true
		e.MCPConfigPath = m.MCP.ConfigPath
		e.MCPScope, _ = agent.ParseScope(m.MCP.Scope)
		e.MCPFormat, _ = agent.ParseFormat(m.MCP.Format)
	}
	if m.Runner != nil {
		r := *m.Runner
		rp := agent.RunnerProfile{
			Command: m.Binary, DefaultModel: r.DefaultModel, Models: r.Models,
			BuildArgs: func(model string, posture agent.PermPosture, briefing string) []string {
				return RenderArgs(r.Args, r.Permission, posture, model, briefing)
			},
		}
		e.Runner = &rp
	}
	return e
}

// Load reads every ~/.pactify/agents/*.toml, parses + validates each, returning
// the valid manifests and a warning per skipped (unreadable/invalid) file.
func Load() (manifests []Manifest, warnings []string) {
	dir, err := agentsDir()
	if err != nil {
		return nil, []string{"agentmanifest: " + err.Error()}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil // no manifests dir → nothing to load (not an error)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".toml") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", ent.Name(), err))
			continue
		}
		m, err := Parse(b)
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", ent.Name(), err))
			continue
		}
		if errs := Validate(m); len(errs) != 0 {
			warnings = append(warnings, fmt.Sprintf("%s: %s", ent.Name(), strings.Join(errs, "; ")))
			continue
		}
		manifests = append(manifests, m)
	}
	return manifests, warnings
}

// LoadAndRegister loads valid manifests and registers them (add-only), returning
// all warnings (invalid files + registration collisions). Never fatal.
func LoadAndRegister() []string {
	manifests, warnings := Load()
	for _, m := range manifests {
		if err := agent.RegisterExternal(m.toExternal()); err != nil {
			warnings = append(warnings, err.Error())
		}
	}
	return warnings
}
```

Replace the test's `registryRef()` cleanup with `agent.UnregisterExternal("myagent")`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/agentmanifest/ ./internal/agent/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/agentmanifest/manifest.go internal/agentmanifest/manifest_test.go internal/agent/agent.go
git commit -m "feat(agentmanifest): Load + toExternal + LoadAndRegister; agent.UnregisterExternal"
```

---

### Task A6: substitute `{seat}` at exec + wire `LoadAndRegister` into `main`

**Files:**
- Modify: `internal/orchestrate/runner.go`, `cmd/pactify/main.go`
- Test: `internal/orchestrate/runner_test.go`

- [ ] **Step 1: Write the failing test** (append to `runner_test.go`)

```go
func TestRunnerSubstitutesSeatToken(t *testing.T) {
	// A custom kind whose argv carries {seat} → the runner replaces it with lc.Seat.
	rp := agent.RunnerProfile{Command: "myagent", DefaultModel: "m1",
		BuildArgs: func(model string, _ agent.PermPosture, briefing string) []string {
			return []string{"run", "--id", "{seat}", briefing}
		}}
	if err := agent.RegisterExternal(agent.External{Kind: "seatkind", Binary: "myagent", Runner: &rp}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { agent.UnregisterExternal("seatkind") })

	var cap runCapture
	r := CmdRunner{Exec: fakeRunExec(&cap, nil)}
	err := r.Run(context.Background(), LaunchContext{Seat: "dev", Kind: "seatkind", Briefing: "B", RepoDir: "/r"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if argsHave(cap.args, "{seat}") || !argsHave(cap.args, "dev") {
		t.Fatalf("args = %v, want {seat}→dev", cap.args)
	}
}
```

(Add `"github.com/agentjoey/pactify/internal/agent"` to runner_test.go imports.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/orchestrate/ -run SubstitutesSeat`
Expected: FAIL — `{seat}` survives in args.

- [ ] **Step 3: Write minimal implementation**

In `internal/orchestrate/runner.go`, in `CmdRunner.Run`, right after the args are
built from `eff.Args` (the placeholder-substitution loop) and before/after the
opencode tagging, add a `{seat}` pass:

```go
	for i, a := range args {
		if a == "{seat}" {
			args[i] = lc.Seat
		}
	}
```

(Place it next to the existing briefing-placeholder substitution loop.)

In `cmd/pactify/main.go`, before `newRootCmd().Execute()`:

```go
	for _, w := range agentmanifest.LoadAndRegister() {
		fmt.Fprintln(os.Stderr, "pactify: custom agent manifest skipped — "+w)
	}
```

Add imports `"github.com/agentjoey/pactify/internal/agentmanifest"` (and ensure `fmt`/`os` present).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/orchestrate/ && go build ./...`
Expected: PASS + clean build.

- [ ] **Step 5: Commit**

```bash
git add internal/orchestrate/runner.go internal/orchestrate/runner_test.go cmd/pactify/main.go
git commit -m "feat(orchestrate): substitute {seat} at exec; load custom-agent manifests at startup"
```

---

### Task A7: `agent manifest list/validate/show` CLI

**Files:**
- Create: `cmd/pactify/cmd_agent_manifest.go`
- Modify: `cmd/pactify/cmd_agent.go` (register `newAgentManifestCmd()`)
- Test: `cmd/pactify/cmd_agent_manifest_test.go`

- [ ] **Step 1: Write the failing test**

```go
package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentManifestValidate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "m.toml")
	os.WriteFile(f, []byte("kind=\"myx\"\nbinary=\"myx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n"), 0o644)
	out := runManifest(t, "validate", f)
	if !contains(out, "OK") {
		t.Fatalf("validate should print OK: %s", out)
	}

	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("binary=\"x\"\n"), 0o644)
	cmd := newAgentManifestCmd()
	cmd.SetArgs([]string{"validate", bad})
	cmd.SetOut(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("validate of an invalid manifest must error")
	}
}

func runManifest(t *testing.T, args ...string) string {
	t.Helper()
	cmd := newAgentManifestCmd()
	var b bytes.Buffer
	cmd.SetOut(&b)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("manifest %v: %v", args, err)
	}
	return b.String()
}
```

(`contains` already exists in `cmd_audit_test.go`.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./cmd/pactify/ -run AgentManifestValidate`
Expected: FAIL — undefined newAgentManifestCmd.

- [ ] **Step 3: Write minimal implementation** (`cmd/pactify/cmd_agent_manifest.go`)

```go
package main

import (
	"fmt"
	"os"

	"github.com/agentjoey/pactify/internal/agent"
	"github.com/agentjoey/pactify/internal/agentmanifest"
	"github.com/spf13/cobra"
)

func newAgentManifestCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "manifest", Short: "manage custom-agent manifests (~/.pactify/agents/*.toml)"}
	cmd.AddCommand(newManifestValidateCmd(), newManifestListCmd(), newManifestShowCmd())
	return cmd
}

func newManifestValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <file.toml>",
		Short: "parse + validate a manifest file",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			b, err := os.ReadFile(a[0])
			if err != nil {
				return err
			}
			m, err := agentmanifest.Parse(b)
			if err != nil {
				return err
			}
			if errs := agentmanifest.Validate(m); len(errs) != 0 {
				for _, e := range errs {
					fmt.Fprintln(c.OutOrStdout(), "  ✗ "+e)
				}
				return fmt.Errorf("manifest invalid (%d issue(s))", len(errs))
			}
			fmt.Fprintf(c.OutOrStdout(), "OK — %s (%s)\n", m.Kind, m.Binary)
			return nil
		},
	}
}

func newManifestListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list all kinds (built-in + custom)",
		RunE: func(c *cobra.Command, _ []string) error {
			ms, _ := agentmanifest.Load()
			custom := map[string]bool{}
			for _, m := range ms {
				custom[m.Kind] = true
			}
			for _, k := range agent.Kinds() {
				src := "built-in"
				if custom[k] {
					src = "custom"
				}
				fmt.Fprintf(c.OutOrStdout(), "%-16s %s\n", k, src)
			}
			return nil
		},
	}
}

func newManifestShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <kind>",
		Short: "print a custom manifest's TOML",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, a []string) error {
			p, err := agentmanifest.PathFor(a[0])
			if err != nil {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("no custom manifest for %q", a[0])
			}
			fmt.Fprint(c.OutOrStdout(), string(b))
			return nil
		},
	}
}
```

Add `PathFor` to `internal/agentmanifest/manifest.go`:
```go
// PathFor returns the manifest file path for a kind (~/.pactify/agents/<kind>.toml).
func PathFor(kind string) (string, error) {
	dir, err := agentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, kind+".toml"), nil
}
```

Register in `cmd/pactify/cmd_agent.go`: add `newAgentManifestCmd()` to the
`a.AddCommand(...)` list (line ~220).

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./cmd/pactify/ -run AgentManifest`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add cmd/pactify/cmd_agent_manifest.go cmd/pactify/cmd_agent_manifest_test.go internal/agentmanifest/manifest.go cmd/pactify/cmd_agent.go
git commit -m "feat(cli): agent manifest list/validate/show"
```

---

### Task A8: Phase-A gate

- [ ] **Step 1:** Run `go build ./... && go vet ./... && go test ./...` — Expected: all green.
- [ ] **Step 2:** Manual smoke:
```bash
mkdir -p ~/.pactify/agents && cat > ~/.pactify/agents/echoagent.toml <<'EOF'
kind = "echoagent"
binary = "echo"
[runner]
args = ["{briefing}"]
EOF
pactify agent manifest validate ~/.pactify/agents/echoagent.toml   # OK
pactify agent manifest list | grep echoagent                       # echoagent custom
pactify agent scan | grep echoagent                                # detected (echo on PATH) + drivable
rm ~/.pactify/agents/echoagent.toml
```
- [ ] **Step 3: Commit** (if any fixups): `git commit -am "chore: phase-A custom-agent manifest core green"`

---

## Phase B — `agent manifest add/remove`

### Task B1: `add` + `remove`

**Files:** `cmd/pactify/cmd_agent_manifest.go`, `internal/agentmanifest/manifest.go`, `cmd/pactify/cmd_agent_manifest_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestAgentManifestAddRemove(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	src := filepath.Join(t.TempDir(), "m.toml")
	os.WriteFile(src, []byte("kind=\"myx\"\nbinary=\"myx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n"), 0o644)

	runManifest(t, "add", src)
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "myx.toml")); err != nil {
		t.Fatalf("manifest not installed: %v", err)
	}
	// add of a built-in-colliding manifest must error.
	bad := filepath.Join(t.TempDir(), "bad.toml")
	os.WriteFile(bad, []byte("kind=\"opencode\"\nbinary=\"x\"\n"), 0o644)
	cmd := newAgentManifestCmd()
	cmd.SetArgs([]string{"add", bad})
	cmd.SetOut(&bytes.Buffer{})
	if cmd.Execute() == nil {
		t.Fatal("add of a built-in kind must error")
	}
	// remove deletes it.
	runManifest(t, "remove", "myx")
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "myx.toml")); !os.IsNotExist(err) {
		t.Fatal("manifest not removed")
	}
}
```

- [ ] **Step 2: Run** `go test ./cmd/pactify/ -run AgentManifestAddRemove` → FAIL (undefined add/remove).

- [ ] **Step 3: Implement** — add `Install(b []byte) (kind string, err error)` to agentmanifest (parse+validate, write to `PathFor(kind)`), and `newManifestAddCmd`/`newManifestRemoveCmd`:

```go
// Install validates a manifest and writes it to ~/.pactify/agents/<kind>.toml.
func Install(b []byte) (string, error) {
	m, err := Parse(b)
	if err != nil {
		return "", err
	}
	if errs := Validate(m); len(errs) != 0 {
		return "", fmt.Errorf("invalid manifest: %s", strings.Join(errs, "; "))
	}
	p, err := PathFor(m.Kind)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}
	return m.Kind, os.WriteFile(p, b, 0o644)
}

// Remove deletes a custom manifest by kind.
func Remove(kind string) error {
	if builtinSet[kind] {
		return fmt.Errorf("%q is a built-in kind", kind)
	}
	p, err := PathFor(kind)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no custom manifest for %q", kind)
		}
		return err
	}
	return nil
}
```
CLI `add <file>` reads the file → `Install` → prints "installed <kind>"; `remove <kind>` → `Remove`. Register both in `newAgentManifestCmd`.

- [ ] **Step 4: Run** `go test ./cmd/pactify/ ./internal/agentmanifest/` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat(cli): agent manifest add/remove (+ agentmanifest Install/Remove)"`

---

## Phase C — serve endpoints

### Task C1: GET/POST/DELETE `/api/agents/manifests`

**Files:** `internal/serve/manifests.go`, `internal/serve/manifests_test.go`, wire `registerManifestRoutes` in `internal/serve/api.go`.

- [ ] **Step 1: Write the failing test**

```go
package serve

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestManifestsCRUD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	s := &Server{}

	// POST valid
	body := []byte("kind=\"webx\"\nbinary=\"webx\"\n[runner]\nargs=[\"run\",\"{briefing}\"]\n")
	r := httptest.NewRequest("POST", "/api/agents/manifests", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleManifestCreate(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("POST = %d (%s)", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(home, ".pactify", "agents", "webx.toml")); err != nil {
		t.Fatalf("not written: %v", err)
	}

	// POST invalid → 422
	r2 := httptest.NewRequest("POST", "/api/agents/manifests", bytes.NewReader([]byte("binary=\"x\"\n")))
	w2 := httptest.NewRecorder()
	s.handleManifestCreate(w2, r2)
	if w2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid POST = %d, want 422", w2.Code)
	}

	// GET list
	r3 := httptest.NewRequest("GET", "/api/agents/manifests", nil)
	w3 := httptest.NewRecorder()
	s.handleManifestList(w3, r3)
	var got []map[string]any
	json.NewDecoder(w3.Body).Decode(&got)
	if len(got) != 1 || got[0]["kind"] != "webx" {
		t.Fatalf("list = %v", got)
	}

	// DELETE
	r4 := httptest.NewRequest("DELETE", "/api/agents/manifests/webx", nil)
	r4.SetPathValue("kind", "webx")
	w4 := httptest.NewRecorder()
	s.handleManifestDelete(w4, r4)
	if w4.Code != http.StatusOK {
		t.Fatalf("DELETE = %d", w4.Code)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/serve/ -run ManifestsCRUD` → FAIL.

- [ ] **Step 3: Implement** (`internal/serve/manifests.go`):

```go
package serve

import (
	"io"
	"net/http"

	"github.com/agentjoey/pactify/internal/agentmanifest"
)

func (s *Server) registerManifestRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/agents/manifests", s.handleManifestList)
	mux.HandleFunc("POST /api/agents/manifests", s.handleManifestCreate)
	mux.HandleFunc("DELETE /api/agents/manifests/{kind}", s.handleManifestDelete)
}

func (s *Server) handleManifestList(w http.ResponseWriter, _ *http.Request) {
	ms, warns := agentmanifest.Load()
	type item struct {
		Kind    string   `json:"kind"`
		Binary  string   `json:"binary"`
		Drivable bool    `json:"drivable"`
	}
	out := make([]item, 0, len(ms))
	for _, m := range ms {
		out = append(out, item{Kind: m.Kind, Binary: m.Binary, Drivable: m.Runner != nil})
	}
	_ = warns // (optional: surface as a header)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleManifestCreate(w http.ResponseWriter, r *http.Request) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	kind, err := agentmanifest.Install(b)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"kind": kind})
}

func (s *Server) handleManifestDelete(w http.ResponseWriter, r *http.Request) {
	if err := agentmanifest.Remove(r.PathValue("kind")); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"removed": r.PathValue("kind")})
}
```
Wire `s.registerManifestRoutes(mux)` next to `registerAuditRoutes` in api.go.

- [ ] **Step 4: Run** `go test ./internal/serve/ -run Manifest` → PASS.
- [ ] **Step 5: Commit** `git commit -am "feat(serve): GET/POST/DELETE /api/agents/manifests"`

---

## Phase D — Settings "Add custom agent" form

### Task D1: api.ts + CustomAgentForm + mount

**Files:** `web/src/lib/api.ts`, `web/src/components/ops/CustomAgentForm.tsx` (+ test), `web/src/components/ops/OpsView.tsx`.

- [ ] **Step 1: Write the failing test** (`CustomAgentForm.test.tsx`)

```tsx
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, it, expect, vi } from "vitest";

const createManifest = vi.fn();
vi.mock("../../lib/api", () => ({ createManifest: (...a: unknown[]) => createManifest(...a) }));

import { CustomAgentForm } from "./CustomAgentForm";

describe("CustomAgentForm", () => {
  it("posts the assembled TOML on submit", async () => {
    createManifest.mockResolvedValue({ kind: "myx" });
    render(<CustomAgentForm onCreated={() => {}} />);
    fireEvent.change(screen.getByTestId("ca-kind"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-binary"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-args"), { target: { value: "run,{briefing}" } });
    fireEvent.click(screen.getByRole("button", { name: /add custom agent/i }));
    await waitFor(() => expect(createManifest).toHaveBeenCalled());
    const toml = createManifest.mock.calls[0][0] as string;
    expect(toml).toContain('kind = "myx"');
    expect(toml).toContain('args = ["run", "{briefing}"]');
  });

  it("shows the server's field error", async () => {
    createManifest.mockRejectedValue(new Error("runner.args: must contain exactly one {briefing}"));
    render(<CustomAgentForm onCreated={() => {}} />);
    fireEvent.change(screen.getByTestId("ca-kind"), { target: { value: "myx" } });
    fireEvent.change(screen.getByTestId("ca-binary"), { target: { value: "myx" } });
    fireEvent.click(screen.getByRole("button", { name: /add custom agent/i }));
    await waitFor(() => expect(screen.getByText(/must contain exactly one/)).toBeTruthy());
  });
});
```

- [ ] **Step 2: Run** `cd web && npm test -- CustomAgentForm` → FAIL.

- [ ] **Step 3: Implement**

`web/src/lib/api.ts`:
```ts
export interface ManifestRow { kind: string; binary: string; drivable: boolean }
export const listManifests = () => getJSON<ManifestRow[]>("/api/agents/manifests");
export const createManifest = async (toml: string): Promise<{ kind: string }> => {
  const r = await fetch("/api/agents/manifests", { method: "POST", headers: { "Content-Type": "text/plain" }, body: toml });
  if (!r.ok) { let m = `${r.status}`; try { const j = await r.json() as {error?:string}; if (j.error) m = j.error; } catch {} throw new Error(m); }
  return r.json() as Promise<{ kind: string }>;
};
export const deleteManifest = (kind: string) =>
  fetch(`/api/agents/manifests/${encodeURIComponent(kind)}`, { method: "DELETE" });
```

`web/src/components/ops/CustomAgentForm.tsx`: a small form (kind, binary, entry,
args CSV, default_model, models CSV, mcp path/scope/format) that assembles a TOML
string and calls `createManifest`. On error, render the message; on success call
`onCreated`. `data-testid`: `ca-kind`/`ca-binary`/`ca-args`. The args CSV
`"run,{briefing}"` → `args = ["run", "{briefing}"]` (split on comma, trim, quote).
Mount it in `OpsView.tsx` under a collapsible "Add custom agent" beside AgentRoster.

- [ ] **Step 4: Run** `cd web && npm test -- CustomAgentForm` → PASS, then full gate
`cd web && npm test && npm run e2e` → double-green; `npx tsc --noEmit` clean.

- [ ] **Step 5: Commit** `git commit -am "feat(web): Settings 'Add custom agent' form (manifest CRUD)"`

---

## Self-Review

**Spec coverage:** §2 schema → A4 (Manifest struct/Parse); §3 RenderArgs → A3;
§4 loader/merge/RegisterExternal add-only → A2/A5; §5 CLI → A7/B1, serve → C1, UI →
D1; §6 validation → A4; §9 internal mapping → A2/A5 (toExternal); §10 testing → each
task is TDD; §11 verify items → A6 (main+serve share main()), A8/D smoke. Identity
`{seat}` → A6. **Gap check:** §7 security (path traversal) → A4 entry check + kindRe;
add-only guard → A2/A4. No spec section without a task. P2 (`[session]`/`[audit]`,
project-level) explicitly out of scope.

**Placeholder scan:** none. The A4 note flags removing the illustrative
`isBuiltinKind` helper to stay lint-clean (not a TODO — a written instruction).

**Type consistency:** `agent.External{Kind,Entry,Binary,HasMCP,MCPConfigPath,
MCPScope,MCPFormat,Desktop,Runner}` defined A2, consumed A5 `toExternal`. `Permission
{Blanket,Scoped}` defined A3, used in `Manifest.Runner.Permission` A4 + `RenderArgs`
A3. `RenderArgs(tmpl, Permission, agent.PermPosture, model, briefing)` consistent
A3↔A5. `agentmanifest.{Parse,Validate,Load,LoadAndRegister,Install,Remove,PathFor}`
consistent across A4/A5/A7/B1/C1. `agent.{RegisterExternal,UnregisterExternal,
ParseFormat,ParseScope}` consistent A2/A5/A6. CLI `newAgentManifestCmd` A7 extended
B1. serve `handleManifest{List,Create,Delete}` C1.
