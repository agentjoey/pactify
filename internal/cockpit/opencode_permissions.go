package cockpit

type opencodePermission struct {
	Permission string `json:"permission"`
	Pattern    string `json:"pattern"`
	Action     string `json:"action"`
}

// Read-only exposes only the native read/glob/grep/list tools. bash is denied
// outright, as in the 2.x policy: an exact-command whitelist depends on
// opencode's shell parsing, and 2.x already skips the permission check for
// commands it cannot parse (AJ-720 B1). AJ-734 applied the same rule to 1.x.
func opencodeReadonlyPermissions() []opencodePermission {
	rules := []opencodePermission{{"*", "*", "deny"}}
	for _, tool := range []string{"read", "glob", "grep", "list"} {
		rules = append(rules, opencodePermission{tool, "*", "allow"})
	}
	return append(rules, opencodePermission{"bash", "*", "deny"})
}
