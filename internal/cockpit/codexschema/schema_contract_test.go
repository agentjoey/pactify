package codexschema

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

const (
	// vendoredSchema is the checked-in copy of the codex app-server schema.
	vendoredSchema = "codex_app_server_protocol.schemas.json"

	// versionFile records the codex CLI version the vendored schema came from.
	versionFile = "CODEX_VERSION"

	// schemaPathEnv points the contract test at a different schema file. CI
	// sets it to a freshly generated schema so a NEW codex release can be
	// contract-checked before anyone vendors it.
	schemaPathEnv = "PACTIFY_CODEX_SCHEMA"

	// requireCodexEnv turns "codex not on PATH" from a skip into a failure.
	// Set it in any CI job that installs codex on purpose, so a broken install
	// cannot make the drift check pass silently.
	requireCodexEnv = "PACTIFY_REQUIRE_CODEX"
)

// regenHint is emitted once alongside any failure in this package. A red here
// is never self-explanatory, so it carries the fix inline.
const regenHint = `
To resolve:
  1. codex app-server generate-json-schema --out <tmp>
  2. cp <tmp>/` + vendoredSchema + ` internal/cockpit/codexschema/
  3. write the output of 'codex --version' into internal/cockpit/codexschema/` + versionFile + `
  4. RE-VERIFY internal/cockpit/codex.go against the new schema: the methods it
     calls, the notification methods it switches on, the JSON field paths it
     reads (jsonStringAt / parseUsageFromParams), and the approval reply shape.
     The vendored schema exists ONLY to make step 4 possible; skipping it
     defeats the purpose of this test.`

// TestSchemaContract pins the parts of the codex app-server protocol that
// internal/cockpit/codex.go actually depends on.
//
// Unlike TestSchemaDrift it needs no codex binary: it asserts against the
// vendored schema (or, when schemaPathEnv is set, against a freshly generated
// one). That makes it the check that runs everywhere, including CI, and it
// gives the vendored schema a real Go consumer so it can no longer rot
// unnoticed.
//
// Scope note: the schema is the source of truth here, codex.go is not. Every
// assertion below names the exact thing codex.go reads or sends, so that a
// protocol change which invalidates a mapping fails HERE — loudly and with a
// regeneration hint — rather than silently degrading the cockpit at runtime.
//
// History: an earlier revision of this file documented seven mappings that
// codex.go got wrong from the day they were written (a plain-string `delta`
// read as delta.text, a "mcpTool" variant that does not exist, flat token
// counters that are actually nested under tokenUsage.last/total, a "turn/failed"
// method that is not in the protocol, thread/resume and turn/interrupt sent as
// notifications when both are ClientRequests, and permissions approvals
// answered with {"decision"}). Those were fixed in the CODEX-MAP change and the
// assertions below now guard the corrected mappings.
func TestSchemaContract(t *testing.T) {
	doc := loadSchemaDoc(t)
	t.Cleanup(func() {
		if t.Failed() {
			t.Log(regenHint)
		}
	})

	t.Run("methods", func(t *testing.T) {
		methods := doc.methods()
		// Every method internal/cockpit/codex.go sends or switches on.
		want := []string{
			// client -> server (codexClient.initialize/threadStart/...)
			"initialize",
			"initialized",
			"thread/start",
			"thread/resume",
			"turn/start",
			"turn/interrupt",
			// server -> client requests (handleServerRequest)
			"item/commandExecution/requestApproval",
			"item/fileChange/requestApproval",
			"item/permissions/requestApproval",
			// server -> client notifications (handleNotification)
			"item/agentMessage/delta",
			"item/commandExecution/outputDelta",
			"item/completed",
			"item/started",
			"thread/tokenUsage/updated",
			"turn/started",
			"turn/completed",
			"turn/diff/updated",
			"turn/plan/updated",
			"error",
		}
		for _, m := range want {
			if !methods[m] {
				t.Errorf("codex.go uses method %q but it is absent from the schema", m)
			}
		}
		// "turn/failed" was handled by codex.go for a long time and has never
		// existed. If it ever appears, handleNotification should grow a real
		// case for it instead of the dead one that was removed.
		if methods["turn/failed"] {
			t.Errorf(`"turn/failed" now exists in the schema; codex.go treats turn/completed{status:"failed"} as the only failure path`)
		}
	})

	t.Run("client_requests_vs_notifications", func(t *testing.T) {
		// codexClient sends these with an id and waits for the response.
		// Demoting any of them back to rpc.notify() would hang or silently
		// no-op, which is exactly the bug CODEX-MAP fixed.
		requests := doc.methodsOf(t, "ClientRequest")
		for _, m := range []string{"initialize", "thread/start", "thread/resume", "turn/start", "turn/interrupt"} {
			if !requests[m] {
				t.Errorf("codex.go calls %q as a JSON-RPC request, but it is not a ClientRequest", m)
			}
		}
		// ...and this one is the single client notification.
		notifications := doc.methodsOf(t, "ClientNotification")
		if !notifications["initialized"] {
			t.Errorf(`codex.go sends "initialized" as a notification, but it is not a ClientNotification`)
		}
		if requests["initialized"] {
			t.Errorf(`"initialized" is now a ClientRequest; codex.go must wait for its response`)
		}
	})

	t.Run("thread_start_result", func(t *testing.T) {
		// codexClient.threadStart parses result.thread.id as a string.
		doc.requireStringField(t, "v2/ThreadStartResponse", "thread", "id")
	})

	t.Run("thread_resume", func(t *testing.T) {
		// threadResume sends {threadId} and reads result.thread.id back.
		doc.requireStringField(t, "v2/ThreadResumeParams", "threadId")
		doc.requireStringField(t, "v2/ThreadResumeResponse", "thread", "id")
	})

	t.Run("turn_start_and_interrupt", func(t *testing.T) {
		// turnStart records result.turn.id so that turnInterrupt can name the
		// turn: TurnInterruptParams requires BOTH ids.
		doc.requireStringField(t, "v2/TurnStartResponse", "turn", "id")
		doc.requireStringField(t, "v2/TurnInterruptParams", "threadId")
		doc.requireStringField(t, "v2/TurnInterruptParams", "turnId")
		doc.requireRequired(t, "v2/TurnInterruptParams", "threadId", "turnId")
	})

	t.Run("stream_deltas", func(t *testing.T) {
		// handleNotification reads the streaming text straight out of `delta`.
		// These are plain strings; reading delta.text yields "" forever.
		doc.requireStringField(t, "v2/AgentMessageDeltaNotification", "delta")
		doc.requireStringField(t, "v2/CommandExecutionOutputDeltaNotification", "delta")
	})

	t.Run("token_usage", func(t *testing.T) {
		// parseUsageFromParams is fed the params of thread/tokenUsage/updated
		// and reads tokenUsage.last, because every EventUsage consumer sums.
		for _, field := range []string{"inputTokens", "outputTokens", "totalTokens"} {
			doc.requireIntField(t, "v2/ThreadTokenUsageUpdatedNotification", "tokenUsage", "last", field)
		}
		// `total` is the running per-thread sum and is deliberately NOT read.
		// Asserted so the "why last, not total" reasoning stays checkable.
		for _, field := range []string{"inputTokens", "outputTokens", "totalTokens"} {
			doc.requireIntField(t, "v2/ThreadTokenUsageUpdatedNotification", "tokenUsage", "total", field)
		}
		// There is no cost anywhere in this notification, so Usage.CostUSD
		// stays 0 rather than being invented.
		if _, ok := doc.field(t, "v2/ThreadTokenUsage", "cost"); ok {
			t.Errorf("ThreadTokenUsage now carries a cost; parseUsageFromParams leaves Usage.CostUSD at 0")
		}
	})

	t.Run("turn_lifecycle", func(t *testing.T) {
		// turn/started gives handleNotification the turn id to interrupt with;
		// turn/completed is the ONLY terminal turn notification, and reports
		// failure through turn.status == "failed" + turn.error.message.
		doc.requireStringField(t, "v2/TurnStartedNotification", "turn", "id")
		doc.requireStringField(t, "v2/TurnCompletedNotification", "turn", "id")
		doc.requireStringField(t, "v2/TurnCompletedNotification", "turn", "error", "message")
		statuses := doc.enumValues(t, "v2/TurnStatus")
		for _, want := range []string{"completed", "failed", "interrupted"} {
			if !statuses[want] {
				t.Errorf("TurnStatus no longer has %q, which handleNotification branches on", want)
			}
		}
		// The separate `error` notification carries the same TurnError shape;
		// handleNotification de-duplicates the two.
		doc.requireStringField(t, "v2/ErrorNotification", "error", "message")
	})

	t.Run("thread_items", func(t *testing.T) {
		// item/started and item/completed carry a ThreadItem; codex.go
		// switches on item.type and then reads a display name off the item.
		// Each variant keeps that name in a different place.
		variants := doc.threadItemVariants(t)
		for itemType, fields := range map[string][]string{
			"agentMessage":     {"text"},
			"commandExecution": {"command"},
			"fileChange":       {"changes"},
			"mcpToolCall":      {"server", "tool"},
		} {
			props, ok := variants[itemType]
			if !ok {
				t.Errorf("ThreadItem has no %q variant; codex.go's item/started + item/completed switch depends on it", itemType)
				continue
			}
			for _, f := range fields {
				if !props[f] {
					t.Errorf("ThreadItem variant %q lost field %q, which codexThreadItem.displayName() reads", itemType, f)
				}
			}
		}
		// codex.go used to match "mcpTool", which is not a variant tag. Guard
		// against the typo coming back by name.
		if _, ok := variants["mcpTool"]; ok {
			t.Errorf(`ThreadItem now has an "mcpTool" variant; codex.go only handles "mcpToolCall"`)
		}
		// fileChange has no name of its own: displayName() falls back to the
		// path of the first FileUpdateChange.
		doc.requireStringField(t, "v2/FileUpdateChange", "path")
	})

	t.Run("approval_replies", func(t *testing.T) {
		// Command and file-change approvals are answered with {"decision": …}
		// and the values decisionString() emits must stay valid.
		for _, def := range []string{"CommandExecutionApprovalDecision", "FileChangeApprovalDecision"} {
			values := doc.enumValues(t, def)
			for _, want := range []string{"accept", "acceptForSession", "decline"} {
				if !values[want] {
					t.Errorf("%s no longer accepts %q, which decisionString() emits", def, want)
				}
			}
		}
		for _, def := range []string{"CommandExecutionRequestApprovalResponse", "FileChangeRequestApprovalResponse"} {
			doc.requireField(t, def, "decision")
		}

		// Permissions approvals are a DIFFERENT shape: no decision at all.
		// replyPermissionsApproval echoes the requested profile back under
		// `permissions` and picks a `scope`.
		if _, ok := doc.field(t, "PermissionsRequestApprovalResponse", "decision"); ok {
			t.Errorf("PermissionsRequestApprovalResponse now has a `decision`; replyPermissionsApproval sends {permissions, scope}")
		}
		doc.requireField(t, "PermissionsRequestApprovalResponse", "permissions")
		doc.requireField(t, "PermissionsRequestApprovalResponse", "scope")
		doc.requireRequired(t, "PermissionsRequestApprovalResponse", "permissions")
		scopes := doc.enumValues(t, "PermissionGrantScope")
		for _, want := range []string{"turn", "session"} {
			if !scopes[want] {
				t.Errorf("PermissionGrantScope no longer accepts %q, which replyPermissionsApproval sends", want)
			}
		}
		// The grant is the request echoed back, so the two profiles must stay
		// structurally interchangeable.
		doc.requireSameProperties(t, "v2/RequestPermissionProfile", "GrantedPermissionProfile")
		doc.requireField(t, "PermissionsRequestApprovalParams", "permissions")
	})
}

// TestSummarizeDiff covers the drift-failure summariser, which is otherwise
// only exercised on machines that have codex installed AND drifted.
func TestSummarizeDiff(t *testing.T) {
	vendored := map[string]any{"definitions": map[string]any{
		"Kept":    map[string]any{"type": "string"},
		"Gone":    map[string]any{"type": "string"},
		"Altered": map[string]any{"type": "string"},
		"v2":      map[string]any{"Inner": map[string]any{"type": "string"}},
	}}
	generated := map[string]any{"definitions": map[string]any{
		"Kept":    map[string]any{"type": "string"},
		"Fresh":   map[string]any{"type": "string"},
		"Altered": map[string]any{"type": "integer"},
		"v2":      map[string]any{"Inner": map[string]any{"type": "integer"}, "NewInner": map[string]any{}},
	}}

	got := strings.Join(summarizeDiff(vendored, generated), "\n")
	for _, want := range []string{
		"definitions added (1): Fresh",
		"definitions removed (1): Gone",
		"definitions.v2 added (1): NewInner",
		"definitions.v2 changed (1): Inner",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q; got:\n%s", want, got)
		}
	}
	// "Altered" and "v2" both changed at the top level.
	if !strings.Contains(got, "definitions changed (2): Altered, v2") {
		t.Errorf("summary missing top-level changed line; got:\n%s", got)
	}
}

// ---- schema helpers -------------------------------------------------------

type schemaDoc struct {
	path string
	root map[string]any
}

func schemaPath() string {
	if p := os.Getenv(schemaPathEnv); p != "" {
		return p
	}
	return vendoredSchema
}

func loadSchemaDoc(t *testing.T) *schemaDoc {
	t.Helper()
	path := schemaPath()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read schema %s: %v", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatalf("unmarshal schema %s: %v", path, err)
	}
	return &schemaDoc{path: path, root: root}
}

// deref follows "$ref": "#/definitions/..." one or more times, and collapses
// the single-payload wrappers this schema uses around a ref: `allOf: [X]` and
// the nullable `anyOf: [X, {"type":"null"}]`. Without the latter, an optional
// field such as Turn.error would look like it had no properties at all.
func (d *schemaDoc) deref(node any) any {
	for i := 0; i < 16; i++ {
		m, ok := node.(map[string]any)
		if !ok {
			return node
		}
		if ref, ok := m["$ref"].(string); ok {
			next, ok := d.lookup(strings.TrimPrefix(ref, "#/"))
			if !ok {
				return node
			}
			node = next
			continue
		}
		if inner, ok := soleNonNullBranch(m); ok {
			node = inner
			continue
		}
		return node
	}
	return node
}

// soleNonNullBranch returns the only non-null branch of an anyOf/allOf/oneOf
// wrapper, if there is exactly one.
func soleNonNullBranch(m map[string]any) (any, bool) {
	for _, key := range []string{"anyOf", "allOf", "oneOf"} {
		branches, ok := m[key].([]any)
		if !ok {
			continue
		}
		var kept []any
		for _, b := range branches {
			if bm, ok := b.(map[string]any); ok && hasType(bm, "null") {
				continue
			}
			kept = append(kept, b)
		}
		if len(kept) == 1 {
			return kept[0], true
		}
	}
	return nil, false
}

// lookup resolves a slash-separated pointer against the document root.
func (d *schemaDoc) lookup(pointer string) (any, bool) {
	var cur any = d.root
	for _, seg := range strings.Split(pointer, "/") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		cur, ok = m[seg]
		if !ok {
			return nil, false
		}
	}
	return cur, true
}

// definition returns definitions/<name>, where name may be "v2/Foo".
func (d *schemaDoc) definition(t *testing.T, name string) map[string]any {
	t.Helper()
	node, ok := d.lookup("definitions/" + name)
	if !ok {
		t.Fatalf("schema %s has no definition %q", d.path, name)
	}
	m, ok := d.deref(node).(map[string]any)
	if !ok {
		t.Fatalf("definition %q is not an object", name)
	}
	return m
}

// field walks properties, dereferencing $ref at every hop — mirroring how
// codex.go's jsonStringAt walks the decoded JSON object.
func (d *schemaDoc) field(t *testing.T, def string, path ...string) (map[string]any, bool) {
	t.Helper()
	cur := d.definition(t, def)
	for i, key := range path {
		props, ok := cur["properties"].(map[string]any)
		if !ok {
			t.Logf("%s.%s: no properties at hop %d", def, strings.Join(path, "."), i)
			return nil, false
		}
		next, ok := props[key]
		if !ok {
			return nil, false
		}
		m, ok := d.deref(next).(map[string]any)
		if !ok {
			return nil, false
		}
		cur = m
	}
	return cur, true
}

func (d *schemaDoc) requireField(t *testing.T, def string, path ...string) map[string]any {
	t.Helper()
	node, ok := d.field(t, def, path...)
	if !ok {
		t.Errorf("codex.go reads %s.%s but the schema no longer defines it", def, strings.Join(path, "."))
		return nil
	}
	return node
}

func (d *schemaDoc) requireTypedField(t *testing.T, want, def string, path ...string) {
	t.Helper()
	node := d.requireField(t, def, path...)
	if node == nil {
		return
	}
	if !hasType(node, want) {
		t.Errorf("%s.%s is %v, but codex.go decodes it as %s",
			def, strings.Join(path, "."), node["type"], want)
	}
}

func (d *schemaDoc) requireStringField(t *testing.T, def string, path ...string) {
	t.Helper()
	d.requireTypedField(t, "string", def, path...)
}

func (d *schemaDoc) requireIntField(t *testing.T, def string, path ...string) {
	t.Helper()
	d.requireTypedField(t, "integer", def, path...)
}

// hasType reports whether a schema node declares the given JSON type, allowing
// for nullable unions like ["string", "null"].
func hasType(node map[string]any, want string) bool {
	switch v := node["type"].(type) {
	case string:
		return v == want
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// requireRequired asserts a definition lists the given properties as required.
func (d *schemaDoc) requireRequired(t *testing.T, def string, want ...string) {
	t.Helper()
	node := d.definition(t, def)
	have := map[string]bool{}
	if arr, ok := node["required"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				have[s] = true
			}
		}
	}
	for _, w := range want {
		if !have[w] {
			t.Errorf("%s no longer requires %q, which codex.go always sends", def, w)
		}
	}
}

// requireSameProperties asserts two definitions expose the same property names.
// replyPermissionsApproval echoes a RequestPermissionProfile back as a
// GrantedPermissionProfile, so the two must not drift apart.
func (d *schemaDoc) requireSameProperties(t *testing.T, a, b string) {
	t.Helper()
	names := func(def string) []string {
		props, _ := d.definition(t, def)["properties"].(map[string]any)
		var out []string
		for k := range props {
			out = append(out, k)
		}
		sort.Strings(out)
		return out
	}
	an, bn := names(a), names(b)
	if len(an) == 0 {
		t.Errorf("%s has no properties", a)
		return
	}
	if strings.Join(an, ",") != strings.Join(bn, ",") {
		t.Errorf("%s (%v) and %s (%v) no longer have the same shape; codex.go echoes the requested profile back as the granted one",
			a, an, b, bn)
	}
}

// methodsOf collects the method names declared by one of the four top-level
// message unions (ClientRequest / ClientNotification / ServerRequest /
// ServerNotification). It is how the contract tells "must be sent with an id"
// from "must be sent without one".
func (d *schemaDoc) methodsOf(t *testing.T, union string) map[string]bool {
	t.Helper()
	node, ok := d.lookup("definitions/" + union)
	if !ok {
		t.Fatalf("schema %s has no %s union", d.path, union)
	}
	m, ok := node.(map[string]any)
	if !ok {
		t.Fatalf("%s is not an object", union)
	}
	branches, ok := m["oneOf"].([]any)
	if !ok {
		t.Fatalf("%s has no oneOf", union)
	}
	out := map[string]bool{}
	for _, b := range branches {
		bm, ok := b.(map[string]any)
		if !ok {
			continue
		}
		props, ok := bm["properties"].(map[string]any)
		if !ok {
			continue
		}
		mn, ok := props["method"].(map[string]any)
		if !ok {
			continue
		}
		for _, e := range enumOf(mn) {
			out[e] = true
		}
	}
	return out
}

// threadItemVariants maps each ThreadItem "type" value to its property set.
func (d *schemaDoc) threadItemVariants(t *testing.T) map[string]map[string]bool {
	t.Helper()
	item := d.definition(t, "v2/ThreadItem")
	oneOf, ok := item["oneOf"].([]any)
	if !ok {
		t.Fatalf("v2/ThreadItem has no oneOf")
	}
	out := map[string]map[string]bool{}
	for _, v := range oneOf {
		m, ok := v.(map[string]any)
		if !ok {
			continue
		}
		props, ok := m["properties"].(map[string]any)
		if !ok {
			continue
		}
		typeNode, ok := props["type"].(map[string]any)
		if !ok {
			continue
		}
		names := map[string]bool{}
		for k := range props {
			names[k] = true
		}
		for _, e := range enumOf(typeNode) {
			out[e] = names
		}
	}
	return out
}

// enumValues collects every string enum value reachable from a definition,
// including through oneOf/anyOf branches.
func (d *schemaDoc) enumValues(t *testing.T, def string) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			for _, e := range enumOf(v) {
				out[e] = true
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(d.definition(t, def))
	return out
}

func enumOf(node map[string]any) []string {
	var out []string
	if c, ok := node["const"].(string); ok {
		out = append(out, c)
	}
	if arr, ok := node["enum"].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// methods collects every JSON-RPC method name the schema declares, by finding
// objects with a "method" property that pins a const/enum value.
func (d *schemaDoc) methods() map[string]bool {
	out := map[string]bool{}
	var walk func(any)
	walk = func(n any) {
		switch v := n.(type) {
		case map[string]any:
			if props, ok := v["properties"].(map[string]any); ok {
				if m, ok := props["method"].(map[string]any); ok {
					for _, e := range enumOf(m) {
						out[e] = true
					}
				}
			}
			for _, child := range v {
				walk(child)
			}
		case []any:
			for _, child := range v {
				walk(child)
			}
		}
	}
	walk(d.root)
	return out
}

// summarizeDiff describes how two schema documents differ, by definition name,
// so a drift failure is triageable without hand-diffing 500k of JSON.
func summarizeDiff(vendored, generated map[string]any) []string {
	oldDefs := definitionsOf(vendored)
	newDefs := definitionsOf(generated)

	var lines []string
	report := func(scope string, o, n map[string]any) {
		var added, removed, changed []string
		for k := range n {
			if _, ok := o[k]; !ok {
				added = append(added, k)
			}
		}
		for k, ov := range o {
			nv, ok := n[k]
			if !ok {
				removed = append(removed, k)
				continue
			}
			if !jsonEqual(ov, nv) {
				changed = append(changed, k)
			}
		}
		for label, names := range map[string][]string{"added": added, "removed": removed, "changed": changed} {
			if len(names) == 0 {
				continue
			}
			sort.Strings(names)
			lines = append(lines, fmt.Sprintf("  %s %s (%d): %s", scope, label, len(names), strings.Join(clip(names, 40), ", ")))
		}
	}

	report("definitions", oldDefs, newDefs)
	oldV2, _ := oldDefs["v2"].(map[string]any)
	newV2, _ := newDefs["v2"].(map[string]any)
	if oldV2 != nil || newV2 != nil {
		report("definitions.v2", oldV2, newV2)
	}

	sort.Strings(lines)
	if len(lines) == 0 {
		lines = append(lines, "  (differences are outside definitions/)")
	}
	return lines
}

func definitionsOf(doc map[string]any) map[string]any {
	if d, ok := doc["definitions"].(map[string]any); ok {
		return d
	}
	return map[string]any{}
}

func jsonEqual(a, b any) bool {
	ab, err1 := json.Marshal(a)
	bb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return string(ab) == string(bb)
}

func clip(names []string, max int) []string {
	if len(names) <= max {
		return names
	}
	return append(append([]string{}, names[:max]...), fmt.Sprintf("... and %d more", len(names)-max))
}
