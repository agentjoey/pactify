package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/agentjoey/pactify/internal/lockx"
)

type verifiedVersion struct {
	Version    string    `json:"version"`
	VerifiedAt time.Time `json:"verifiedAt"`
}

// A missing or damaged table is unverified, never a job error.
func readVerifiedVersions(path string) map[string]verifiedVersion {
	var versions map[string]verifiedVersion
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &versions) != nil || versions == nil {
		return map[string]verifiedVersion{}
	}
	return versions
}

func unverifiedVersionWarning(home string, h Harness, version string) string {
	// fake is a reserved, opt-in lifecycle fixture, not an installed CLI.
	if h.Name == "fake" {
		return ""
	}
	verified := readVerifiedVersions(filepath.Join(home, "verified-versions.json"))[h.Name]
	if version != "" && verified.Version == version && !verified.VerifiedAt.IsZero() {
		return ""
	}
	if version == "" {
		version = "（版本未知）"
	}
	return fmt.Sprintf("%s %s 尚未自检，建议先跑 pactify sub doctor", h.Name, version)
}

func recordVerifiedVersion(ctx context.Context, home, name, version string, now time.Time) error {
	if err := os.MkdirAll(home, 0700); err != nil {
		return err
	}
	path := filepath.Join(home, "verified-versions.json")
	release, err := lockx.Acquire(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	versions := readVerifiedVersions(path)
	versions[name] = verifiedVersion{Version: version, VerifiedAt: now.UTC()}
	data, err := json.MarshalIndent(versions, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(home, ".verified-versions-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err = f.Write(append(data, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}
