package orchestrate

import "testing"

func TestDefaultTransportModes(t *testing.T) {
	modes := DefaultTransportModes()
	if len(modes) != 1 {
		t.Fatalf("expected exactly one default mode, got %v", modes)
	}
	if modes["opencode"] != "acp" {
		t.Fatalf("expected opencode=acp by default, got %v", modes)
	}
}
