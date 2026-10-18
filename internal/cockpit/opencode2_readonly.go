package cockpit

import (
	"context"
	"fmt"
	"net/url"
)

// CheckOpencode2Readonly evaluates the existing session, without creating,
// resuming or PATCHing it. A resumed session would replace the policy being tested.
func CheckOpencode2Readonly(ctx context.Context, opts StartOpts, id string) error {
	if id == "" {
		return fmt.Errorf("opencode 2.x permission check requires the probe session id")
	}
	server, err := startOpencode2Server(ctx, opts)
	if err != nil {
		return err
	}
	defer server.close()
	for _, check := range []struct{ action, resource string }{{"shell", "touch x"}, {"edit", "x"}, {"external_directory", "/pactify-readonly-probe/*"}} {
		var result struct {
			Effect string `json:"effect"`
		}
		if err := server.call2(ctx, "POST", "/api/session/"+url.PathEscape(id)+"/permission", map[string]any{"action": check.action, "resources": []string{check.resource}}, &result); err != nil {
			return fmt.Errorf("opencode 2.x %s permission evaluation: %w", check.action, err)
		}
		if result.Effect != "deny" {
			return fmt.Errorf("opencode 2.x readonly %s permission is %q; expected deny", check.action, result.Effect)
		}
	}
	return nil
}
