package sub

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/pelletier/go-toml/v2"
)

func kimiModels(ctx context.Context, opts cockpit.StartOpts) ([]string, error) {
	catalog, err := kimiModelDetails(ctx, opts)
	return catalog.Items, err
}

// kimiModelDetails lists live ACP options without a prompt; configuration is
// an explicitly warned offline fallback. Credential files are linked by the
// managed-home mechanism and are never opened by this adapter.
func kimiModelDetails(ctx context.Context, opts cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
	out, liveErr := kimiACPModels(ctx, opts)
	if liveErr == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	out, err := kimiConfigModels(ctx, opts)
	if err == nil {
		out.Warning = "Kimi ACP model discovery unavailable; using offline config.toml model list"
	}
	return out, err
}

// Discovery uses the same managed links and session-list isolation as jobs,
// in a disposable home. Closing and removing it also hides unprompted sessions.
func kimiACPModels(ctx context.Context, opts cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
	return kimiACPModelsWithCleanup(ctx, opts, os.RemoveAll)
}

func kimiACPModelsWithCleanup(ctx context.Context, opts cockpit.StartOpts, remove func(string) error) (out cockpit.ModelDiscovery, err error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	source := ""
	for _, entry := range opts.Env {
		if v, ok := strings.CutPrefix(entry, "KIMI_CODE_HOME="); ok {
			source = v
		}
	}
	if source == "" {
		source, err = kimiHomeDir()
		if err != nil {
			return out, err
		}
	}
	if _, e := os.Stat(filepath.Join(source, "config.toml")); e != nil {
		source, err = kimiHomeDir()
		if err != nil {
			return out, err
		}
	}
	dir, err := os.MkdirTemp("", "pactify-kimi-models-*")
	if err != nil {
		return out, err
	}
	defer func() {
		if cleanupErr := remove(dir); cleanupErr != nil {
			out.Warning = strings.TrimSpace(out.Warning + " Kimi model discovery cleanup failed: " + cleanupErr.Error())
		}
	}()
	home := filepath.Join(dir, "home")
	if err := prepareKimiHome(ctx, home, source); err != nil {
		return out, err
	}
	cwd := filepath.Join(dir, "workspace")
	if err := os.Mkdir(cwd, 0700); err != nil {
		return out, err
	}
	env := append(append([]string{}, opts.Env...), "KIMI_CODE_HOME="+home)
	client, err := acp.Spawn(ctx, "kimi", []string{"acp"}, env, cwd, io.Discard)
	if err != nil {
		return out, err
	}
	defer func() {
		if e := client.Close(); e != nil {
			if len(out.Items) > 0 && err == nil {
				out.Warning = strings.TrimSpace(out.Warning + " Kimi model discovery close failed: " + e.Error())
			} else if err == nil {
				err = e
			}
		}
	}()
	if _, err = client.Initialize(ctx); err != nil {
		return out, err
	}
	res, err := client.NewSessionDetailed(ctx, cwd)
	if err != nil {
		return out, err
	}
	for _, opt := range res.ConfigOptions {
		if opt.ID == "model" {
			out.DefaultModel = opt.CurrentValue
			for _, value := range opt.Options {
				if value.Value != "" && !slices.Contains(out.Items, value.Value) {
					out.Items = append(out.Items, value.Value)
				}
			}
		}
		if opt.Category == "thought_level" || opt.ID == "thinking" {
			for _, v := range opt.Options {
				out.Efforts = append(out.Efforts, v.Value)
			}
		}
	}
	if len(out.Items) == 0 {
		return out, fmt.Errorf("kimi: ACP returned no model options")
	}
	return out, nil
}

func kimiConfigModels(ctx context.Context, opts cockpit.StartOpts) (cockpit.ModelDiscovery, error) {
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
