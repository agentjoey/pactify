package sub

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// HarnessVersion is a stored version snapshot, never a live health check.
type HarnessVersion struct {
	Name            string `json:"name"`
	Installed       bool   `json:"installed"`
	Version         string `json:"version"`
	VerifiedVersion string `json:"verifiedVersion"`
}

// StoredHarnessVersions checks executable metadata and reads cached versions.
// Unlike Harnesses, it never discovers models, executes a CLI or persists a cache.
// A missing or changed executable has no known installed version until discovery.
func (s Store) StoredHarnessVersions() []HarnessVersion {
	var entries map[string]harnessCatalogEntry
	if data, err := os.ReadFile(catalogPath(s)); err == nil {
		_ = json.Unmarshal(data, &entries)
	}
	verified := readVerifiedVersions(filepath.Join(filepath.Dir(s.Root), "verified-versions.json"))
	rows := make([]HarnessVersion, 0, len(Names()))
	for _, name := range Names() {
		stamp, installed := processHarnessCatalog.executable(name)
		row := HarnessVersion{Name: name, Installed: installed, VerifiedVersion: verified[name].Version}
		if entry, ok := entries[name]; ok && installed && entry.Executable == stamp && entry.VersionError == "" {
			row.Version = entry.Version
		}
		rows = append(rows, row)
	}
	return rows
}
