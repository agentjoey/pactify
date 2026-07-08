package cockpit

import "strings"

// gradeRisk classifies a cockpit tool call or approval into a coarse risk
// bucket. It is conservative: anything that can mutate state or run arbitrary
// code is "write" or "exec", and MCP tool calls are called out separately.
func gradeRisk(tool, detail string) string {
	toolLower := strings.ToLower(tool)
	detailLower := strings.ToLower(detail)

	// Anything that executes arbitrary commands/shells is exec.
	if strings.Contains(toolLower, "bash") ||
		strings.Contains(toolLower, "shell") ||
		strings.Contains(toolLower, "exec") ||
		strings.Contains(toolLower, "command") ||
		strings.Contains(toolLower, "run") ||
		hasDangerousCommand(detailLower) {
		return "exec"
	}

	// State-mutating file/tool operations.
	if strings.Contains(toolLower, "write") ||
		strings.Contains(toolLower, "edit") ||
		strings.Contains(toolLower, "create") ||
		strings.Contains(toolLower, "patch") ||
		strings.Contains(toolLower, "apply") ||
		strings.Contains(toolLower, "delete") ||
		strings.Contains(toolLower, "rm") ||
		strings.Contains(toolLower, "mv") {
		return "write"
	}

	// MCP tool calls are explicitly flagged.
	if strings.Contains(toolLower, "mcp") {
		return "mcp"
	}

	// Everything else (read/grep/glob/ls/fetch/...) is read-only by default.
	return "read"
}

// hasDangerousCommand is a lightweight signal for command text that should be
// treated as exec even if the tool name itself is generic (e.g. a wrapper that
// runs shell commands). It does NOT log the detail; callers must keep raw input
// out of audit summaries.
func hasDangerousCommand(detailLower string) bool {
	return strings.Contains(detailLower, "rm ") ||
		strings.Contains(detailLower, "rm\t") ||
		strings.Contains(detailLower, "sudo") ||
		strings.Contains(detailLower, "bash") ||
		strings.Contains(detailLower, "sh ") ||
		strings.Contains(detailLower, "chmod") ||
		strings.Contains(detailLower, "chown") ||
		strings.Contains(detailLower, "curl") ||
		strings.Contains(detailLower, "wget") ||
		strings.Contains(detailLower, "python") ||
		strings.Contains(detailLower, "perl") ||
		strings.Contains(detailLower, "ruby")
}
