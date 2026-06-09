package paths

import (
	"os"
	"testing"
)

func TestDirDefaultsToDotPact(t *testing.T) {
	os.Unsetenv("PACT_DIR")
	if Dir() != ".pact" {
		t.Fatalf("Dir() = %q, want .pact", Dir())
	}
}

func TestDirHonoursEnv(t *testing.T) {
	t.Setenv("PACT_DIR", "/tmp/p")
	if Dir() != "/tmp/p" {
		t.Fatalf("Dir() = %q, want /tmp/p", Dir())
	}
	if Log() != "/tmp/p/log.jsonl" {
		t.Fatalf("Log() = %q", Log())
	}
}

func TestProtocolVersionIsOne(t *testing.T) {
	if ProtocolVersion != 1 {
		t.Fatalf("ProtocolVersion = %d, want 1", ProtocolVersion)
	}
}
