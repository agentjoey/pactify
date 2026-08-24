package audit

import (
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 6, 16, 5, 10, 0, 0, time.UTC)

func TestFromHookClaudeBash(t *testing.T) {
	stdin := []byte(`{"tool_name":"Bash","tool_input":{"command":"git push origin main"},"session_id":"ses_x","cwd":"/repo/demo"}`)
	env := Env{Seat: "dev", Task: "t3", Project: "demo"}
	r, ok := FromHook("claude-code", stdin, env, fixedTime)
	if !ok {
		t.Fatal("expected ok=true for Bash")
	}
	if r.Tool != "bash" || r.Risk != "exec" || r.Summary != "git push origin main" {
		t.Fatalf("record = %+v", r)
	}
	if r.Seat != "dev" || r.Task != "t3" || r.Project != "demo" || r.Kind != "claude-code" {
		t.Fatalf("attribution = %+v", r)
	}
	if r.Session != "ses_x" || r.Repo != "/repo/demo" || r.Decision != "allow" {
		t.Fatalf("fields = %+v", r)
	}
	if r.TS != "2026-06-16T05:10:00Z" {
		t.Fatalf("ts = %q", r.TS)
	}
}

func TestFromHookClaudeFileTools(t *testing.T) {
	w := []byte(`{"tool_name":"Write","tool_input":{"file_path":"/repo/x.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("claude-code", w, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" || r.Summary != "/repo/x.go" {
		t.Fatalf("write = %+v", r)
	}
	rd := []byte(`{"tool_name":"Read","tool_input":{"file_path":"/repo/y.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("claude-code", rd, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" {
		t.Fatalf("read = %+v", r)
	}
}

func TestFromHookGeminiTools(t *testing.T) {
	bash := []byte(`{"tool_name":"run_shell_command","tool_input":{"command":"git status"},"cwd":"/repo/demo"}`)
	if r, _ := FromHook("gemini", bash, Env{Seat: "dev", Task: "t1"}, fixedTime); r.Tool != "bash" || r.Risk != "exec" || r.Summary != "git status" || r.Kind != "gemini" {
		t.Fatalf("run_shell_command = %+v", r)
	}

	w := []byte(`{"tool_name":"write_file","tool_input":{"file_path":"/repo/x.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", w, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" || r.Summary != "/repo/x.go" {
		t.Fatalf("write_file = %+v", r)
	}

	rep := []byte(`{"tool_name":"replace","tool_input":{"file_path":"/repo/x.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", rep, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" {
		t.Fatalf("replace = %+v", r)
	}

	rd := []byte(`{"tool_name":"read_file","tool_input":{"file_path":"/repo/y.go"},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", rd, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" {
		t.Fatalf("read_file = %+v", r)
	}

	many := []byte(`{"tool_name":"read_many_files","tool_input":{"file_paths":["/repo/a.go","/repo/b.go"]},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", many, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" {
		t.Fatalf("read_many_files = %+v", r)
	}

	search := []byte(`{"tool_name":"google_web_search","tool_input":{"query":"pactify docs"},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", search, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "pactify docs" {
		t.Fatalf("google_web_search = %+v", r)
	}

	fetch := []byte(`{"tool_name":"web_fetch","tool_input":{"url":"https://example.com"},"cwd":"/repo"}`)
	if r, _ := FromHook("gemini", fetch, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "https://example.com" {
		t.Fatalf("web_fetch = %+v", r)
	}
}

// TestFromHookAntigravityTools pins agy's PreToolUse payload shape. Unlike
// gemini-cli, agy does NOT send claude's {tool_name, tool_input, session_id,
// cwd}: it sends protojson camelCase with the call nested under `toolCall` and
// the session as `conversationId`. Every payload/arg name below is copied from a
// real agy 1.1.19 run captured through `.agents/hooks.json` (see docs/backlog.md
// [AUDIT]).
func TestFromHookAntigravityTools(t *testing.T) {
	run := []byte(`{"conversationId":"7a81210f","modelName":"gemini-3.7-flash-high","stepIdx":3,` +
		`"toolCall":{"name":"run_command","args":{"CommandLine":"echo PACTPROBE > probe-out.txt","Cwd":"/repo/demo","WaitMsBeforeAsync":5000}},` +
		`"workspacePaths":["/repo/demo"]}`)
	r, ok := FromHook("antigravity", run, Env{Seat: "agy", Task: "t9"}, fixedTime)
	if !ok {
		t.Fatal("expected ok=true for run_command")
	}
	if r.Tool != "bash" || r.Risk != "exec" || r.Summary != "echo PACTPROBE > probe-out.txt" {
		t.Fatalf("run_command = %+v", r)
	}
	if r.Kind != "antigravity" || r.Session != "7a81210f" || r.Repo != "/repo/demo" || r.Seat != "agy" || r.Task != "t9" {
		t.Fatalf("attribution = %+v", r)
	}
	// No PACT_PROJECT in env → project falls back to the workspace basename.
	if r.Project != "demo" {
		t.Fatalf("project = %q, want demo", r.Project)
	}
	if r.Decision != "allow" || r.TS != "2026-06-16T05:10:00Z" {
		t.Fatalf("fields = %+v", r)
	}

	view := []byte(`{"conversationId":"c1","toolCall":{"name":"view_file","args":{"AbsolutePath":"/repo/seed.txt"}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", view, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "/repo/seed.txt" {
		t.Fatalf("view_file = %+v", r)
	}

	write := []byte(`{"conversationId":"c1","toolCall":{"name":"write_to_file","args":{"TargetFile":"/repo/notes.md","CodeContent":"alpha\n","Overwrite":true}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", write, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" || r.Summary != "/repo/notes.md" {
		t.Fatalf("write_to_file = %+v", r)
	}

	repl := []byte(`{"conversationId":"c1","toolCall":{"name":"replace_file_content","args":{"TargetFile":"/repo/notes.md","ReplacementContent":"beta","TargetContent":"alpha"}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", repl, Env{}, fixedTime); r.Tool != "fs.write" || r.Risk != "write" || r.Summary != "/repo/notes.md" {
		t.Fatalf("replace_file_content = %+v", r)
	}

	ls := []byte(`{"conversationId":"c1","toolCall":{"name":"list_dir","args":{"DirectoryPath":"/repo/sub"}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", ls, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "/repo/sub" {
		t.Fatalf("list_dir = %+v", r)
	}

	grep := []byte(`{"conversationId":"c1","toolCall":{"name":"grep_search","args":{"Query":"needle","SearchPath":"/repo","MatchPerLine":true}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", grep, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "needle" {
		t.Fatalf("grep_search = %+v", r)
	}

	find := []byte(`{"conversationId":"c1","toolCall":{"name":"find_by_name","args":{"Pattern":"*.txt","SearchDirectory":"/repo"}},"workspacePaths":["/repo"]}`)
	if r, _ := FromHook("antigravity", find, Env{}, fixedTime); r.Tool != "fs.read" || r.Risk != "read" || r.Summary != "*.txt" {
		t.Fatalf("find_by_name = %+v", r)
	}
}

// The claude-shaped payload must NOT be accepted under --kind antigravity, and a
// tool agy never emits must stay unmapped, so a shape regression fails loudly
// rather than silently recording nothing.
func TestFromHookAntigravityRejectsForeignShapes(t *testing.T) {
	claudeShaped := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/repo"}`)
	if _, ok := FromHook("antigravity", claudeShaped, Env{}, fixedTime); ok {
		t.Fatal("claude-shaped payload should not parse as antigravity")
	}
	if _, ok := FromHook("antigravity", []byte(`{not json`), Env{}, fixedTime); ok {
		t.Fatal("malformed stdin should return ok=false")
	}
	unmapped := []byte(`{"conversationId":"c1","toolCall":{"name":"brain_update","args":{}},"workspacePaths":["/repo"]}`)
	if _, ok := FromHook("antigravity", unmapped, Env{}, fixedTime); ok {
		t.Fatal("unmapped agy tool should return ok=false")
	}
}

// PACT_PROJECT (set by the runner) must win over the workspace basename.
func TestFromHookAntigravityEnvProjectWins(t *testing.T) {
	in := []byte(`{"conversationId":"c1","toolCall":{"name":"run_command","args":{"CommandLine":"ls"}},"workspacePaths":["/home/me/checkout"]}`)
	if r, _ := FromHook("antigravity", in, Env{Project: "pactify"}, fixedTime); r.Project != "pactify" {
		t.Fatalf("project = %q, want pactify", r.Project)
	}
}

func TestFromHookProjectFallsBackToCwdBase(t *testing.T) {
	in := []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"},"cwd":"/home/me/myrepo"}`)
	if r, _ := FromHook("claude-code", in, Env{}, fixedTime); r.Project != "myrepo" {
		t.Fatalf("project fallback = %q, want myrepo", r.Project)
	}
}

func TestFromHookMCPPassthrough(t *testing.T) {
	m := []byte(`{"tool_name":"mcp__pact__status","tool_input":{"project":"demo"},"cwd":"/repo"}`)
	r, ok := FromHook("claude-code", m, Env{}, fixedTime)
	if !ok || r.Tool != "mcp__pact__status" || r.Risk != "mcp" {
		t.Fatalf("mcp = %+v ok=%v", r, ok)
	}
}

func TestFromHookUnmappedReturnsFalse(t *testing.T) {
	if _, ok := FromHook("claude-code", []byte(`{"tool_name":"TodoWrite","tool_input":{}}`), Env{}, fixedTime); ok {
		t.Fatal("unmapped tool should return ok=false")
	}
	if _, ok := FromHook("claude-code", []byte(`{not json`), Env{}, fixedTime); ok {
		t.Fatal("malformed stdin should return ok=false")
	}
}

func TestRedactionTruncatesAndMasksSecrets(t *testing.T) {
	long := "echo " + strings.Repeat("a", 400)
	in := []byte(`{"tool_name":"Bash","tool_input":{"command":"` + long + `"},"cwd":"/r"}`)
	r, _ := FromHook("claude-code", in, Env{}, fixedTime)
	if len([]rune(r.Summary)) > 205 {
		t.Fatalf("summary not truncated: %d runes", len([]rune(r.Summary)))
	}
	sec := []byte(`{"tool_name":"Bash","tool_input":{"command":"curl -H 'Authorization: Bearer sk-abc123secret' x"},"cwd":"/r"}`)
	r2, _ := FromHook("claude-code", sec, Env{}, fixedTime)
	if strings.Contains(r2.Summary, "sk-abc123secret") {
		t.Fatalf("secret not masked: %q", r2.Summary)
	}
}
