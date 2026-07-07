package doctor

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// FindRepoRoot walks upward from start looking for a directory that contains
// bridge/claude-host/package.json. It stops at the filesystem root to avoid
// infinite loops (filepath.Dir("/") == "/").
func FindRepoRoot(start string) (string, bool) {
	dir := start
	if dir == "" {
		dir, _ = os.Getwd()
		if dir == "" {
			return "", false
		}
	}
	for {
		marker := filepath.Join(dir, "bridge", "claude-host", "package.json")
		if info, err := os.Stat(marker); err == nil && !info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// BridgeChecks returns the node and claude bridge dependency preflight checks.
func BridgeChecks(repoRoot string) []Check {
	checks := []Check{checkNode()}
	if repoRoot != "" {
		checks = append(checks, checkBridgeDeps(repoRoot))
	}
	return checks
}

func checkNode() Check {
	nodePath, err := exec.LookPath("node")
	if err != nil {
		return Check{
			Name:   "cli node: present",
			OK:     false,
			Detail: "install Node.js (the claude cockpit bridge needs it)",
		}
	}
	version := ""
	if out, err := exec.Command(nodePath, "--version").Output(); err == nil {
		version = string(out)
		// Drop trailing newline for a compact detail string.
		if len(version) > 0 && version[len(version)-1] == '\n' {
			version = version[:len(version)-1]
		}
	}
	detail := nodePath
	if version != "" {
		detail = fmt.Sprintf("%s (%s)", nodePath, version)
	}
	return Check{Name: "cli node: present", OK: true, Detail: detail}
}

func checkBridgeDeps(repoRoot string) Check {
	marker := filepath.Join(repoRoot, "bridge", "claude-host", "node_modules", "@anthropic-ai", "claude-agent-sdk")
	if info, err := os.Stat(marker); err == nil && info.IsDir() {
		return Check{Name: "claude bridge: deps", OK: true, Detail: "bridge deps materialized"}
	}
	return Check{
		Name:   "claude bridge: deps",
		OK:     false,
		Detail: "run `pactify doctor --setup-bridge`",
	}
}

// SetupBridge materializes the claude cockpit bridge node dependencies by
// running npm ci in repoRoot/bridge/claude-host (falling back to npm install
// when no package-lock.json exists). Command output is streamed to out.
func SetupBridge(repoRoot string, out io.Writer) error {
	bridgeDir := filepath.Join(repoRoot, "bridge", "claude-host")
	info, err := os.Stat(bridgeDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("bridge directory not found: %s", bridgeDir)
	}

	cmdName := "npm"
	args := []string{"ci"}
	if _, err := os.Stat(filepath.Join(bridgeDir, "package-lock.json")); err != nil {
		args = []string{"install"}
	}

	cmd := exec.Command(cmdName, args...)
	cmd.Dir = bridgeDir
	cmd.Stdout = out
	cmd.Stderr = out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("npm %s failed in %s: %w", args[0], bridgeDir, err)
	}
	fmt.Fprintln(out, "bridge deps installed")
	return nil
}
