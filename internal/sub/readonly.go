package sub

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
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
	var evidenceErr error
	if st.Request.Harness == "kimi" {
		evidenceErr = s.CheckKimiReadonly(st.ID, *result, nil)
		var violation *cockpit.KimiReadOnlyViolation
		if errors.As(evidenceErr, &violation) {
			return evidenceErr.Error(), true
		}
	}
	message, violation := verifyReadOnlyFiles(st, result)
	if st.Request.Harness == "kimi" && result.Status != StatusSucceeded {
		// Missing evidence cannot replace an auth/model/quota failure or a
		// cancellation. Positively observed file/tool violations still win.
		if !violation {
			return "", false
		}
	}
	if message != "" {
		return message, violation
	}
	if evidenceErr != nil {
		return evidenceErr.Error(), false
	}
	return "", false
}

func verifyReadOnlyFiles(st *State, result *Result) (string, bool) {
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

// A successful read tool alone cannot establish that writes are denied. Only
// agy needs this: opencode read-only removes the shell tool, so its doctor
// reads the session policy back instead (CheckOpencodeReadonly).
func doctorDeniedWrite(logPath, harness string) bool {
	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	commands := map[int]string{}
	for scan.Scan() {
		var e struct {
			Kind string `json:"kind"`
			Raw  struct {
				Step struct {
					Index int    `json:"step_index"`
					Name  string `json:"tool_name"`
					State string `json:"state"`
					Info  struct {
						Parameters struct {
							Command string `json:"CommandLine"`
						} `json:"parameters"`
						Error struct {
							Message string `json:"message"`
						} `json:"error"`
					} `json:"tool_info"`
				} `json:"step_update"`
			} `json:"raw"`
		}
		if json.Unmarshal(scan.Bytes(), &e) != nil {
			continue
		}
		step := e.Raw.Step
		lower := strings.ToLower(step.Info.Error.Message)
		if harness == "agy-hook" {
			if e.Kind == "tool" && step.Name == "write_to_file" && step.State == "ERROR" && strings.Contains(lower, "denied by pre-tool hook: pactify read-only policy") {
				return true
			}
			continue
		}
		if step.Info.Parameters.Command != "" {
			commands[step.Index] = step.Info.Parameters.Command
		}
		if e.Kind == "tool" && step.Name == "run_command" && step.State == "ERROR" && commands[step.Index] == "echo x > probe.txt" && (strings.Contains(lower, "permission") || strings.Contains(lower, "denied")) {
			return true
		}
	}
	return false
}

func requiresReadOnlyBaseline(req Request) bool {
	return (req.Harness == "opencode" || req.Harness == "agy" || req.Harness == "kimi") && req.Access == "read-only"
}
