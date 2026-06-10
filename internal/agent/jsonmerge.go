package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// serverEntry builds the per-dialect JSON value for the pact server.
func serverEntry(format Format, inv Invoke) map[string]any {
	if format == JSONOpencode {
		cmd := append([]string{inv.Command}, inv.Args...)
		arr := make([]any, len(cmd))
		for i, s := range cmd {
			arr[i] = s
		}
		return map[string]any{
			"type":        "local",
			"command":     arr,
			"enabled":     true,
			"environment": toAny(inv.Env),
		}
	}
	args := make([]any, len(inv.Args))
	for i, s := range inv.Args {
		args[i] = s
	}
	return map[string]any{
		"command": inv.Command,
		"args":    args,
		"env":     toAny(inv.Env),
	}
}

func toAny(m map[string]string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func parentKey(format Format) string {
	if format == JSONOpencode {
		return "mcp"
	}
	return "mcpServers"
}

// mergeServer inserts/updates the "pact" server in the JSON config at path,
// preserving all other content. Missing file => {}. Invalid existing JSON is a
// hard error (never clobbered). Output is byte-idempotent (json.MarshalIndent
// sorts object keys). Symlink-safe and mkdir -p's the parent.
func mergeServer(path string, format Format, inv Invoke) error {
	root := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if len(bytes.TrimSpace(b)) > 0 {
			if err := json.Unmarshal(b, &root); err != nil {
				return fmt.Errorf("%s: existing config is not valid JSON: %w", path, err)
			}
		}
	}
	pk := parentKey(format)
	servers, _ := root[pk].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers["pact"] = serverEntry(format, inv)
	root[pk] = servers

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return err
	}
	out = append(out, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(path)
	}
	return os.WriteFile(path, out, 0o644)
}

// snippet renders the human-readable config fragment for --print / docs.
func snippet(format Format, inv Invoke) string {
	if format == TOML {
		args := "["
		for i, a := range inv.Args {
			if i > 0 {
				args += ", "
			}
			args += fmt.Sprintf("%q", a)
		}
		args += "]"
		env := ""
		for k, v := range inv.Env {
			env += fmt.Sprintf("%s = %q", k, v)
		}
		return fmt.Sprintf("[mcp_servers.pact]\ncommand = %q\nargs = %s\nenv = { %s }\n", inv.Command, args, env)
	}
	root := map[string]any{parentKey(format): map[string]any{"pact": serverEntry(format, inv)}}
	b, _ := json.MarshalIndent(root, "", "  ")
	return string(b) + "\n"
}
