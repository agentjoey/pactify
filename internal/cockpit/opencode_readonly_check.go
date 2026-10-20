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
	return checkOpencodePermissions(ctx, opts, id, []opencodePermission{{"bash", "touch x", "deny"}, {"edit", "x", "deny"}, {"external_directory", "/pactify-readonly-probe/*", "deny"}})
}

func checkOpencodePermissions(ctx context.Context, opts StartOpts, id string, checks []opencodePermission) error {
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
	for _, check := range checks {
		if action := opencodeLastAction(session.Permission, check.Permission, check.Pattern); action != check.Action {
			return fmt.Errorf("opencode %s %s permission is %q; expected %s", opts.Access, check.Permission, action, check.Action)
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

// CheckOpencodeWrite reads the completed write job's stored rules without PATCHing.
func CheckOpencodeWrite(ctx context.Context, opts StartOpts, id string) error {
	checks, err := opencodeWriteChecks(opts)
	if err != nil {
		return err
	}
	return checkOpencodePermissions(ctx, opts, id, checks)
}

func opencodeWriteChecks(opts StartOpts) ([]opencodePermission, error) {
	roots := OpencodeWritableRoots(opts.WritableRoots...)
	if len(roots) == 0 {
		return nil, fmt.Errorf("opencode write permission check requires allowed roots")
	}
	outside := "/pactify-write-probe-outside/*"
	if opencodePathAllowed(opencodeDirectoryPath(outside), roots) {
		return nil, fmt.Errorf("opencode write probe outside path is within allowed roots")
	}
	checks := []opencodePermission{{"external_directory", outside, "deny"}}
	for _, root := range roots {
		checks = append(checks, opencodePermission{"external_directory", strings.TrimRight(root, "/") + "/*", "ask"})
	}
	return checks, nil
}
