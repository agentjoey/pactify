package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// AgyReadOnlyWorkspace rejects executable workspace hooks before any CLI probe:
// agy loads <dir>/hooks.json from .agents, .agent or _agents between the cwd
// and the repository root, and those hooks would run outside the read-only
// policy. A directory without hooks.json (such as a .agent notes folder) is fine.
// Resolve the real cwd so symlink aliases cannot hide a parent hook file.
func AgyReadOnlyWorkspace(workspace string) error {
	if workspace == "" {
		workspace = "."
	}
	dir, err := filepath.Abs(workspace)
	if err != nil {
		return err
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("agy read-only cannot inspect workspace hooks: %w", err)
	}

	for ; ; dir = filepath.Dir(dir) {
		for _, name := range []string{".agents", ".agent", "_agents"} {
			hooks := filepath.Join(dir, name, "hooks.json")
			if _, e := os.Lstat(hooks); e == nil {
				return fmt.Errorf("agy read-only refuses workspace hooks: %s", hooks)
			} else if !os.IsNotExist(e) && !errors.Is(e, syscall.ENOTDIR) {
				return fmt.Errorf("agy read-only cannot inspect workspace hooks: %w", e)
			}
		}
		if _, e := os.Lstat(filepath.Join(dir, ".git")); e == nil || filepath.Dir(dir) == dir {
			break
		}
	}
	return nil
}

// Agy uses prefix permissions natively. The hook narrows these to the exact
// command list shared with opencode and rejects shell composition altogether.
func AgyEnv(o StartOpts) ([]string, error) {
	env := append([]string{}, o.Env...)
	readOnly := "0"
	if o.Access == AccessReadOnly || o.ReadOnlyPolicy {
		readOnly = "1"
	}
	env = append(env, "PACTIFY_AGY_READ_ONLY="+readOnly)
	if !o.MinimalConfig {
		if o.Access == AccessReadOnly || o.ReadOnlyPolicy {
			return nil, fmt.Errorf("agy read-only requires managed minimal config")
		}
		return env, nil
	}
	if (o.Access == AccessReadOnly || o.ReadOnlyPolicy) && o.RepoDir != "" {
		if err := AgyReadOnlyWorkspace(o.RepoDir); err != nil {
			return nil, err
		}
	}
	home := ""
	for _, e := range env {
		if v, ok := strings.CutPrefix(e, "HOME="); ok {
			home = v
		}
	}
	if home == "" {
		return nil, fmt.Errorf("agy policy requires a managed HOME")
	}
	// os.Root prevents a replaced config directory from redirecting policy writes
	// into the user's HOME. Policy files themselves must never be symlinks.
	root, err := os.OpenRoot(home)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	for _, dir := range []string{".gemini", ".gemini/config", ".gemini/antigravity-cli"} {
		if err = root.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
			return nil, err
		}
		info, e := root.Lstat(dir)
		if e != nil || !info.IsDir() {
			return nil, fmt.Errorf("agy policy directory is not private: %s", dir)
		}
	}
	write := func(name string, data []byte) error {
		if info, e := root.Lstat(name); e == nil && !info.Mode().IsRegular() {
			return fmt.Errorf("agy policy file is not regular: %s", name)
		} else if e != nil && !os.IsNotExist(e) {
			return e
		}
		// All jobs install identical policy bytes. Rename publishes whole files.
		f, e := os.CreateTemp(filepath.Join(home, filepath.Dir(name)), ".pactify-policy-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		if e = f.Chmod(0600); e == nil {
			_, e = f.Write(data)
		}
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		return root.Rename(filepath.Join(filepath.Dir(name), filepath.Base(f.Name())), name)
	}
	executable, err := os.Executable()
	if err != nil {
		return nil, err
	}
	quoted := "'" + strings.ReplaceAll(executable, "'", "'\\''") + "'"
	// The handler has its own bounded input deadline. A launch/exit failure also
	// produces a deny record, rather than relying on agy's hook exit semantics.
	command := quoted + ` sub agy-hook || printf '%s\n' '{"decision":"deny","reason":"Pactify read-only policy: hook failed"}'`
	hooks, _ := json.Marshal(map[string]any{"pactify": map[string]any{"enabled": true, "PreToolUse": []any{map[string]any{"matcher": "*", "hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 10}}}}}})
	if err = write(".gemini/config/hooks.json", hooks); err != nil {
		return nil, err
	}
	settings, _ := json.Marshal(map[string]any{"permissions": map[string]any{"allow": []string{}}})
	if err = write(".gemini/antigravity-cli/settings.json", settings); err != nil {
		return nil, err
	}
	return env, nil
}

// RunAgyHook emits exactly one decision. Only an explicit "0" enables writes.
// It never loads user config, starts an agent, or waits unboundedly for stdin.
func RunAgyHook(ctx context.Context, in io.Reader, out io.Writer, access string) {
	result := map[string]any{"decision": "deny", "reason": "Pactify read-only policy: invalid hook input"}
	defer func() {
		if recover() != nil {
			result = map[string]any{"decision": "deny", "reason": "Pactify read-only policy: hook failure"}
		}
		_ = json.NewEncoder(out).Encode(result)
	}()
	ctx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	type payload struct {
		Call struct {
			Name string `json:"name"`
			Args struct {
				Bypass  bool   `json:"BypassSandbox"`
				Command string `json:"CommandLine"`
			} `json:"args"`
		} `json:"toolCall"`
	}
	type decoded struct {
		p   payload
		err error
	}
	read := make(chan decoded, 1)
	go func() {
		var p payload
		decoder := json.NewDecoder(io.LimitReader(in, 1024*1024))
		err := decoder.Decode(&p)
		if err == nil {
			var extra any
			if e := decoder.Decode(&extra); e != io.EOF {
				err = fmt.Errorf("unexpected trailing input")
			}
		}
		read <- decoded{p, err}
	}()
	var p payload
	select {
	case <-ctx.Done():
		result["reason"] = "Pactify read-only policy: hook input timed out"
		return
	case input := <-read:
		if input.err != nil || input.p.Call.Name == "" {
			return
		}
		p = input.p
	}
	if p.Call.Args.Bypass {
		result["reason"] = "Pactify denies BypassSandbox requests"
		return
	}
	if access == "0" {
		result = map[string]any{"decision": "allow"}
		return
	}
	name := p.Call.Name
	if name == "run_command" {
		command := p.Call.Args.Command
		if !slices.Contains(opencodeReadonlyCommands, command) {
			result["reason"] = "Pactify read-only policy: command is not an exact allowed read command"
			return
		}
		result = map[string]any{"decision": "auto_approve"}
		// Git's local helpers are executable code. Disable them even for exact reads.
		if strings.HasPrefix(command, "git ") {
			safe := "git --no-optional-locks -c core.fsmonitor=false -c core.hooksPath=/dev/null " + strings.TrimPrefix(command, "git ")
			if strings.HasPrefix(command, "git diff") || strings.HasPrefix(command, "git show") {
				safe += " --no-ext-diff --no-textconv"
			}
			result["overwrite"] = map[string]string{"CommandLine": safe}
		}
		return
	}
	if slices.Contains([]string{"view_file", "view_file_outline", "view_code_item", "list_dir", "grep_search", "find_by_name", "read_file", "list_resources", "read_resource", "command_status", "read_terminal", "task_boundary", "notify_user"}, name) {
		result = map[string]any{"decision": "allow"}
		return
	}
	result["reason"] = "Pactify read-only policy: writing tools are denied"
}
