package cockpit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// kimi-code 2.x has no ACP or CLI verb to archive a session, but each session's
// state.json carries kimi's own "archived" flag: `kimi session list` and ACP
// session/list hide archived sessions, and kimi clears the flag itself when the
// session is loaded again (verified with kimi 2.2.0, 2026-10-15). These helpers use
// that flag so delegated sessions stay out of the user's kimi lists.

// ArchiveKimiSession sets kimi's archived flag on sessionID, keeping every other
// field of state.json, and returns the session directory.
func ArchiveKimiSession(kimiHome, sessionID string) (string, error) {
	dir, err := kimiSessionDir(kimiHome, sessionID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "state.json")
	var state map[string]any
	if err := readJSONFile(path, &state); err != nil {
		return "", err
	}
	state["archived"] = true
	return dir, writeJSONFile(path, state)
}

// PruneKimiWorkspace removes the workspaces.json entry for cwd when it was created
// at or after since (i.e. by the job, not by the user) and none of the sessions in
// that workspace is still visible. It reports whether an entry was removed.
func PruneKimiWorkspace(kimiHome, cwd string, since time.Time) (bool, error) {
	path := filepath.Join(kimiHome, "workspaces.json")
	var file map[string]any
	if err := readJSONFile(path, &file); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	workspaces, _ := file["workspaces"].(map[string]any)
	want := canonicalPath(cwd)
	for key, raw := range workspaces {
		w, _ := raw.(map[string]any)
		root, _ := w["root"].(string)
		if root == "" || canonicalPath(root) != want {
			continue
		}
		created, err := time.Parse(time.RFC3339Nano, fmt.Sprint(w["created_at"]))
		if err != nil || created.Before(since) {
			return false, nil
		}
		visible, err := kimiWorkspaceHasVisibleSession(kimiHome, key)
		if err != nil || visible {
			return false, err
		}
		delete(workspaces, key)
		return true, writeJSONFile(path, file)
	}
	return false, nil
}

func kimiWorkspaceHasVisibleSession(kimiHome, workspaceKey string) (bool, error) {
	states, err := filepath.Glob(filepath.Join(kimiHome, "sessions", workspaceKey, "*", "state.json"))
	if err != nil {
		return false, err
	}
	for _, p := range states {
		var st struct {
			Archived bool `json:"archived"`
		}
		if err := readJSONFile(p, &st); err != nil || !st.Archived {
			return true, nil // unreadable counts as visible: never prune on doubt
		}
	}
	return false, nil
}

func canonicalPath(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeJSONFile replaces path atomically, keeping its permissions.
func writeJSONFile(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pactify-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
