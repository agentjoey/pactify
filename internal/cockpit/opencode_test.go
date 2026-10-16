package cockpit

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpencodeEnvMergeAndUserConfig(t *testing.T) {
	t.Setenv("OPENCODE_CONFIG_CONTENT", `{"permission":{"*":"deny"},"model":"test/default","other":true}`)
	for _, minimal := range []bool{true, false} {
		env, err := OpencodeEnv(StartOpts{Access: AccessWrite, MinimalConfig: minimal, Env: []string{`OPENCODE_CONFIG_CONTENT={"plugin":[],"permission":{"edit":"allow"}}`}})
		if err != nil {
			t.Fatal(err)
		}
		var config map[string]any
		for _, e := range env {
			if v, ok := strings.CutPrefix(e, "OPENCODE_CONFIG_CONTENT="); ok {
				json.Unmarshal([]byte(v), &config)
			}
		}
		if config["plugin"] == nil || config["permission"].(map[string]any)["edit"] != "allow" || config["other"] != nil {
			t.Fatal(config)
		}
		if strings.Contains(strings.Join(env, "\n"), "OPENCODE_PURE=1") != minimal {
			t.Fatal(env)
		}
	}
	t.Setenv("OPENCODE_CONFIG_CONTENT", "not-json")
	if _, err := OpencodeEnv(StartOpts{Access: AccessWrite}); err == nil {
		t.Fatal("malformed config accepted")
	}
}
