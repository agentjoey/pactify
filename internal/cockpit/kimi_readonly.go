package cockpit

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// KimiReadOnlyTools is also used to generate the execution-time tool policy.
func KimiReadOnlyTools() []string {
	return []string{"Read", "Grep", "Glob", "ReadMediaFile", "TodoList", "TaskList", "TaskOutput", "WaitFor"}
}

type KimiReadOnlyViolation struct{ Tool string }

func (e *KimiReadOnlyViolation) Error() string {
	return fmt.Sprintf("Read-only tool policy violation: kimi offered tool %q", e.Tool)
}

type kimiReadOnlyIndex struct {
	SessionID  string `json:"sessionId"`
	SessionDir string `json:"sessionDir"`
}

type kimiReadOnlyEvent struct {
	Type    string          `json:"type"`
	AgentID string          `json:"agentId"`
	Tools   json.RawMessage `json:"tools"`
}

// Decode one event at a time, retaining only requested fields. The decoder has
// no scanner token limit: large unrelated messages cannot hide later snapshots.
func readKimiReadOnlyJSON[T any](root *os.Root, path string, visit func(T) error) error {
	f, err := root.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("Read-only verification unavailable: kimi evidence file is missing: %w", os.ErrNotExist)
		}
		return fmt.Errorf("Read-only verification unavailable: cannot read kimi session evidence")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("Read-only verification unavailable: invalid kimi evidence file")
	}
	decoder := json.NewDecoder(f)
	for {
		var entry T
		err := decoder.Decode(&entry)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("Read-only verification unavailable: invalid or incomplete kimi session evidence")
		}
		if err := visit(entry); err != nil {
			return err
		}
	}
}

// CheckKimiReadonly reads only index routing and tool/agent event fields from
// the private session store. Root confines reads after canonical path checks.
func CheckKimiReadonly(home, sessionID string) error {
	if home == "" || !filepath.IsAbs(home) || sessionID == "" {
		return fmt.Errorf("Read-only verification unavailable: kimi private home or session is missing")
	}
	home, err := filepath.EvalSymlinks(home)
	if err != nil {
		return fmt.Errorf("Read-only verification unavailable: cannot resolve kimi private home")
	}
	root, err := os.OpenRoot(home)
	if err != nil {
		return fmt.Errorf("Read-only verification unavailable: cannot open kimi private home")
	}
	defer root.Close()
	dir := ""
	err = readKimiReadOnlyJSON(root, "session_index.jsonl", func(entry kimiReadOnlyIndex) error {
		if entry.SessionID == sessionID {
			dir = entry.SessionDir
		}
		return nil
	})
	if err != nil {
		return err
	}
	if dir == "" {
		return fmt.Errorf("Read-only verification unavailable: kimi session is absent")
	}
	dir, err = filepath.EvalSymlinks(dir)
	if err != nil {
		return fmt.Errorf("Read-only verification unavailable: cannot resolve kimi session directory")
	}
	rel, err := filepath.Rel(home, dir)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return fmt.Errorf("Read-only verification unavailable: kimi session is outside its private home")
	}
	agents := filepath.Join(rel, "agents")
	entries, err := fs.ReadDir(root.FS(), filepath.ToSlash(agents))
	if err != nil || len(entries) == 0 {
		return fmt.Errorf("Read-only verification unavailable: kimi tools snapshot is missing")
	}
	allowed := map[string]bool{}
	for _, tool := range KimiReadOnlyTools() {
		allowed[tool] = true
	}
	var unavailable error
	recordUnavailable := func(err error) {
		if unavailable == nil {
			unavailable = err
		}
	}
	calls, missing := map[string]bool{}, map[string]bool{}
	foundAny := false
	for _, entry := range entries {
		if !entry.IsDir() {
			recordUnavailable(fmt.Errorf("Read-only verification unavailable: kimi agent store is not a private directory"))
			continue
		}
		name := entry.Name()
		found := false
		err := readKimiReadOnlyJSON(root, filepath.Join(agents, name, "wire.jsonl"), func(event kimiReadOnlyEvent) error {
			if event.Type == "llm.request" {
				id := event.AgentID
				if id == "" {
					id = name
				}
				calls[id] = true
			}
			if event.Type != "llm.tools_snapshot" {
				return nil
			}
			var tools []struct {
				Name string `json:"name"`
			}
			if json.Unmarshal(event.Tools, &tools) != nil || tools == nil {
				recordUnavailable(fmt.Errorf("Read-only verification unavailable: invalid kimi tools snapshot"))
				return nil
			}
			found, foundAny = true, true
			for _, tool := range tools {
				if tool.Name == "" {
					recordUnavailable(fmt.Errorf("Read-only verification unavailable: kimi snapshot tool name is missing"))
					continue
				}
				if !allowed[tool.Name] {
					return &KimiReadOnlyViolation{Tool: tool.Name}
				}
			}
			return nil
		})
		var violation *KimiReadOnlyViolation
		if errors.As(err, &violation) {
			return err
		}
		if errors.Is(err, os.ErrNotExist) {
			missing[name] = true
			continue
		}
		if err != nil {
			recordUnavailable(err)
		}
		// An existing wire without a snapshot is always incomplete evidence.
		if !found {
			recordUnavailable(fmt.Errorf("Read-only verification unavailable: kimi agent tools snapshot is missing"))
		}
	}
	for name := range missing {
		if calls[name] {
			recordUnavailable(fmt.Errorf("Read-only verification unavailable: called kimi agent wire is missing"))
		}
	}
	if !foundAny {
		recordUnavailable(fmt.Errorf("Read-only verification unavailable: kimi tools snapshot is missing"))
	}
	return unavailable
}
