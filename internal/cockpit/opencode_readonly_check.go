package cockpit

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// CheckOpencodeReadonly reads the persisted 1.x session's own permission rules
// back, without resuming or PATCHing it. In 1.19 session rules are evaluated
// after agent and user rules, so the last matching session rule decides.
func CheckOpencodeReadonly(ctx context.Context, opts StartOpts, id string) error {
	if id == "" {
		return fmt.Errorf("opencode permission check requires the probe session id")
	}
	server, err := startOpencodeServer(ctx, opts)
	if err != nil {
		return err
	}
	defer server.close()
	var session struct {
		Permission []opencodePermission `json:"permission"`
	}
	if err := server.call(ctx, "GET", "/session/"+url.PathEscape(id), nil, &session); err != nil {
		return fmt.Errorf("opencode session permission read: %w", err)
	}
	for _, check := range []struct{ permission, pattern string }{{"bash", "touch x"}, {"edit", "x"}, {"external_directory", "/pactify-readonly-probe/*"}} {
		if action := opencodeLastAction(session.Permission, check.permission, check.pattern); action != "deny" {
			return fmt.Errorf("opencode readonly %s permission is %q; expected deny", check.permission, action)
		}
	}
	return nil
}

// opencodeLastAction mirrors opencode's findLast over wildcard rules; an
// unmatched request has no session decision and is reported as "".
func opencodeLastAction(rules []opencodePermission, permission, pattern string) string {
	action := ""
	for _, r := range rules {
		if opencodeWildcard(r.Permission, permission) && opencodeWildcard(r.Pattern, pattern) {
			action = r.Action
		}
	}
	return action
}

func opencodeWildcard(glob, s string) bool {
	quoted := regexp.QuoteMeta(glob)
	quoted = strings.ReplaceAll(quoted, `\*`, ".*")
	quoted = strings.ReplaceAll(quoted, `\?`, ".")
	return regexp.MustCompile("^(?s:" + quoted + ")$").MatchString(s)
}
