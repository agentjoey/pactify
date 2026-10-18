package sub

import "path/filepath"

var kimiLinkedNames = []string{"config.toml", "credentials", "device_id", "oauth"}

func managedKimiHome() (string, error) { return managedHarnessHome("kimi") }
func kimiHome(home, source string) *managedHome {
	links := map[string]homeLink{}
	for _, name := range kimiLinkedNames {
		links[name] = homeLink{Target: filepath.Join(source, name), Directory: name == "credentials" || name == "oauth", Credential: name == "credentials", CreateDirectory: name == "oauth"}
	}
	return &managedHome{Name: "kimi", Home: home, Source: source, Links: links, Remove: []string{"mcp.json"}}
}

// checkKimiLinks verifies the managed home used by doctor.
func checkKimiLinks(home, source string) error { return kimiHome(home, source).Check() }
