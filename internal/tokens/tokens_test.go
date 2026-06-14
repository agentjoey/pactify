package tokens

import (
	"testing"
)

func TestParse_ClaudeJSON(t *testing.T) {
	// the shape claude --output-format json emits (verified).
	out := `{"total_cost_usd":0.10,"usage":{"input_tokens":12009,"output_tokens":4,"cache_read_input_tokens":15363}}`
	n, ok := Parse("claude-code", out)
	if !ok || n != 12013 {
		t.Fatalf("claude parse = %d (ok=%v), want 12013", n, ok)
	}
}

func TestParse_StreamJSONL(t *testing.T) {
	// JSONL where the LAST usage-bearing line wins.
	out := "{\"type\":\"text\",\"value\":\"hi\"}\n{\"usage\":{\"input_tokens\":100,\"output_tokens\":20}}\n{\"usage\":{\"input_tokens\":140,\"output_tokens\":35}}\n"
	n, ok := Parse("opencode", out)
	if !ok || n != 175 {
		t.Fatalf("stream parse = %d (ok=%v), want 175", n, ok)
	}
}

func TestParse_TopLevelAndNone(t *testing.T) {
	if n, ok := Parse("x", `{"total_tokens":42}`); !ok || n != 42 {
		t.Fatalf("top-level total = %d (ok=%v), want 42", n, ok)
	}
	if _, ok := Parse("x", "no json here"); ok {
		t.Fatal("expected no usage parsed from non-json")
	}
}

func TestStore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := Load(dir) // missing → empty
	if len(s.Tasks) != 0 {
		t.Fatalf("fresh store should be empty, got %+v", s)
	}
	s.Add("t1", 100)
	s.Add("t1", 50)
	s.Add("t2", 200)
	if err := Save(dir, s); err != nil {
		t.Fatalf("save: %v", err)
	}
	got := Load(dir)
	if got.Get("t1") != 150 || got.Tasks["t1"].Runs != 2 {
		t.Fatalf("t1 = %+v, want 150 tokens / 2 runs", got.Tasks["t1"])
	}
	if got.Get("t2") != 200 {
		t.Fatalf("t2 = %d, want 200", got.Get("t2"))
	}
}
