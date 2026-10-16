package sub

import (
	"bufio"
	"encoding/json"
	"fmt"
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
	if st.Request.Harness != "opencode" || st.Request.Access != "read-only" {
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
func opencodeDoctorDeniedWrite(logPath string) bool {
	f, err := os.Open(logPath)
	if err != nil {
		return false
	}
	defer f.Close()
	scan := bufio.NewScanner(f)
	scan.Buffer(make([]byte, 65536), 16*1024*1024)
	for scan.Scan() {
		var e struct {
			Kind string `json:"kind"`
			Raw  struct {
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
		p := e.Raw.Part
		lower := strings.ToLower(p.State.Error)
		if e.Kind == "tool" && p.Tool == "bash" && p.State.Input.Command == "echo x > probe.txt" && p.State.Status == "error" && (strings.Contains(lower, "permission") || strings.Contains(lower, "denied")) {
			return true
		}
	}
	return false
}
