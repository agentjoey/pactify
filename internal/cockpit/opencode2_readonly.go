package cockpit

import (
	"context"
	"fmt"
	"net/url"
)

// CheckOpencode2Readonly evaluates the existing session, without creating,
// resuming or PATCHing it. A resumed session would replace the policy being tested.
func CheckOpencode2Readonly(ctx context.Context, opts StartOpts, id string) error {
	return checkOpencode2Permissions(ctx, opts, id, []opencodePermission{{"shell", "touch x", "deny"}, {"edit", "x", "deny"}, {"external_directory", "/pactify-readonly-probe/*", "deny"}})
}

func checkOpencode2Permissions(ctx context.Context, opts StartOpts, id string, checks []opencodePermission) error {
	if id == "" {
		return fmt.Errorf("opencode 2.x permission check requires the probe session id")
	}
	server, err := startOpencode2Server(ctx, opts)
	if err != nil {
		return err
	}
	defer server.close()
	for _, check := range checks {
		var result struct {
			Effect string `json:"effect"`
		}
		if err := server.call2(ctx, "POST", "/api/session/"+url.PathEscape(id)+"/permission", map[string]any{"action": check.Permission, "resources": []string{check.Pattern}}, &result); err != nil {
			return fmt.Errorf("opencode 2.x %s permission evaluation: %w", check.Permission, err)
		}
		if result.Effect != check.Action {
			return fmt.Errorf("opencode 2.x %s %s permission is %q; expected %s", opts.Access, check.Permission, result.Effect, check.Action)
		}
	}
	return nil
}

// CheckOpencode2Write evaluates the completed write job's own session policy.
func CheckOpencode2Write(ctx context.Context, opts StartOpts, id string) error {
	checks, err := opencodeWriteChecks(opts)
	if err != nil {
		return err
	}
	return checkOpencode2Permissions(ctx, opts, id, checks)
}
