package sub

import (
	"context"
	"path/filepath"
)

var kimiLinkedNames = []string{"config.toml", "credentials", "device_id", "oauth"}

func managedKimiHome() (string, error) { return managedHarnessHome("kimi") }
func kimiHome(home, source string) *managedHome {
	links := map[string]homeLink{}
	for _, name := range kimiLinkedNames {
		links[name] = homeLink{Target: filepath.Join(source, name), Directory: name == "credentials" || name == "oauth", Credential: name == "credentials", CreateDirectory: name == "oauth"}
	}
	return &managedHome{Name: "kimi", Home: home, Source: source, Links: links, Remove: []string{"mcp.json"}}
}

// Compatibility helpers keep the existing kimi tests and callers unchanged.
func checkKimiLinks(home, source string) error { return kimiHome(home, source).Check() }

func prepareKimiHome(ctx context.Context, home, source string) error {
	return kimiHome(home, source).Prepare(ctx)
}
