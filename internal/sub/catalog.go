package sub

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
	"github.com/agentjoey/pactify/internal/orchestrate"
)

type ModelCatalog struct {
	Items     []string  `json:"items"`
	Error     string    `json:"error"`
	FetchedAt time.Time `json:"fetchedAt"`
}
type HarnessInfo struct {
	Name            string         `json:"name"`
	Installed       bool           `json:"installed"`
	Version         string         `json:"version"`
	VerifiedVersion string         `json:"verifiedVersion"`
	Experimental    bool           `json:"experimental"`
	ReadOnly        bool           `json:"readOnly"`
	Models          ModelCatalog   `json:"models"`
	Default         HarnessDefault `json:"default"`
	Efforts         []string       `json:"efforts"`
}

type harnessCatalogEntry struct {
	Models       ModelCatalog
	Efforts      []string
	DefaultModel string
}

type harnessCatalog struct {
	mu         sync.Mutex
	entries    map[string]harnessCatalogEntry
	now        func() time.Time
	lookupPath func(string) (string, error)
}

func newHarnessCatalog() *harnessCatalog {
	return &harnessCatalog{entries: map[string]harnessCatalogEntry{}, now: time.Now, lookupPath: exec.LookPath}
}

var processHarnessCatalog = newHarnessCatalog()

// Harnesses uses only version and catalog discovery commands, never a prompt or
// a doctor probe. Cache keys include the store to keep independent homes separate.
func (s Store) Harnesses(ctx context.Context, refresh bool) ([]HarnessInfo, error) {
	router, err := s.LoadRouter()
	if err != nil {
		return nil, err
	}
	rows := []HarnessInfo{}
	for _, name := range Names() {
		h, _ := Lookup(name)
		var defaults HarnessDefault
		if router != nil {
			defaults = router.Defaults[name]
		}
		rows = append(rows, processHarnessCatalog.harness(ctx, s, h, defaults, refresh))
	}
	return rows, nil
}
func (c *harnessCatalog) harness(ctx context.Context, s Store, h Harness, defaults HarnessDefault, refresh bool) HarnessInfo {
	row := HarnessInfo{Name: h.Name, Default: defaults, Efforts: h.Efforts(), Experimental: h.Experimental != "", ReadOnly: h.NoReadOnly == ""}
	row.VerifiedVersion = readVerifiedVersions(filepath.Join(filepath.Dir(s.Root), "verified-versions.json"))[h.Name].Version
	_, err := c.lookupPath(h.Name)
	row.Installed = err == nil
	if row.Installed && h.Version != nil {
		row.Version, _ = h.Version(ctx)
	}
	// Serialize refreshes, including discovery, so simultaneous polling cannot
	// launch duplicate catalog processes and late failures cannot overwrite success.
	c.mu.Lock()
	defer c.mu.Unlock()
	key := s.Root + "\x00" + h.Name
	cached, ok := c.entries[key]
	now := c.now().UTC()
	if ok && !refresh && now.Sub(cached.Models.FetchedAt) < 10*time.Minute && row.Installed {
		return row.withCatalog(cached)
	}
	next := cached
	next.Models = ModelCatalog{Items: append([]string{}, cached.Models.Items...), FetchedAt: now}
	switch {
	case !row.Installed:
		next.Models.Error = "harness is not installed"
	case h.Models == nil:
		next.Models.Items = []string{}
		next.Efforts, next.DefaultModel = nil, ""
		next.Models.Error = "model discovery is not supported by this harness"
	default:
		details, err := discoverHarnessModels(ctx, h)
		if err != nil {
			next.Models.Error = err.Error()
		} else {
			next.Models.Items = append([]string{}, details.Items...)
			next.Efforts = append([]string{}, details.Efforts...)
			next.DefaultModel = details.DefaultModel
		}
	}
	c.entries[key] = next
	return row.withCatalog(next)
}

// Router overrides remain authoritative; native defaults and dynamic effort
// metadata share the exact same TTL and stale-on-error behavior as model ids.
func (row HarnessInfo) withCatalog(cached harnessCatalogEntry) HarnessInfo {
	row.Models = cached.Models
	if len(cached.Efforts) > 0 {
		row.Efforts = append([]string{}, cached.Efforts...)
	}
	if row.Default.Model == "" {
		row.Default.Model = cached.DefaultModel
	}
	return row
}

// discoverHarnessModels follows doctor's environment/home isolation but never
// enters Doctor or Backend.Start/Prompt. Codex only lists models after its
// handshake; kimi reads config metadata. AgyModels lists ids; OpencodeModels only
// initializes ACP and reads session configuration, without sending a prompt.
func discoverHarnessModels(ctx context.Context, h Harness) (catalog cockpit.ModelDiscovery, err error) {
	opts := cockpit.StartOpts{Env: orchestrate.CrossVendorStrip(h.Kind), MinimalConfig: true}
	discover := func() (cockpit.ModelDiscovery, error) {
		if h.ModelDetails != nil {
			return h.ModelDetails(ctx, opts)
		}
		items, err := h.Models(ctx, opts)
		return cockpit.ModelDiscovery{Items: items}, err
	}
	// Local kimi discovery reads the existing managed config link directly.
	// Preparing the full job home would inspect credentials/device_id needlessly.
	if h.Name == "kimi" {
		home, err := managedKimiHome()
		if err != nil {
			return catalog, fmt.Errorf("kimi: managed configuration directory is unavailable")
		}
		opts.Env = append(opts.Env, "KIMI_CODE_HOME="+home)
		return discover()
	}
	dir, err := os.MkdirTemp("", "pactify-models-*")
	if err != nil {
		return catalog, err
	}
	defer os.RemoveAll(dir)
	opts.RepoDir = dir
	if h.Home != nil {
		_, _, env, layouts, e := prepareHarnessHomes(ctx, h.Home, "", nil, true)
		defer func() {
			for _, layout := range layouts {
				if warnings := layout.Cleanup(); len(warnings) > 0 {
					err = fmt.Errorf("model discovery cleanup failed: %v (discovery error: %v)", warnings, err)
				}
			}
		}()
		if e != nil {
			return catalog, e
		}
		opts.Env = append(opts.Env, env...)
	}
	if h.Name == "opencode" {
		opts.Env = append(opts.Env, opencodeVendorStrip()...)
	}
	return discover()
}
