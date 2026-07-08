package cockpit

import "testing"

func TestGradeRisk(t *testing.T) {
	cases := []struct {
		tool, detail, want string
	}{
		{"Bash", "rm -rf /tmp/x", "exec"},
		{"Write", "some file content", "write"},
		{"Read", "some file content", "read"},
		{"mcp__x", "anything", "mcp"},
		{"ls", "", "read"},
		{"edit", "", "write"},
		{"run", "", "exec"},
		{"Fetch", "https://example.com", "read"},
	}
	for _, tc := range cases {
		got := gradeRisk(tc.tool, tc.detail)
		if got != tc.want {
			t.Errorf("gradeRisk(%q, %q) = %q, want %q", tc.tool, tc.detail, got, tc.want)
		}
	}
}
