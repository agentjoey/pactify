package sub

import (
	"fmt"
	"os"
	"path/filepath"
)

const agyTokenPath = ".gemini/antigravity-cli/antigravity-oauth-token"

func managedAgyHome() (string, error) { return managedHarnessHome("agy") }
func agyHome(home, source string) *managedHome {
	return &managedHome{Name: "agy", Home: home, Source: source, Mirror: true, Exclude: []string{".gemini"}, Links: map[string]homeLink{agyTokenPath: {Target: filepath.Join(source, agyTokenPath)}}}
}
func doctorAgyHome(store Store, id string, result Result, succeeded bool) error {
	if !succeeded || result.SessionID == "" {
		return fmt.Errorf("managed-home probe did not succeed with a session id")
	}
	home, err := managedAgyHome()
	if err != nil {
		return err
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if st.HarnessHome != home || st.EffectiveConfigMode() != "minimal" {
		return fmt.Errorf("probe did not use the shared minimal home")
	}
	source, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if err = agyHome(home, source).Check(); err != nil {
		return err
	}
	// Session databases and protobuf caches must remain inside the private tree.
	for _, name := range []string{".gemini", ".gemini/antigravity-cli"} {
		info, err := os.Lstat(filepath.Join(home, name))
		if err != nil || !info.IsDir() {
			return fmt.Errorf("agy private session directory is not a real directory: %s", name)
		}
	}

	db := filepath.Join(home, ".gemini", "antigravity-cli", "conversation_summaries.db")
	info, err := os.Lstat(db)
	if err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("agy private session database is missing or is not a regular file")
	}
	return nil
}

// Compare index metadata, never credentials or database contents. A concurrent
// user session can make this probe fail conservatively; it cannot falsely pass.
func snapshotAgyUserIndex() (func() error, error) {
	source, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(source, ".gemini", "antigravity-cli", "conversation_summaries.db")
	before, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return func() error {
		after, e := os.Lstat(path)
		if os.IsNotExist(e) && before == nil {
			return nil
		}
		if e != nil {
			return fmt.Errorf("agy user session index changed or cannot be inspected: %w", e)
		}
		if before == nil || !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
			return fmt.Errorf("agy user session index changed during doctor")
		}
		return nil
	}, nil
}
