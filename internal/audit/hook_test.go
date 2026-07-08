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
