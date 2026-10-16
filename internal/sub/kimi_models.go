package sub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/pelletier/go-toml/v2"
)

func kimiModels(ctx context.Context, opts cockpit.StartOpts) ([]string, error) {
	catalog, err := kimiModelDetails(ctx, opts)
	return catalog.Items, err
}

// kimiModelDetails prefers the managed config link, then the user config if
// missing. It never reads credentials, device_id or a CLI session.
// The decoder allowlist ignores all provider,
// authentication and model detail fields, even when present in config.toml.
func kimiModelDetails(ctx context.Context, opts cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
	out := cockpit.ModelDiscovery{Items: []string{}, Efforts: []string{}}
	if err := ctx.Err(); err != nil {
		return out, fmt.Errorf("kimi: model discovery canceled: %w", err)
	}
	home := ""
	for _, entry := range opts.Env {
		if value, ok := strings.CutPrefix(entry, "KIMI_CODE_HOME="); ok {
			home = value
		}
	}
	if home == "" {
		var err error
		home, err = managedKimiHome()
		if err != nil {
			return out, fmt.Errorf("kimi: managed configuration directory is unavailable")
		}
	}
	data, err := os.ReadFile(filepath.Join(home, "config.toml"))
	if os.IsNotExist(err) {
		// Discovery must also work before any job prepares the managed home.
		// Only the user's config.toml is opened; no directories or auth links
		// are created, and all fields still pass through the same allowlist.
		source, sourceErr := kimiHomeDir()
		if sourceErr != nil {
			return out, fmt.Errorf("kimi: configuration directory is unavailable")
		}
		data, err = os.ReadFile(filepath.Join(source, "config.toml"))
	}
	if err != nil {
		return out, fmt.Errorf("kimi: cannot read config.toml")
	}
	var config struct {
		Models       map[string]struct{} `toml:"models"`
		DefaultModel string              `toml:"default_model"`
	}
	// Do not wrap TOML diagnostics: they can quote provider/key values from the
	// original document, including malformed input outside our allowlist.
	if err := toml.Unmarshal(data, &config); err != nil {
		return out, fmt.Errorf("kimi: invalid config.toml")
	}
	for name := range config.Models {
		out.Items = append(out.Items, name)
	}
	slices.Sort(out.Items)
	if len(out.Items) == 0 {
		return out, fmt.Errorf("kimi: config.toml contains no configured models")
	}
	out.DefaultModel = config.DefaultModel
	return out, nil
}
