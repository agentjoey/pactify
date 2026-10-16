package cockpit

import (
	"encoding/json"
	"strings"
	"testing"
)

// AJ-710 explicitly restores the read-only profile disabled in AJ-683.
func TestOpencodeReadOnlyPolicy(t *testing.T) {
	env, err := OpencodeEnv(StartOpts{Access: AccessReadOnly, MinimalConfig: true})
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	for _, entry := range env {
		if value, ok := strings.CutPrefix(entry, "OPENCODE_CONFIG_CONTENT="); ok {
			if err := json.Unmarshal([]byte(value), &config); err != nil {
				t.Fatal(err)
			}
		}
	}
	permission := config["permission"].(map[string]any)
	if permission["edit"] != "deny" || permission["write"] != "deny" {
		t.Fatal(permission)
	}
	for _, tool := range []string{"read", "glob", "grep", "list"} {
		if permission[tool] != "allow" {
			t.Errorf("read-only tool %s would require a denied permission", tool)
		}
	}
	bash := permission["bash"].(map[string]any)
	if bash["*"] != "deny" || bash["ls"] != "allow" || bash["git diff"] != "allow" || bash["git diff *"] != nil {
		t.Fatal(bash)
	}
}
