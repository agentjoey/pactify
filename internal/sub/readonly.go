package sub

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
)

type ReadOnlyFile struct {
	Size   int64       `json:"size"`
	Mtime  int64       `json:"mtime"`
	Mode   fs.FileMode `json:"mode"`
	Target string      `json:"target,omitempty"`
}

// ReadOnlySnapshot persists with the job so worker recovery also fails closed.
// Lstat metadata includes ignored/untracked files without reading their contents
// or following symlinks into user configuration and credentials.
type ReadOnlySnapshot struct {
	Root   string                  `json:"root"`
	Status string                  `json:"status"`
	Files  map[string]ReadOnlyFile `json:"files"`
}

func captureReadOnly(dir string) (*ReadOnlySnapshot, error) {
	root, err := repositoryRoot(dir)
	if err != nil {
		return nil, err
	}
	status, err := runGit(root, "--no-optional-locks", "status", "--porcelain=v1", "--ignored", "--untracked-files=all", "-z")
	if err != nil {
		return nil, err
	}
	snap := &ReadOnlySnapshot{Root: root, Status: string(status), Files: map[string]ReadOnlyFile{}}
	err = filepath.WalkDir(root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if file == root {
			return nil
		}
		if entry.Name() == ".git" {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		meta := ReadOnlyFile{Size: info.Size(), Mtime: info.ModTime().UnixNano(), Mode: info.Mode()}
		if info.IsDir() {
			meta.Size = 0
			meta.Mtime = 0
		}
		if info.Mode()&os.ModeSymlink != 0 {
			meta.Target, err = os.Readlink(file)
			if err != nil {
				return err
			}
		}
		snap.Files[rel] = meta
		return nil
	})
	return snap, err
}
func (s Store) verifyReadOnly(st *State, result *Result) (string, bool) {
	if !requiresReadOnlyBaseline(st.Request) {
		return "", false
	}
	if len(result.Diff.Files) > 0 {
		return "Read-only task changed repository files", true
	}
	if st.ReadOnlyBaseline == nil {
		// Never-started jobs have no workspace exposure. Running/recovered jobs
		// require a durable baseline even when the worker never publishes a diff.
		if st.StartedAt.IsZero() {
			return "", false
		}
		return "Read-only verification unavailable: workspace baseline is missing", false
	}
	current, err := captureReadOnly(st.ReadOnlyBaseline.Root)
	if err != nil {
		return "Read-only verification unavailable: " + err.Error(), false
	}
	before := st.ReadOnlyBaseline
	paths := map[string]bool{}
	for p := range before.Files {
		paths[p] = true
	}
	for p := range current.Files {
		paths[p] = true
	}
	var changed []string
	for p := range paths {
		a, aok := before.Files[p]
		b, bok := current.Files[p]
		if aok != bok || !reflect.DeepEqual(a, b) {
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	if len(changed) > 0 {
		for _, p := range changed {
			result.Diff.Files = append(result.Diff.Files, FileDelta{Path: p})
		}
		return fmt.Sprintf("Read-only task changed repository files: %v", changed), true
	}
	if before.Status != current.Status {
		return "Read-only task changed repository status", true
	}
	return "", false
}

// A successful read tool alone cannot establish that writes are denied.
func doctorDeniedWrite(logPath, harness string) bool {
	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	commands := map[int]string{}
	oc2Commands := map[string]string{}
	for scan.Scan() {
		var e struct {
			Kind string             `json:"kind"`
			Tool *cockpit.ToolEvent `json:"tool"`
			Raw  struct {
				AssistantMessageID string `json:"assistantMessageID"`
				ID                 string `json:"id"`
				Name               string `json:"name"`
				Input              struct {
					Command string `json:"command"`
				} `json:"input"`
				Error struct {
					Message string `json:"message"`
				} `json:"error"`
				Step struct {
					Index int    `json:"step_index"`
					Type  string `json:"step_type"`
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
				Part struct {
					Tool  string `json:"tool"`
					State struct {
						Status string `json:"status"`
						Input  struct {
							Command string `json:"command"`
						} `json:"input"`
						Error string `json:"error"`
					} `json:"state"`
				} `json:"part"`
			} `json:"raw"`
		}
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			continue
		}
		if harness == "agy" || harness == "agy-hook" {
			step := e.Raw.Step
			if harness == "agy-hook" {
				if e.Kind == "tool" && step.Name == "write_to_file" && step.State == "ERROR" && strings.Contains(strings.ToLower(step.Info.Error.Message), "denied by pre-tool hook: pactify read-only policy") {
					return true
				}
				continue
			}
			if step.Info.Parameters.Command != "" {
				commands[step.Index] = step.Info.Parameters.Command
			}
			lower := strings.ToLower(step.Info.Error.Message)
			if e.Kind == "tool" && step.Name == "run_command" && step.State == "ERROR" && commands[step.Index] == "echo x > probe.txt" && (strings.Contains(lower, "permission") || strings.Contains(lower, "denied")) {
				return true
			}
			continue
		}
		if harness == "opencode" && e.Kind == "tool" {
			key := e.Raw.AssistantMessageID + "/" + e.Raw.ID
			name := e.Raw.Name
			if name == "" && e.Tool != nil {
				name = e.Tool.Name
			}
			if name == "shell" && e.Raw.Input.Command != "" {
				oc2Commands[key] = e.Raw.Input.Command
			}
			if e.Tool != nil && e.Tool.Name == "shell" && e.Tool.Phase == "end" && oc2Commands[key] == "echo x > probe.txt" {
				lower := strings.ToLower(e.Raw.Error.Message)
				if strings.Contains(lower, "permission") || strings.Contains(lower, "denied") {
					return true
				}
			}
		}
		p := e.Raw.Part
		lower := strings.ToLower(p.State.Error)
		if e.Kind == "tool" && p.Tool == "bash" && p.State.Input.Command == "echo x > probe.txt" && p.State.Status == "error" && (strings.Contains(lower, "permission") || strings.Contains(lower, "denied")) {
			return true
		}
	}
	return false
}

func requiresReadOnlyBaseline(req Request) bool {
	return (req.Harness == "opencode" || req.Harness == "agy") && req.Access == "read-only"
}
