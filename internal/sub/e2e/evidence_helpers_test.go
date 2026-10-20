package e2e

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Pure evidence checks are untagged so ordinary tests can exercise them without
// building or launching the opt-in live suite.
func deniedWrites(harness string, rows [][]byte) (shell, file bool) {
	commands := map[int]string{}
	targets := map[int]string{}
	for _, row := range rows {
		var e struct {
			Kind string `json:"kind"`
			Raw  struct {
				Part struct {
					Tool  string `json:"tool"`
					State struct {
						Status string `json:"status"`
						Input  struct {
							Command  string `json:"command"`
							FilePath string `json:"filePath"`
						} `json:"input"`
						Error string `json:"error"`
					} `json:"state"`
				} `json:"part"`
				Step struct {
					Index int    `json:"step_index"`
					Name  string `json:"tool_name"`
					State string `json:"state"`
					Info  struct {
						Parameters struct {
							Command string `json:"CommandLine"`
							Target  string `json:"TargetFile"`
						} `json:"parameters"`
						Error struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"tool_info"`
				} `json:"step_update"`
				Item struct {
					Type     string `json:"type"`
					Status   string `json:"status"`
					Command  string `json:"command"`
					ExitCode *int   `json:"exitCode"`
					Output   string `json:"aggregatedOutput"`
					Changes  []struct {
						Path string `json:"path"`
					} `json:"changes"`
				} `json:"item"`
			} `json:"raw"`
		}
		if harness == "codex" && !json.Valid(row) {
			// codex rejects apply_patch in its tool router before any item starts;
			// the rejection appears only on the harness stderr kept in the job log.
			line := string(row)
			file = file || strings.Contains(line, "codex_core::tools::router") && strings.Contains(line, "patch rejected: writing is blocked by read-only sandbox")
			continue
		}
		if json.Unmarshal(row, &e) != nil || e.Kind != "tool" {
			continue
		}
		switch harness {
		case "opencode":
			p := e.Raw.Part
			if p.State.Status != "error" || !denialMessage(p.State.Error) {
				continue
			}
			shell = shell || p.Tool == "bash" && strings.Contains(p.State.Input.Command, "echo x > denied-shell.txt")
			file = file || (p.Tool == "write" || p.Tool == "edit" || p.Tool == "apply_patch") && filepath.Base(p.State.Input.FilePath) == "denied-tool.txt"
		case "agy":
			step := e.Raw.Step
			if step.Info.Parameters.Command != "" {
				commands[step.Index] = step.Info.Parameters.Command
			}
			if step.Info.Parameters.Target != "" {
				targets[step.Index] = step.Info.Parameters.Target
			}
			if step.State != "ERROR" || !denialMessage(step.Info.Error.Message) {
				continue
			}
			shell = shell || step.Name == "run_command" && strings.Contains(commands[step.Index], "echo x > denied-shell.txt")
			file = file || (step.Name == "write_to_file" || step.Name == "replace_file_content") && filepath.Base(targets[step.Index]) == "denied-tool.txt"
		case "codex":
			item := e.Raw.Item
			shell = shell || item.Type == "commandExecution" && item.ExitCode != nil && *item.ExitCode != 0 && denialMessage(item.Output) && strings.Contains(item.Command, "echo x > denied-shell.txt")
			if item.Type == "fileChange" && item.Status == "declined" {
				for _, change := range item.Changes {
					file = file || filepath.Base(change.Path) == "denied-tool.txt"
				}
			}
		}
	}
	return
}

func longToolEvidence(rows [][]byte, duration string) error {
	elapsed, err := time.ParseDuration(duration)
	if err != nil || elapsed < 240*time.Second {
		return fmt.Errorf("recorded job duration %q is below 240 seconds or invalid", duration)
	}
	command, output := false, false
	for _, row := range rows {
		var event struct {
			Kind string `json:"kind"`
			Tool struct {
				Phase string `json:"phase"`
				Text  string `json:"text"`
			} `json:"tool"`
			Raw struct {
				SessionUpdate string `json:"sessionUpdate"`
				Status        string `json:"status"`
				Input         struct {
					Command string `json:"command"`
				} `json:"rawInput"`
				Content []struct {
					Content struct {
						Type string `json:"type"`
						Text string `json:"text"`
					} `json:"content"`
				} `json:"content"`
				Item struct {
					Command string `json:"command"`
					Output  string `json:"aggregatedOutput"`
				} `json:"item"`
				Part struct {
					State struct {
						Input struct {
							Command string `json:"command"`
						} `json:"input"`
						Output string `json:"output"`
					} `json:"state"`
				} `json:"part"`
				Step struct {
					Info struct {
						Parameters struct {
							Command string `json:"CommandLine"`
						} `json:"parameters"`
						Output json.RawMessage `json:"output"`
					} `json:"tool_info"`
				} `json:"step_update"`
			} `json:"raw"`
		}
		if json.Unmarshal(row, &event) != nil || event.Kind != "tool" {
			continue
		}
		command = command || strings.Contains(event.Raw.Item.Command, "sleep 240") || strings.Contains(event.Raw.Part.State.Input.Command, "sleep 240") || strings.Contains(event.Raw.Step.Info.Parameters.Command, "sleep 240") || event.Tool.Phase == "start" && strings.Contains(event.Tool.Text, "sleep 240")
		command = command || event.Raw.SessionUpdate == "tool_call" && strings.Contains(event.Raw.Input.Command, "sleep 240")
		output = output || strings.Contains(event.Raw.Item.Output, "E2E_LONG_DONE") || strings.Contains(event.Raw.Part.State.Output, "E2E_LONG_DONE") || strings.Contains(string(event.Raw.Step.Info.Output), "E2E_LONG_DONE") || (event.Tool.Phase == "output" || event.Tool.Phase == "end") && strings.Contains(event.Tool.Text, "E2E_LONG_DONE")
		if event.Raw.SessionUpdate == "tool_call_update" {
			for _, part := range event.Raw.Content {
				text := part.Content.Type == "text"
				// Kimi 2.2.0 streams tool arguments as in-progress text, not rawInput.
				command = command || text && event.Raw.Status == "in_progress" && strings.Contains(part.Content.Text, `"command":"sleep 240`)
				output = output || text && event.Raw.Status == "completed" && strings.Contains(part.Content.Text, "E2E_LONG_DONE")
			}
		}
	}
	if !command || !output {
		return fmt.Errorf("long tool evidence missing: command=%t output=%t", command, output)
	}
	return nil
}

func hostAuthStatus(run func(...string) ([]byte, error)) error {
	out, err := run("auth", "status", "--json")
	var status struct {
		LoggedIn bool `json:"loggedIn"`
	}
	if err != nil || json.Unmarshal(out, &status) != nil || !status.LoggedIn {
		return fmt.Errorf("Claude host is not authenticated or auth status is unavailable; run claude auth login outside this suite")
	}
	return nil
}

func hostEnvironment(env []string, user, private, platform string) []string {
	set := func(key, value string) {
		filtered := env[:0:0]
		for _, entry := range env {
			if !strings.HasPrefix(entry, key+"=") {
				filtered = append(filtered, entry)
			}
		}
		env = append(filtered, key+"="+value)
	}
	set("CLAUDE_CONFIG_DIR", private)
	secure := user + "/.claude"
	if platform == "darwin" {
		secure = ""
		// The macOS keychain search list is resolved from HOME, so a private
		// HOME hides the user's login item. Configuration stays private through
		// CLAUDE_CONFIG_DIR.
		set("HOME", user)
	}
	set("CLAUDE_SECURESTORAGE_CONFIG_DIR", secure)
	set("DISABLE_AUTOUPDATER", "1")
	return env
}

func requiresCredentialRetention(warnings []string) bool {
	for _, warning := range warnings {
		if strings.Contains(warning, "private credential refresh retained at") || strings.Contains(warning, "private copy retained at") || strings.Contains(warning, "source metadata unavailable:") {
			return true
		}
	}
	return false
}

func denialMessage(text string) bool {
	text = strings.ToLower(text)
	for _, phrase := range []string{"permission denied", "not permitted", "denied by", "denied access", "permission", "read-only file system", "rejected"} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func toolRunningEvidence(rows [][]byte, opencode2 bool) bool {
	for _, row := range rows {
		var event struct {
			Kind  string `json:"kind"`
			State string `json:"state"`
			Tool  struct {
				Phase string `json:"phase"`
			} `json:"tool"`
		}
		if json.Unmarshal(row, &event) != nil {
			continue
		}
		if event.Kind == "tool" && event.Tool.Phase == "start" {
			return true
		}
		// v2 may publish shell.created before tool.called. The adapter has
		// already filtered it to this job's root/descendant sessions.
		if opencode2 && event.Kind == "system" && event.State == "shell.created" {
			return true
		}
	}
	return false
}

// readonlyToolPolicy identifies sessions whose write tools are removed entirely.
func readonlyToolPolicy(harness string, opencode2 bool) bool {
	return harness == "kimi" || harness == "opencode"
}
