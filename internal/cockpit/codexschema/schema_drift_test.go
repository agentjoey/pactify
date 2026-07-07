package codexschema

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

const vendoredSchema = "codex_app_server_protocol.schemas.json"

// TestSchemaDrift guards against codex binary version drift. If `codex` is on
// PATH, the test regenerates the app-server schema and compares it to the
// vendored copy. Any difference fails the test so the vendored schema and the
// event/approval mapping can be re-validated.
func TestSchemaDrift(t *testing.T) {
	if _, err := exec.LookPath("codex"); err != nil {
		t.Skip("codex not found in PATH")
	}

	tmpDir := t.TempDir()

	cmd := exec.Command("codex", "app-server", "generate-json-schema", "--out", tmpDir)
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("generate-json-schema failed: %v", err)
	}

	generated, err := os.ReadFile(filepath.Join(tmpDir, vendoredSchema))
	if err != nil {
		t.Fatalf("read generated schema: %v", err)
	}
	vendored, err := os.ReadFile(vendoredSchema)
	if err != nil {
		t.Fatalf("read vendored schema %s: %v", vendoredSchema, err)
	}

	var g, v any
	if err := json.Unmarshal(generated, &g); err != nil {
		t.Fatalf("unmarshal generated schema: %v", err)
	}
	if err := json.Unmarshal(vendored, &v); err != nil {
		t.Fatalf("unmarshal vendored schema: %v", err)
	}

	if !reflect.DeepEqual(g, v) {
		t.Fatalf("codex app-server schema drifted from vendored %s; update vendored schema and revalidate mappings", vendoredSchema)
	}
}
