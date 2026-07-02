package gitx

import "testing"

// ValidBranchName accepts normal branch names (including slashes) and rejects
// everything that could reach git argv as a flag or expand to another ref:
// dash-prefixed names (check-ref-format has no "--" separator for its
// positional arg), previous-checkout syntax ("@{-1}", which --branch mode
// EXPANDS rather than rejects), and plain malformed names.
func TestValidBranchName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"main", true},
		{"feat/x", true},
		{"pact-t1-parse-args", true},
		{"", false},
		{"-evil", false},
		{"--force", false},
		{"bad name", false},
		{"a..b", false},
		{"@{-1}", false},
		{"feat/x.lock", false},
	}
	for _, tc := range cases {
		if got := ValidBranchName(tc.name); got != tc.want {
			t.Errorf("ValidBranchName(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}
