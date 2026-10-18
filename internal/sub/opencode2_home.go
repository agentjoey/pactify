package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/agentjoey/pactify/internal/acp"
	"github.com/agentjoey/pactify/internal/cockpit"
)

func opencodeMajor(version string) int {
	major, _, _ := strings.Cut(strings.TrimPrefix(version, "v"), ".")
	n, _ := strconv.Atoi(major)
	return n
}

// ForVersion selects the installed protocol using the already probed version.
func (h Harness) ForVersion(version string) Harness {
	if h.Name == "opencode" && opencodeMajor(version) == 2 {
		h.Opencode2 = true
		h.Backend = cockpit.NewOpencode2Backend
		h.Models = cockpit.Opencode2Models
		h.ModelDetails = cockpit.Opencode2ModelDetails
		h.Home = opencode2Homes()
	}
	return h
}
func (h Harness) forVersion(version string) Harness { return h.ForVersion(version) }
func onceVersion(probe func(context.Context) (string, error)) func(context.Context) (string, error) {
	var once sync.Once
	var version string
	var err error
	return func(ctx context.Context) (string, error) {
		once.Do(func() { version, err = probe(ctx) })
		return version, err
	}
}

// Opencode2ManagedHomes returns the private XDG roots for a pactify home.
// It neither reads user state nor creates files.
func Opencode2ManagedHomes(home string) map[string]string {
	root := filepath.Join(home, "harness-homes", "opencode2")
	return map[string]string{"XDG_DATA_HOME": filepath.Join(root, "share"), "XDG_CONFIG_HOME": filepath.Join(root, "config"), "XDG_STATE_HOME": filepath.Join(root, "state"), "XDG_CACHE_HOME": filepath.Join(root, "cache")}
}
func opencode2Homes() *harnessHome {
	var dirs []*harnessHome
	for _, item := range []struct{ env, name, fallback string }{{"XDG_DATA_HOME", "share", ".local/share"}, {"XDG_CONFIG_HOME", "config", ".config"}, {"XDG_STATE_HOME", "state", ".local/state"}, {"XDG_CACHE_HOME", "cache", ".cache"}} {
		item := item
		dirs = append(dirs, &harnessHome{Env: item.env, Source: func() (string, error) { return opencodeSource(item.env, item.fallback) }, Managed: func() (string, error) {
			store, err := DefaultStore()
			if err != nil {
				return "", err
			}
			home, err := filepath.Abs(filepath.Dir(store.Root))
			return Opencode2ManagedHomes(home)[item.env], err
		}, Layout: func(home, source string) *managedHome {
			return &managedHome{Name: "opencode2", Home: home, Source: source, AllowMissingSource: true}
		}})
	}
	dirs[0].Additional = dirs[1:]
	dirs[0].Doctor = doctorOpencode2Home
	return dirs[0]
}
func doctorOpencode2Home(store Store, id string, result Result, succeeded bool) error {
	if !succeeded || !strings.HasPrefix(result.SessionID, "ses") {
		return fmt.Errorf("managed-home probe did not succeed with a v2 session id")
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	for i, d := range opencode2Homes().directories() {
		home, err := d.Managed()
		if err != nil {
			return err
		}
		saved := st.HarnessHomes[d.Env]
		if i == 0 {
			saved = st.HarnessHome
		}
		if saved != home || st.EffectiveConfigMode() != "minimal" {
			return fmt.Errorf("probe did not use the private v2 %s", d.Env)
		}
		source, err := d.Source()
		if err != nil {
			return err
		}
		if err := d.Layout(home, source).Check(); err != nil {
			return err
		}
	}
	data := filepath.Join(st.HarnessHome, "opencode")
	for _, name := range []string{data, filepath.Join(data, "opencode.db")} {
		info, err := os.Lstat(name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("opencode private v2 database directory is unavailable")
		}
		if name == data && !info.IsDir() || name != data && !info.Mode().IsRegular() {
			return fmt.Errorf("invalid opencode private v2 database")
		}
	}
	return nil
}

// OpencodeLogin runs the interactive native flow in pactify's persistent v2 home.
func OpencodeLogin(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	h, _ := Lookup("opencode")
	version, err := h.Version(ctx)
	if err != nil {
		return err
	}
	if err = h.unsupported(version); err != nil {
		return err
	}
	h = h.forVersion(version)
	if opencodeMajor(version) != 2 {
		_, err = fmt.Fprintln(stdout, "Opencode 1.x uses your own opencode login (auth.json); run opencode auth login.")
		return err
	}
	_, _, env, layouts, err := prepareHarnessHomes(ctx, h.Home, "", nil, true, "")
	defer func() {
		for _, m := range layouts {
			for _, warning := range m.Cleanup() {
				fmt.Fprintln(stderr, warning)
			}
		}
	}()
	if err != nil {
		return err
	}
	env = append(env, opencodeVendorStrip()...)
	env, err = cockpit.Opencode2Env(cockpit.StartOpts{Env: append(env, "OPENCODE_CONFIG_CONTENT="), MinimalConfig: true})
	if err != nil {
		return err
	}
	// Jobs keep project config for AGENTS.md; login loads no project files.
	env = append(env, "OPENCODE_CONFIG_CONTENT=", "OPENCODE_DISABLE_PROJECT_CONFIG=1")
	release, err := cockpit.Opencode2Lease(ctx, env)
	if err != nil {
		return err
	}
	defer release()
	cmd := exec.CommandContext(ctx, "opencode", append([]string{"auth", "login", "--standalone"}, args...)...)
	cmd.Dir = filepath.Dir(layouts[0].Home)
	cmd.Env = acp.CommandEnv(env)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}
func (store Store) checkOpencodeResume(st State, version string) error {
	if st.Request.Resume == "" {
		return nil
	}
	old := store.opencodeResumeMajor(st.ResumeOf)
	current := opencodeMajor(version)
	if current == 2 && old != 2 {
		return fmt.Errorf("cannot resume opencode session: previous CLI major must be explicitly 2 (recorded major %d); start a new job", old)
	}
	if old > 0 && current > 0 && old != current {
		return fmt.Errorf("cannot resume an opencode %d.x session with opencode %d.x; start a new job", old, current)
	}
	return nil
}

const opencodeResumeUnknownWarning = "Previous opencode CLI version is unavailable; resuming with the installed 1.x backend"

func (store Store) opencodeResumeMajor(previous string) int {
	if previous == "" {
		return 0
	}
	if b, err := os.ReadFile(store.ResultPath(previous)); err == nil {
		var r Result
		if json.Unmarshal(b, &r) == nil {
			return opencodeMajor(r.CLIVersion)
		}
	}
	return 0
}
func (store Store) opencodeResumeWarning(st State, version string) string {
	if st.Request.Resume != "" && opencodeMajor(version) == 1 && store.opencodeResumeMajor(st.ResumeOf) == 0 {
		return opencodeResumeUnknownWarning
	}
	return ""
}

// Reject old or unexpected saved paths before prepareHarnessHomes can touch them.
func checkOpencode2SavedHomes(st State) error {
	if st.EffectiveConfigMode() != "minimal" {
		return nil
	}
	for i, d := range opencode2Homes().directories() {
		saved := st.HarnessHomes[d.Env]
		if i == 0 {
			saved = st.HarnessHome
		}
		want, err := d.Managed()
		if err != nil {
			return err
		}
		if saved != "" && filepath.Clean(saved) != filepath.Clean(want) {
			return fmt.Errorf("cannot use saved opencode home %q with opencode 2.x; start a new job with the private v2 home", saved)
		}
	}
	return nil
}
