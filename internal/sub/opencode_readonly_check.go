package sub

import (
	"context"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"path/filepath"
	"time"
)

// CheckOpencodeReadonly checks the completed job's own persisted session with
// the backend recorded for it (1.x rule read-back or 2.x evaluation). The
// caller still checks job success and write-target absence independently.
func (store Store) CheckOpencodeReadonly(ctx context.Context, id string, result Result, env []string) error {
	return store.checkOpencodePermissions(ctx, id, result, env, "read-only")
}

// CheckOpencodeWrite verifies the completed job's own policy, without resuming it.
func (store Store) CheckOpencodeWrite(ctx context.Context, id string, result Result, env []string) error {
	return store.checkOpencodePermissions(ctx, id, result, env, "write")
}

func (store Store) checkOpencodePermissions(ctx context.Context, id string, result Result, env []string, access string) error {
	h, _ := Lookup("opencode")
	if result.CLIVersion == "" {
		return fmt.Errorf("permission evaluation requires a recorded opencode CLI version")
	}
	v2 := h.ForVersion(result.CLIVersion).Opencode2
	homes := opencodeHomes()
	if v2 {
		homes = opencode2Homes()
	}
	st, err := store.Load(id)
	if err != nil {
		return err
	}
	if st.Request.Harness != "opencode" || st.Request.Access != access || result.SessionID == "" {
		return fmt.Errorf("permission evaluation requires this probe's %s opencode session", access)
	}
	env = append(append([]string{}, env...), opencodeVendorStrip()...)
	for i, d := range homes.directories() {
		home := st.HarnessHomes[d.Env]
		if i == 0 {
			home = st.HarnessHome
		}
		if home == "" || !filepath.IsAbs(home) {
			return fmt.Errorf("permission evaluation requires the job's saved %s", d.Env)
		}
		env = append(env, d.Env+"="+home)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cwd := st.Request.Cwd
	if st.Worktree != nil {
		cwd = st.Worktree.Cwd
	}
	opts := cockpit.StartOpts{RepoDir: cwd, Env: env, MinimalConfig: st.EffectiveConfigMode() == "minimal", Access: cockpit.Access(access)}
	if access == "write" {
		opts.WritableRoots = cockpit.OpencodeWritableRoots(cwd, filepath.Join(store.Dir(id), "tmp"), filepath.Join(filepath.Dir(store.Root), "cache"))
		if v2 {
			return cockpit.CheckOpencode2Write(ctx, opts, result.SessionID)
		}
		return cockpit.CheckOpencodeWrite(ctx, opts, result.SessionID)
	}
	if v2 {
		return cockpit.CheckOpencode2Readonly(ctx, opts, result.SessionID)
	}
	return cockpit.CheckOpencodeReadonly(ctx, opts, result.SessionID)
}
