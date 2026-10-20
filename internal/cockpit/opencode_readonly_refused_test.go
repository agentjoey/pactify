package cockpit

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// Read-only sessions must remove shell tools that cannot enforce safe parsing.
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
	if permission["bash"] != "deny" {
		t.Fatalf("read-only bash = %#v, want deny", permission["bash"])
	}
}

// the session rules deny bash entirely; no command is allowed back.
func TestOpencodeReadOnlySessionRules(t *testing.T) {
	want := []opencodePermission{{"*", "*", "deny"}, {"read", "*", "allow"}, {"glob", "*", "allow"}, {"grep", "*", "allow"}, {"list", "*", "allow"}, {"bash", "*", "deny"}}
	if got := opencodeReadonlyPermissions(); !reflect.DeepEqual(got, want) {
		t.Fatalf("read-only session rules = %v, want %v", got, want)
	}
}
