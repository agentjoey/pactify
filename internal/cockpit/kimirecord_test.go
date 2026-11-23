package cockpit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeKimiFixture(t *testing.T, index, wire string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", t.TempDir())
	dir := filepath.Join(home, "sessions", "wd_x", "session_abc")
	if err := os.MkdirAll(filepath.Join(dir, "agents", "main"), 0700); err != nil {
		t.Fatal(err)
	}
	index = strings.ReplaceAll(index, "SESSION_DIR", dir)
	mustWriteKimi(t, filepath.Join(home, "session_index.jsonl"), index)
	mustWriteKimi(t, filepath.Join(dir, "agents", "main", "wire.jsonl"), wire)
	return home
}
func mustWriteKimi(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestReadKimiRecord(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"other","sessionDir":"/nope"}
{"sessionId":"session_abc","sessionDir":"SESSION_DIR","workDir":"/w"}
`, `{"type":"config.update","modelAlias":"ignored"}
{"type":"llm.request","model":"kimi-for-coding","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"usage.record","usage":{"inputOther":100,"output":20,"inputCacheRead":1000,"inputCacheCreation":0},"usageScope":"turn"}
{"type":"context.append_loop_event","event":{"type":"step.end","usage":{"inputOther":100,"output":20,"inputCacheRead":1000}}}
{"type":"llm.request","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"usage.record","usage":{"inputOther":50,"output":5,"inputCacheRead":0,"inputCacheCreation":10},"usageScope":"turn"}
{"type":"usage.record","usage":{"inputOther":999,"output":999},"usageScope":"session"}
`)
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	want := KimiRecord{Models: []string{"kimi-code/kimi-for-coding"}, InputTokens: 1160, OutputTokens: 25, CacheRead: 1000, Turns: 2}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("record=%+v want=%+v", r, want)
	}
}
func TestReadKimiRecordModelOrderFallbackAndLongLines(t *testing.T) {
	wire := `{"type":"llm.request","modelAlias":"a","ignored":"` + strings.Repeat("x", 100000) + `"}
{"type":"llm.request","model":"b"}
{"type":"llm.request","modelAlias":"a"}
{"type":"llm.request"}
{"type":"llm.request","modelAlias":"c"}`
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, wire)
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Models, []string{"a", "b", "c"}) || r.Turns != 0 {
		t.Fatalf("record=%+v", r)
	}
}
func TestReadKimiRecordUnknownSession(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"a","sessionDir":"/x"}`, "")
	if _, err := ReadKimiRecord(home, "zzz"); err == nil || !strings.Contains(err.Error(), "zzz") {
		t.Fatalf("err=%v", err)
	}
}
func TestReadKimiRecordErrors(t *testing.T) {
	for _, tc := range []struct{ name, index, wire string }{
		{"bad index", "{", ""},
		{"missing directory", `{"sessionId":"session_abc"}`, ""},
		{"bad wire", `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, "{\n{}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := writeKimiFixture(t, tc.index, tc.wire)
			if _, err := ReadKimiRecord(home, "session_abc"); err == nil {
				t.Fatal("want error")
			}
		})
	}
	if _, err := ReadKimiRecord(t.TempDir(), "missing"); err == nil {
		t.Fatal("want missing index error")
	}
}

func TestReadKimiRecordTruncatedFinalLine(t *testing.T) {
	const prefix = `{"type":"llm.request","model":"kimi"}
{"type":"usage.record","usage":{"inputOther":10,"output":2},"usageScope":"turn"}
`
	for _, tc := range []struct {
		name string
		tail string
	}{
		{"without trailing newline", `{"type":"usage.record","usage":{"inputOther":100`},
		{"with trailing newline", "{\"type\":\"usage.record\",\"usage\":{\"inputOther\":100\n"},
		{"truncated string", `{"type":"llm.request","model":"unfinished`},
		{"truncated literal", `{"type":tru`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, prefix+tc.tail)
			r, err := ReadKimiRecord(home, "session_abc")
			if err != nil {
				t.Fatal(err)
			}
			want := KimiRecord{Models: []string{"kimi"}, InputTokens: 10, OutputTokens: 2, Turns: 1}
			if !reflect.DeepEqual(r, want) {
				t.Fatalf("record=%+v want=%+v", r, want)
			}
		})
	}
}

func TestReadKimiRecordEmptyOrTruncatedWire(t *testing.T) {
	for _, wire := range []string{"", "\n", "{", "{\n"} {
		t.Run(wire, func(t *testing.T) {
			home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, wire)
			r, err := ReadKimiRecord(home, "session_abc")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(r, KimiRecord{}) {
				t.Fatalf("record=%+v want empty record", r)
			}
		})
	}
}

func TestReadKimiRecordCorruptLineLocation(t *testing.T) {
	for _, tc := range []struct {
		name string
		wire string
	}{
		{"truncated middle line", "{}\n\n{\n{}\n"},
		{"corrupt middle line", "{}\n\nnot json\n{}\n"},
		{"corrupt final line", "{}\n\nnot json\n"},
		{"invalid final field type", "{}\n\n{\"type\":42}\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, tc.wire)
			_, err := ReadKimiRecord(home, "session_abc")
			path := filepath.Join(home, "sessions", "wd_x", "session_abc", "agents", "main", "wire.jsonl")
			if err == nil || !strings.Contains(err.Error(), path+":3:") {
				t.Fatalf("err=%v want %s:3:", err, path)
			}
		})
	}
}

func TestKimiRecordReportsMCPDiscovery(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, "{\"type\":\"mcp.tools_discovered\",\"tools\":[]}\n")
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil || !r.MCPToolsDiscovered {
		t.Fatalf("MCP discovery missing: %+v %v", r, err)
	}
}

// kimi-code 2.7 stopped writing llm.request; the model only appears on
// usage.record. Lines are trimmed from a real 2.7.2 wire.
func TestReadKimiRecordModelFromUsageRecordWithoutLLMRequest(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, `{"type":"profile.bind","agentId":"main","modelAlias":"kimi-code/kimi-for-coding","profileName":"agent"}
{"type":"usage.record","agentId":"main","model":"kimi-code/kimi-for-coding","usage":{"inputOther":1200,"output":234,"inputCacheRead":12288,"inputCacheCreation":0},"usageScope":"turn"}
{"type":"usage.record","agentId":"main","model":"kimi-code/kimi-for-coding","usage":{"inputOther":468,"output":120,"inputCacheRead":13312,"inputCacheCreation":0},"usageScope":"turn"}
{"type":"turn.ended","agentId":"main","reason":"completed"}
{"type":"prompt.completed","agentId":"main"}
`)
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	want := KimiRecord{Models: []string{"kimi-code/kimi-for-coding"}, InputTokens: 27268, OutputTokens: 354, CacheRead: 25600, Turns: 2, CompletedPrompts: 1, EndedTurns: 1, Reason: "completed"}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("record=%+v want=%+v", r, want)
	}
}

// When llm.request is present it stays authoritative: usage.record models
// from older wires must not add a second, differently spelled entry.
func TestReadKimiRecordLLMRequestWinsOverUsageModel(t *testing.T) {
	home := writeKimiFixture(t, `{"sessionId":"session_abc","sessionDir":"SESSION_DIR"}`, `{"type":"llm.request","model":"kimi-for-coding","modelAlias":"kimi-code/kimi-for-coding"}
{"type":"usage.record","model":"kimi-for-coding","usage":{"inputOther":1,"output":1},"usageScope":"turn"}
`)
	r, err := ReadKimiRecord(home, "session_abc")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r.Models, []string{"kimi-code/kimi-for-coding"}) {
		t.Fatalf("models=%v", r.Models)
	}
}
