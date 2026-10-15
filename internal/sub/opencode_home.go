package sub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

func opencodeVendorStrip() []string {
	// Opencode normally supports all providers. Sub authenticates through its
	// linked auth.json instead of inheriting sibling harness API credentials.
	return orchestrate.CrossVendorStrip("antigravity")
}

func opencodeSource(env, fallback string) (string, error) {
	if path := os.Getenv(env); path != "" {
		return filepath.Abs(path)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, fallback), nil
}
func opencodeDataSource() (string, error)   { return opencodeSource("XDG_DATA_HOME", ".local/share") }
func opencodeConfigSource() (string, error) { return opencodeSource("XDG_CONFIG_HOME", ".config") }
func opencodeManaged(name string) func() (string, error) {
	return func() (string, error) {
		root, err := managedHarnessHome("opencode")
		return filepath.Join(root, name), err
	}
}
func opencodeData(home, source string) *managedHome {
	return &managedHome{Name: "opencode", Home: home, Source: source, Mirror: true, Exclude: []string{"opencode"}, Links: map[string]homeLink{"opencode/auth.json": {Target: filepath.Join(source, "opencode/auth.json")}}}
}
func opencodeConfig(home, source string) *managedHome {
	return &managedHome{Name: "opencode", Home: home, Source: source, Mirror: true, AllowMissingSource: true, Exclude: []string{"opencode"}}
}
func opencodeHomes() *harnessHome {
	return &harnessHome{Env: "XDG_DATA_HOME", Source: opencodeDataSource, Managed: opencodeManaged("share"), Layout: opencodeData, Doctor: doctorOpencodeHome, BeforeDoctor: snapshotOpencodeUserData,
		Additional: []*harnessHome{{Env: "XDG_CONFIG_HOME", Source: opencodeConfigSource, Managed: opencodeManaged("config"), Layout: opencodeConfig}}}
}
func doctorOpencodeHome(store Store, id string, result Result, succeeded bool) error {
	if !succeeded || result.SessionID == "" {
		return fmt.Errorf("managed-home probe did not succeed with a session id")
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	h := opencodeHomes()
	for i, d := range h.directories() {
		home, err := d.Managed()
		if err != nil {
			return err
		}
		source, err := d.Source()
		if err != nil {
			return err
		}
		saved := st.HarnessHomes[d.Env]
		if i == 0 {
			saved = st.HarnessHome
		}
		if saved != home || st.EffectiveConfigMode() != "minimal" {
			return fmt.Errorf("probe did not use the shared minimal %s", d.Env)
		}
		if err = d.Layout(home, source).Check(); err != nil {
			return err
		}
	}
	for _, p := range []string{st.HarnessHome, filepath.Join(st.HarnessHome, "opencode")} {
		info, e := os.Lstat(p)
		if e != nil || !info.IsDir() {
			return fmt.Errorf("opencode private session directory is not a real directory: %s", p)
		}
	}
	info, e := os.Lstat(filepath.Join(st.HarnessHome, "opencode/opencode.db"))
	if e != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("opencode private session database is missing or is not a regular file")
	}
	return nil
}

// Metadata only: never open the login file or the user's session database.
func snapshotOpencodeUserData() (func() error, error) {
	source, err := opencodeDataSource()
	if err != nil {
		return nil, err
	}
	paths := []string{"opencode.db", "opencode.db-wal", "opencode.db-shm", "auth.json"}
	before := map[string]os.FileInfo{}
	for _, name := range paths {
		info, e := os.Lstat(filepath.Join(source, "opencode", name))
		if e != nil && !os.IsNotExist(e) {
			return nil, e
		}
		before[name] = info
	}
	return func() error {
		for _, name := range paths {
			after, e := os.Lstat(filepath.Join(source, "opencode", name))
			old := before[name]
			if os.IsNotExist(e) && old == nil {
				continue
			}
			if e != nil || old == nil || !os.SameFile(old, after) || old.Size() != after.Size() || !old.ModTime().Equal(after.ModTime()) {
				return fmt.Errorf("opencode user %s changed during doctor", name)
			}
		}
		return nil
	}, nil
}
func doctorOpencodeReadOnly(result Result) (bool, string) {
	if result.Status == StatusFailed || result.Status == StatusSucceeded {
		for _, w := range result.Warnings {
			if w == cockpit.OpencodeReadOnlyDenied {
				return true, "opencode denied the shell request without writing (expected read-only denial)"
			}
		}
	}
	return false, "no backend evidence of an opencode read-only permission denial"
}

// Name-based tiers are a heuristic, not a price quote: flash, then mini/lite/
// nano/haiku, then a non-pro model; preserve live-list order within a tier.
func opencodeDoctorModel(models []string) string {
	for _, tier := range [][]string{{"flash"}, {"mini", "lite", "nano", "haiku"}} {
		for _, m := range models {
			for _, word := range tier {
				if strings.Contains(strings.ToLower(m), word) {
					return m
				}
			}
		}
	}
	for _, m := range models {
		if !strings.Contains(strings.ToLower(m), "pro") {
			return m
		}
	}
	return models[0]
}
