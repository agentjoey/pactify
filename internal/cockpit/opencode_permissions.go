package cockpit

import (
	"bytes"
	"encoding/json"
)

type opencodePermission struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

// Exact commands only. Native read/glob/grep tools handle arbitrary file paths.
var opencodeReadonlyCommands = []string{"ls", "ls -a", "ls -l", "ls -la", "pwd", "git status", "git status --short", "git diff", "git diff --stat", "git diff --numstat", "git log --oneline", "git log -5", "git show --stat", "git ls-files", "git rev-parse --show-toplevel"}
var opencodeUnsafePatterns = []string{"*>*", "*>>*", "*|*", "*;*", "*&*", "*$(*", "*`*", "*-delete*", "*-exec*", "*--output*", "*--pre*"}

func opencodeReadonlyPermissions() []opencodePermission {
	rules := []opencodePermission{{"*", "*", "deny"}}
	for _, tool := range []string{"read", "glob", "grep", "list"} {
		rules = append(rules, opencodePermission{tool, "*", "allow"})
	}
	rules = append(rules, opencodePermission{"bash", "*", "deny"})
	for _, cmd := range opencodeReadonlyCommands {
		rules = append(rules, opencodePermission{"bash", cmd, "allow"})
	}
	for _, pattern := range opencodeUnsafePatterns {
		rules = append(rules, opencodePermission{"bash", pattern, "deny"})
	}
	return rules
}

// A map would sort deny patterns before allows. OpenCode uses findLast, so
// preserve rule order both in config JSON and the session-specific rule array.
func orderedBashPolicy() json.RawMessage {
	var out bytes.Buffer
	out.WriteByte('{')
	first := true
	for _, r := range opencodeReadonlyPermissions() {
		if r.Permission != "bash" {
			continue
		}
		if !first {
			out.WriteByte(',')
		}
		first = false
		k, _ := json.Marshal(r.Pattern)
		v, _ := json.Marshal(r.Action)
		out.Write(k)
		out.WriteByte(':')
		out.Write(v)
	}
	out.WriteByte('}')
	return out.Bytes()
}
