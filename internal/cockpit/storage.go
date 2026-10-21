package cockpit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DeleteCodexThreads reuses the initialized app-server connection without
// loading or unarchiving the threads. Failure is returned so retention retries.
func DeleteCodexThreads(ctx context.Context, ids []string) error {
	s, err := newCodexAppServer(ctx, StartOpts{MinimalConfig: true, Stderr: io.Discard})
	if err != nil {
		return err
	}
	defer s.Close()
	return deleteCodexThreads(ctx, s.client, ids)
}
func deleteCodexThreads(ctx context.Context, c *codexClient, ids []string) error {
	for _, id := range ids {
		_, err := c.rpc.call(ctx, "thread/delete", map[string]string{"threadId": id})
		if err != nil {
			var rpc *rpcError
			if errors.As(err, &rpc) {
				m := strings.ToLower(strings.TrimSpace(rpc.Message))
				thread := strings.ToLower(id)
				missing := m == "thread not found" || m == "no such thread" || m == "no such thread "+thread || m == "no rollout found for thread id "+thread
				missing = missing || (rpc.Code != -32603 && (m == "not found" || m == "already deleted"))
				identified := strings.HasPrefix(m, "thread "+thread+" ") || strings.HasPrefix(m, "thread with id "+thread+" ") || strings.HasPrefix(m, "thread id "+thread+" ")
				missing = missing || identified && (strings.Contains(m, "not found") || strings.Contains(m, "already deleted") || strings.Contains(m, "does not exist"))
				if rpc.Code != -32601 && missing {
					continue
				}
			}
			return fmt.Errorf("codex thread/delete %s: %w", id, err)
		}
	}
	return nil
}

func (s *opencodeServer) deleteStorageSession(ctx context.Context, id string) error {
	_, e := s.deleteStorageSessionCount(ctx, id)
	return e
}
func (s *opencodeServer) deleteStorageSessionCount(ctx context.Context, id string) (bool, error) {
	path := "/session/" + url.PathEscape(id)
	if s.v2 {
		path = "/api/session/" + url.PathEscape(id)
	}
	var ok bool
	var err error
	if s.v2 {
		err = s.call2(ctx, http.MethodDelete, path, nil, &ok)
	} else {
		err = s.call(ctx, http.MethodDelete, path, nil, &ok)
	}
	var status *opencodeHTTPError
	if errors.As(err, &status) && status.status == http.StatusNotFound {
		sessions, e := s.storageSessions(ctx)
		if e != nil {
			return false, e
		}
		if _, exists := sessions[id]; exists {
			return false, fmt.Errorf("opencode returned 404 for existing session %s", id)
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("opencode refused session deletion: %s", id)
	}
	return true, nil
}

// PruneOpencodeSessions starts the existing private native server adapter. A
// nil ids list discovers only orphan sessions; keep includes every existing job.
func PruneOpencodeSessions(ctx context.Context, opts StartOpts, v2 bool, ids []string, keep map[string]bool) error {
	_, err := PruneOpencodeSessionsCount(ctx, opts, v2, ids, keep)
	return err
}
func PruneOpencodeSessionsCount(ctx context.Context, opts StartOpts, v2 bool, ids []string, keep map[string]bool) (int, error) {
	return pruneOpencodeSessions(ctx, opts, v2, ids, keep)
}

func PruneOpencodeOrphans(ctx context.Context, opts StartOpts, v2 bool, keep map[string]bool) (int, error) {
	return pruneOpencodeSessions(ctx, opts, v2, nil, keep)
}
func pruneOpencodeSessions(ctx context.Context, opts StartOpts, v2 bool, ids []string, keep map[string]bool) (int, error) {
	var s *opencodeServer
	var err error
	if v2 {
		s, err = startOpencode2Server(ctx, opts)
	} else {
		s, err = startOpencodeServer(ctx, opts)
	}
	if err != nil {
		return 0, err
	}
	defer s.close()
	expectedData := ""
	for _, e := range opts.Env {
		if k, v, ok := strings.Cut(e, "="); ok && k == "XDG_DATA_HOME" {
			expectedData = v
		}
	}
	if err = s.verifyStorageDatabase(ctx, s.cmd.Env, expectedData); err != nil {
		return 0, err
	}
	return s.pruneStorageSessions(ctx, ids, keep)
}

type storageNativeSession struct {
	ID       string `json:"id"`
	ParentID string `json:"parentID"`
}

// The ordinary v1 /session list is project-scoped. Its global endpoint and the
// v2 cursor endpoint must be exhausted before deciding anything is an orphan.
func (s *opencodeServer) storageSessions(ctx context.Context) (map[string]storageNativeSession, error) {
	sessions := map[string]storageNativeSession{}
	cursor := ""
	seen := map[string]bool{}
	for {
		path := "/experimental/session?limit=1000&archived=true"
		if s.v2 {
			path = "/api/session?limit=1000"
		}
		if cursor != "" {
			path += "&cursor=" + url.QueryEscape(cursor)
		}
		res, err := s.request(ctx, http.MethodGet, path, nil)
		if err != nil {
			return nil, err
		}
		var page []storageNativeSession
		next := ""
		decode := json.NewDecoder(io.LimitReader(res.Body, 16<<20))
		if s.v2 {
			var response struct {
				Data   []storageNativeSession `json:"data"`
				Cursor *struct {
					Next *string `json:"next"`
				} `json:"cursor"`
			}
			err = decode.Decode(&response)
			page = response.Data
			if response.Cursor == nil && err == nil {
				err = fmt.Errorf("opencode session listing missing cursor")
			}
			if response.Cursor != nil && response.Cursor.Next != nil {
				next = *response.Cursor.Next
			}
		} else {
			err = decode.Decode(&page)
			next = res.Header.Get("x-next-cursor")
		}
		if err == nil && decode.Decode(new(any)) != io.EOF {
			err = fmt.Errorf("invalid opencode session listing")
		}
		res.Body.Close()
		if err != nil {
			return nil, err
		}
		for _, entry := range page {
			if entry.ID == "" {
				return nil, fmt.Errorf("opencode session listing missing id")
			}
			sessions[entry.ID] = entry
			if len(sessions) > 200000 {
				return nil, fmt.Errorf("opencode session listing exceeds scan limit")
			}
		}
		if next == "" {
			return sessions, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("opencode session listing repeated cursor")
		}
		seen[next] = true
		cursor = next
	}
}
func (s *opencodeServer) pruneStorageSessions(ctx context.Context, ids []string, keep map[string]bool) (int, error) {
	protected := map[string]bool{}
	for id, v := range keep {
		if v && id != "" {
			protected[id] = true
		}
	}
	if ids == nil || len(protected) > 0 {
		sessions, err := s.storageSessions(ctx)
		if err != nil {
			return 0, err
		}
		for id := range keep {
			if id == "" || !keep[id] {
				continue
			}
			if _, ok := sessions[id]; !ok {
				continue
			}
			seen := map[string]bool{}
			for at := id; at != ""; at = sessions[at].ParentID {
				if seen[at] {
					return 0, fmt.Errorf("opencode session parent cycle")
				}
				seen[at] = true
				protected[at] = true
			}
		}
		if ids == nil {
			ids = []string{}
			for id := range sessions {
				if !protected[id] {
					ids = append(ids, id)
				}
			}
		} else {
			for _, id := range ids {
				if protected[id] {
					return 0, fmt.Errorf("opencode session %s has a referenced descendant", id)
				}
			}
		}
	}
	sort.Strings(ids)
	removed := 0
	seenIDs := map[string]bool{}
	for _, id := range ids {
		if seenIDs[id] {
			continue
		}
		seenIDs[id] = true
		if protected[id] {
			continue
		}
		deleted, err := s.deleteStorageSessionCount(ctx, id)
		if err != nil {
			return removed, err
		}
		if deleted {
			removed++
		}
	}
	return removed, nil
}

func DeleteKimiSessionsViaServer(ctx context.Context, home string, ids []string) error {
	return withKimiWebServer(ctx, []string{home}, func(base, token string) error {
		for _, id := range ids {
			if err := deleteKimiSessionHTTP(ctx, base, token, id); err != nil {
				return err
			}
		}
		return nil
	})
}
func deleteKimiSessionHTTP(ctx context.Context, base, token, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, base+"/api/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	defer client.CloseIdleConnections()
	var res *http.Response
	for attempt := 0; ; attempt++ {
		res, err = client.Do(req)
		if err == nil || attempt == 20 || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}

	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 64<<10))
	if res.StatusCode == http.StatusNotFound || res.StatusCode >= 200 && res.StatusCode < 300 {
		return nil
	}
	return fmt.Errorf("kimi delete API: HTTP %d", res.StatusCode)
}

// The v1 path contract reports state/config, not data. Verify the actual
// child environment as well as its reported paths before trusting its database.
func (s *opencodeServer) verifyStorageDatabase(ctx context.Context, env []string, expectedData string) error {
	values := map[string]string{}
	for _, e := range env {
		k, v, ok := strings.Cut(e, "=")
		if ok {
			values[k] = v
		}
	}
	data := values["XDG_DATA_HOME"]
	if !filepath.IsAbs(data) || !filepath.IsAbs(expectedData) || values["OPENCODE_DB"] != "opencode.db" || values["OPENCODE_DISABLE_CHANNEL_DB"] != "1" {
		return fmt.Errorf("private opencode database requires an absolute XDG data root and pinned relative OPENCODE_DB")
	}
	verify := func(name, reported, expected string) error {
		aPath, aErr := filepath.EvalSymlinks(reported)
		bPath, bErr := filepath.EvalSymlinks(expected)
		a, e := os.Stat(aPath)
		b, f := os.Stat(bPath)
		if aErr != nil || bErr != nil || e != nil || f != nil || !a.IsDir() || !b.IsDir() || !os.SameFile(a, b) {
			return fmt.Errorf("opencode server %s path %q is not the private home %q", name, reported, expected)
		}
		return nil
	}
	if err := verify("XDG_DATA_HOME", data, expectedData); err != nil {
		return err
	}
	// Anchor all subsequent checks to the caller's verified home, including when
	// the child uses an alias for its data root.
	resolvedData, resolveErr := filepath.EvalSymlinks(expectedData)
	if resolveErr != nil {
		return fmt.Errorf("cannot resolve private opencode data root %q: %w", expectedData, resolveErr)
	}
	data = resolvedData
	var paths struct {
		Data   string `json:"data"`
		State  string `json:"state"`
		Config string `json:"config"`
	}
	path := "/path"
	if s.v2 {
		path = "/api/path"
	}
	var err error
	if s.v2 {
		err = s.call2(ctx, http.MethodGet, path, nil, &paths)
	} else {
		err = s.call(ctx, http.MethodGet, path, nil, &paths)
	}
	if err != nil {
		return fmt.Errorf("cannot confirm private opencode database: %w", err)
	}
	if s.v2 && paths.Data != "" {
		if err = verify("data", paths.Data, filepath.Join(data, "opencode")); err != nil {
			return err
		}
	} else {
		for _, p := range []struct{ name, reported, key string }{{"state", paths.State, "XDG_STATE_HOME"}, {"config", paths.Config, "XDG_CONFIG_HOME"}} {
			root := values[p.key]
			resolved, resolveErr := filepath.EvalSymlinks(root)
			rel, e := filepath.Rel(filepath.Dir(data), resolved)
			if !filepath.IsAbs(root) || resolveErr != nil || e != nil || !filepath.IsLocal(rel) {
				return fmt.Errorf("opencode %s root %q is not inside private home %q", p.name, root, filepath.Dir(data))
			}
			if err = verify(p.name, p.reported, filepath.Join(resolved, "opencode")); err != nil {
				return err
			}
		}
	}
	db, err := os.Lstat(filepath.Join(data, "opencode/opencode.db"))
	if err != nil {
		return err
	}
	if !db.Mode().IsRegular() {
		return fmt.Errorf("opencode database is not private regular storage")
	}
	return nil
}
