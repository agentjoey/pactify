package cockpit

type opencodePermission struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

// Read-only exposes only the native read/glob/grep/list tools. bash is denied
// outright, as in the 2.x policy: an exact-command whitelist depends on
// opencode's shell parsing, and 2.x already skips the permission check for
// commands it cannot parse, so both API versions remove the shell tool.
func opencodeReadonlyPermissions() []opencodePermission {
	rules := []opencodePermission{{"*", "*", "deny"}}
	for _, tool := range []string{"read", "glob", "grep", "list"} {
		rules = append(rules, opencodePermission{tool, "*", "allow"})
	}
	return append(rules, opencodePermission{"bash", "*", "deny"})
}
