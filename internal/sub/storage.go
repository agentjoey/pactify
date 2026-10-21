package sub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/procx"
)

// StoragePolicy follows router.json's home-level, private JSON convention.
// Durations are strings so an operator can edit the file without nanoseconds.
type StoragePolicy struct {
	OlderThan     string `json:"olderThan"`
	CacheMaxBytes int64  `json:"cacheMaxBytes"`
	Interval      string `json:"interval"`
}

func DefaultStoragePolicy() StoragePolicy { return StoragePolicy{"336h", 5_000_000_000, "24h"} }

type PruneOptions struct {
	OlderThan        time.Duration `json:"-"`
	CacheMaxBytes    int64         `json:"cacheMaxBytes,omitempty"`
	IncludeWorktrees bool          `json:"includeWorktrees,omitempty"`
	ExpectedVersion  string        `json:"-"`
	scanLimit        int           // Status polling only; explicit plans use the caller's context.
}
type StorageItem struct {
	Path      string `json:"path"`
	Bytes     int64  `json:"bytes"`
	Reason    string `json:"reason"`
	Protected bool   `json:"protected"`
	Chain     string `json:"chain,omitempty"`
	Session   string `json:"session,omitempty"`
	Action    string `json:"action,omitempty"`
	modified  time.Time
}
type StorageLocation struct {
	Summaries      []StorageReasonSummary `json:"summaries,omitempty"`
	Name           string                 `json:"name"`
	Bytes          int64                  `json:"bytes"`
	Reclaimable    int64                  `json:"reclaimable"`
	Items          []StorageItem          `json:"items,omitempty"`
	Count          int                    `json:"count"`
	ProtectedCount int                    `json:"protectedCount"`
	Breakdown      map[string]int64       `json:"breakdown,omitempty"`
}
type StoragePlan struct {
	Version        string            `json:"version"`
	ComputedAt     time.Time         `json:"computedAt"`
	Policy         StoragePolicy     `json:"policy"`
	Locations      []StorageLocation `json:"locations"`
	StaleWorktrees []string          `json:"staleWorktrees"`
	Failures       []string          `json:"failures"`
}
type StorageRemoved struct {
	DatabaseMeasured bool   `json:"databaseMeasured,omitempty"`
	Sessions         int    `json:"sessions,omitempty"`
	DatabaseBytes    int64  `json:"databaseBytes,omitempty"`
	Location         string `json:"location"`
	Bytes            int64  `json:"bytes"`
	Items            int    `json:"items"`
}
type StorageRun struct {
	StartedAt time.Time        `json:"startedAt"`
	EndedAt   time.Time        `json:"endedAt,omitzero"`
	Removed   []StorageRemoved `json:"removed"`
	Failures  []string         `json:"failures"`
}
type StorageSnapshot struct {
	StoragePlan
	LastRun *StorageRun `json:"lastRun"`
}

func (s Store) storageHome() string { return filepath.Dir(s.Root) }

// Resolve the configured home once, then anchor traversal and removal to Root.
// OS aliases and a symlinked PACTIFY_HOME are supported.
func (s Store) openStorage() (*os.Root, error) {
	p, err := filepath.Abs(s.storageHome())
	if err != nil {
		return nil, err
	}
	p, err = filepath.EvalSymlinks(p)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(p)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("storage home must be a directory")
	}
	r, err := os.OpenRoot(p)
	if err != nil {
		return nil, err
	}
	opened, err := r.Stat(".")
	if err != nil || !os.SameFile(info, opened) {
		r.Close()
		return nil, fmt.Errorf("storage home changed")
	}
	return r, nil
}
func storageCredential(path string) bool {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 0 || parts[0] != "harness-homes" {
		return false
	}
	for _, part := range parts {
		switch strings.ToLower(part) {
		case "credentials", "oauth", "auth.json", "device_id", "secrets", "antigravity-oauth-token", "config.toml":
			return true
		}
	}
	return false
}
func storageRealPath(r *os.Root, path string) error {
	if !filepath.IsLocal(path) || path == "." {
		return fmt.Errorf("unsafe storage path %q", path)
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	for i := range parts {
		p := filepath.Join(parts[:i+1]...)
		info, err := r.Lstat(p)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink protected: %s", p)
		}
	}
	return nil
}
func storageReadBytes(r *os.Root, path string) ([]byte, error) {
	if e := storageRealPath(r, path); e != nil {
		return nil, e
	}
	parent, name, e := storageParent(r, path)
	if e != nil {
		return nil, e
	}
	defer parent.Close()
	f, e := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<20 {
		return nil, fmt.Errorf("invalid storage JSON: %s", path)
	}
	return io.ReadAll(io.LimitReader(f, 8<<20))
}
func storageReadJSON(r *os.Root, path string, out any) error {
	raw, e := storageReadBytes(r, path)
	if e != nil {
		return e
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if e = d.Decode(out); e != nil {
		return e
	}
	if d.Decode(new(any)) != io.EOF {
		return fmt.Errorf("invalid trailing JSON: %s", path)
	}
	return nil
}

func (s Store) storageState(r *os.Root, id string) (State, error) {
	var st State
	if err := validJobID(id); err != nil {
		return st, err
	}
	if err := storageReadJSON(r, filepath.Join(filepath.Base(s.Root), id, "state.json"), &st); err != nil {
		return st, err
	}
	if st.ID != id {
		return st, fmt.Errorf("job state id does not match directory %s", id)
	}
	return st, nil
}
func (s Store) storageStates() ([]State, error) { return s.storageStatesContext(context.Background()) }
func (s Store) storageStatesContext(ctx context.Context) ([]State, error) {
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	r, err := s.openStorage()
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer r.Close()
	entries, err := storageEntries(r, filepath.Base(s.Root))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	states := []State{}
	for _, entry := range entries {
		if e := ctx.Err(); e != nil {
			return nil, e
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("symlinked job entry protected")
		}
		if !entry.IsDir() {
			continue
		}
		st, e := s.storageState(r, entry.Name())
		if e != nil {
			if os.IsNotExist(e) && storageCrashLeftover(r, filepath.Join(filepath.Base(s.Root), entry.Name()), time.Now()) {
				continue
			}
			return nil, e
		}
		states = append(states, st)
	}
	return states, nil
}
func (s Store) checkStorageTrigger(id string) error {
	r, err := s.openStorage()
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = s.storageState(r, id)
	return err
}

func (s Store) storageResult(r *os.Root, st State) (Result, error) {
	var result Result
	if err := storageReadJSON(r, filepath.Join(filepath.Base(s.Root), st.ID, "result.json"), &result); err != nil {
		return result, err
	}
	if result.JobID != "" && result.JobID != st.ID || result.SessionID != "" && result.JobID != st.ID {
		return result, fmt.Errorf("result session ownership does not match job %s", st.ID)
	}
	if result.Harness != "" && result.Harness != st.Request.Harness {
		return result, fmt.Errorf("result harness does not match job %s", st.ID)
	}
	if result.Status != "" && result.Status != st.Status {
		return result, fmt.Errorf("result status does not match job %s", st.ID)
	}
	return result, nil
}
func storagePolicy(r *os.Root, opts PruneOptions) (StoragePolicy, time.Duration, error) {
	p := DefaultStoragePolicy()
	if r != nil {
		var raw json.RawMessage
		if err := storageReadJSON(r, "storage.json", &raw); err != nil && !os.IsNotExist(err) {
			return p, 0, fmt.Errorf("storage.json: %w", err)
		}
		if len(raw) > 0 {
			if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				return p, 0, fmt.Errorf("storage.json must contain an object")
			}
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if err := d.Decode(&p); err != nil {
				return p, 0, fmt.Errorf("invalid storage.json: %w", err)
			}
		}
	}
	age, err := time.ParseDuration(p.OlderThan)
	if err != nil || age < 24*time.Hour {
		return p, 0, fmt.Errorf("storage olderThan must be at least 24h")
	}
	interval, err := time.ParseDuration(p.Interval)
	if err != nil || interval <= 0 {
		return p, 0, fmt.Errorf("storage interval must be a positive duration")
	}
	if p.CacheMaxBytes < 1_000_000_000 {
		return p, 0, fmt.Errorf("storage cacheMaxBytes must be at least 1000000000")
	}
	if opts.OlderThan < 0 || opts.CacheMaxBytes < 0 {
		return p, 0, fmt.Errorf("prune limits must be positive")
	}
	if opts.OlderThan > 0 {
		age = opts.OlderThan
		p.OlderThan = age.String()
	}
	if opts.CacheMaxBytes > 0 {
		p.CacheMaxBytes = opts.CacheMaxBytes
	}
	return p, age, nil
}

// A bounded metadata walk never opens credentials or follows links. A partial
// scan cannot authorize any deletion. The limit also bounds status polling.
type storageScanner struct {
	ctx       context.Context
	root      *os.Root
	remaining int
}

func (w *storageScanner) tree(path string) (size int64, latest time.Time, err error) {
	if err = storageRealPath(w.root, path); err != nil {
		return
	}
	var walk func(string) error
	walk = func(p string) error {
		w.remaining--
		if w.remaining < 0 {
			return fmt.Errorf("storage scan exceeds 200000 entries")
		}
		if e := w.ctx.Err(); e != nil {
			return e
		}
		info, e := w.root.Lstat(p)
		if e != nil {
			return e
		}
		if info.Mode()&os.ModeSymlink != 0 {
			parts := strings.Split(filepath.ToSlash(p), "/")
			if len(parts) > 3 && (parts[0] == "jobs" || parts[0] == "trash") && parts[2] == "wt" {
				size += info.Size()
				return nil
			}
			return fmt.Errorf("symlink protected: %s", p)
		}
		if storageCredential(p) {
			return fmt.Errorf("credential protected: %s", p)
		}
		if info.Mode().IsRegular() {
			size += info.Size()
			latest = maxStorageTime(latest, storageAccessTime(info))
			return nil
		}
		if !info.IsDir() {
			return fmt.Errorf("special file protected: %s", p)
		}
		entries, e := storageEntries(w.root, p)
		if e != nil {
			return e
		}
		for _, entry := range entries {
			if e = walk(filepath.Join(p, entry.Name())); e != nil {
				return e
			}
		}
		return nil
	}
	err = walk(path)
	return
}
func maxStorageTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return b
	}
	return a
}
func storageEntries(r *os.Root, p string) ([]os.DirEntry, error) {
	if err := storageRealPath(r, p); err != nil {
		return nil, err
	}
	parent, name, err := storageParent(r, p)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	d, err := parent.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_DIRECTORY, 0)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	entries, err := d.ReadDir(200001)
	if err == io.EOF {
		err = nil
	}
	if len(entries) > 200000 {
		return nil, fmt.Errorf("storage directory exceeds scan limit: %s", p)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, err
}
func storageJobReason(st State, now time.Time, age time.Duration, include bool) string {
	if !Terminal(st.Status) {
		return "running, queued, or unknown state"
	}
	if reason := storageProcessReason(st); reason != "" {
		return reason
	}
	if st.FinishedAt.IsZero() || !st.FinishedAt.Before(now.Add(-age)) {
		return "finish time is missing or within retention"
	}
	if st.Worktree != nil && !st.Worktree.Applied && !st.Worktree.Discarded && !include {
		return "pending worktree; explicit --include-worktrees required"
	}
	return ""
}
func storageProcessReason(st State) string {
	if st.ProcessTrackingError != "" {
		return "process ownership is uncertain"
	}
	for _, p := range append(append([]procx.Identity(nil), st.Processes...), st.Descendants...) {
		alive, err := p.Alive()
		if err != nil || alive {
			return "harness process may still be running"
		}
	}
	if st.Worker != nil {
		alive, err := st.Worker.Alive()
		if err != nil || alive {
			return "worker may still be running"
		}
	}
	return ""
}

func (s Store) PlanStorage(ctx context.Context, opts PruneOptions) (StoragePlan, error) {
	r, err := s.openStorage()
	if os.IsNotExist(err) {
		r = nil
	} else if err != nil {
		return StoragePlan{}, err
	}
	if r != nil {
		defer r.Close()
	}
	return s.planStorage(ctx, r, opts, time.Now().UTC())
}
func (s Store) planStorage(ctx context.Context, r *os.Root, opts PruneOptions, now time.Time) (p StoragePlan, err error) {
	defer func() {
		if err == nil {
			for li := range p.Locations {
				l := &p.Locations[li]
				l.Breakdown = map[string]int64{}
				l.Summaries = SummarizeStorage(l.Items)
				l.Count = len(l.Items)
				for _, item := range l.Items {
					if item.Protected {
						l.ProtectedCount++
					}
					parts := strings.Split(filepath.ToSlash(item.Path), "/")
					if len(parts) > 2 && li > 0 {
						l.Breakdown[parts[1]] += item.Bytes
					}
				}
			}
			copy := p
			copy.ComputedAt = time.Time{}
			copy.Version = ""
			data, _ := json.Marshal(copy)
			p.Version = fmt.Sprintf("%x", sha256.Sum256(data))
		}
	}()
	policy, age, err := storagePolicy(r, opts)
	p = StoragePlan{ComputedAt: now, Policy: policy, Locations: []StorageLocation{{Name: "jobs", Items: []StorageItem{}}, {Name: "cache", Items: []StorageItem{}}, {Name: "harness-homes", Items: []StorageItem{}}}, StaleWorktrees: []string{}, Failures: []string{}}
	if err != nil {
		return p, err
	}
	if r == nil {
		return p, nil
	}
	walkCtx := ctx
	cancel := func() {}
	limit := opts.scanLimit
	if limit > 0 {
		walkCtx, cancel = context.WithTimeout(ctx, 5*time.Second)
	} else {
		limit = int(^uint(0) >> 1)
	}
	defer cancel()
	scan := storageScanner{walkCtx, r, limit}
	states := map[string]State{}
	reasons := map[string]string{}
	sessions := map[string]string{}
	groups := map[string][]string{}
	active := false
	uncertain := false
	blockAll := false
	rawProtected := map[string]bool{}
	unreadableReferences := []map[string]bool{}
	entries, err := storageEntries(r, filepath.Base(s.Root))
	if err != nil && !os.IsNotExist(err) {
		return p, err
	}
	existing := map[string]bool{}
	for _, entry := range entries {
		existing[entry.Name()] = true
	}
	for _, entry := range entries {
		if walkCtx.Err() != nil || scan.remaining < 0 {
			uncertain = true
			blockAll = true
			p.Failures = append(p.Failures, "job inventory scan budget exceeded")
			break
		}
		scan.remaining--
		id := entry.Name()
		rel := filepath.Join(filepath.Base(s.Root), id)
		if !entry.IsDir() {
			if entry.Type()&os.ModeSymlink != 0 {
				p.Locations[0].Items = append(p.Locations[0].Items, StorageItem{Path: rel, Protected: true, Reason: "symlink protected"})
				uncertain = true
				blockAll = true
			}
			continue
		}
		st, e := s.storageState(r, id)
		if e != nil {
			info, statErr := r.Lstat(rel)
			if os.IsNotExist(e) && statErr == nil && info.ModTime().Before(now.Add(-age)) && validJobID(id) == nil {
				p.Locations[0].Items = append(p.Locations[0].Items, StorageItem{Path: rel, Chain: id, Action: "incomplete-job", Reason: "incomplete job directory older than retention"})
				continue
			}
			if os.IsNotExist(e) && storageCrashLeftover(r, rel, now) {
				p.Locations[0].Items = append(p.Locations[0].Items, StorageItem{Path: rel, Protected: true, Reason: "crash leftover without job state; one-hour creation grace elapsed"})
				continue
			}
			refs, refErr := storageRawReferences(r, filepath.Join(rel, "state.json"), id, existing)
			if refErr != nil || len(refs) == 0 {
				blockAll = true
			}
			unreadableReferences = append(unreadableReferences, refs)
			uncertain = true
			p.Failures = append(p.Failures, rel+": "+e.Error())
			reasons[id] = "job state cannot be verified"
		} else {
			states[id] = st
			reasons[id] = storageJobReason(st, now, age, opts.IncludeWorktrees)
			if !Terminal(st.Status) || st.ProcessTrackingError != "" {
				active = true
			}
			if strings.Contains(reasons[id], "may still be running") {
				active = true
			}
			if st.Worktree != nil && !st.Worktree.Applied && !st.Worktree.Discarded && !st.FinishedAt.IsZero() && st.FinishedAt.Before(now.Add(-age)) {
				p.StaleWorktrees = append(p.StaleWorktrees, id)
			}
			result, e := s.storageResult(r, st)
			if e != nil {
				if !os.IsNotExist(e) || Terminal(st.Status) {
					reasons[id] = "result/session ownership cannot be verified"
					uncertain = true
					p.Failures = append(p.Failures, rel+": "+e.Error())
				}
			} else {
				sessions[id] = result.SessionID
			}
		}
		p.Locations[0].Items = append(p.Locations[0].Items, StorageItem{Path: rel})
	}
	// Inspect the small state inventory before walking potentially large
	// worktrees. A size limit must not hide other stale/active jobs.
	for i := range p.Locations[0].Items {
		item := &p.Locations[0].Items[i]
		if item.Protected {
			continue
		}
		size, latest, e := scan.tree(item.Path)
		if e != nil {
			reasons[filepath.Base(item.Path)] = e.Error()
		} else if item.Action == "incomplete-job" && !latest.IsZero() && !latest.Before(now.Add(-age)) {
			reasons[filepath.Base(item.Path)] = "incomplete job contains recent files"
		}
		p.Locations[0].Bytes += size
		item.Bytes = size
	}
	// Only verified states identify existing chains. Store metadata files and
	// crash leftovers cannot make an unreadable job's ownership look bounded.
	for _, refs := range unreadableReferences {
		bounded := false
		for ref := range refs {
			if _, ok := states[ref]; ok {
				rawProtected[ref] = true
				bounded = true
			}
		}
		if !bounded {
			blockAll = true
		}
	}
	for id := range rawProtected {
		reasons[id] = "unreadable job may reference this chain"
	}
	if blockAll {
		for _, item := range p.Locations[0].Items {
			reasons[filepath.Base(item.Path)] = "unreadable job ownership cannot be bounded; all job deletions protected"
		}
	}
	// Union explicit resume/fallback/worktree relations and reused native sessions.
	parent := map[string]string{}
	for id := range states {
		parent[id] = id
	}
	var find func(string) string
	find = func(id string) string {
		if parent[id] != id {
			parent[id] = find(parent[id])
		}
		return parent[id]
	}
	join := func(a, b string) {
		if _, ok := parent[b]; ok {
			a, b = find(a), find(b)
			if a > b {
				a, b = b, a
			}
			parent[b] = a
		} else if b != "" {
			reasons[a] = "resume/worktree chain member is missing"
			reasons[b] = "referenced chain member has unverifiable state"
		}
	}
	native := map[string]string{}
	for id, st := range states {
		for _, link := range []string{st.ResumeOf, st.FallbackFrom, st.FallbackTo} {
			if link != "" {
				join(id, link)
			}
		}
		if st.Worktree != nil {
			join(id, st.Worktree.Owner)
			if st.Worktree.Holder != "" {
				join(id, st.Worktree.Holder)
			}
		}
		for _, sid := range []string{sessions[id], st.Request.Resume} {
			if sid != "" {
				key := st.Request.Harness + "\x00" + sid
				if other := native[key]; other != "" {
					join(id, other)
				}
				native[key] = id
			}
		}
	}
	for id := range states {
		key := find(id)
		groups[key] = append(groups[key], id)
	}
	for id := range states {
		seen := map[string]bool{}
		for at := id; at != ""; at = states[at].ResumeOf {
			if seen[at] {
				reasons[id] = "resume chain contains a cycle"
				break
			}
			seen[at] = true
			if _, ok := states[at]; !ok {
				break
			}
		}
	}
	// A queued worker has not necessarily acquired its home lease yet. Protect
	// session-owning jobs before grouping, so an expired sibling cannot mutate
	// that shared native home while startup is in flight.
	if active {
		for id, st := range states {
			if reasons[id] == "" && st.Request.Harness != "codex" && (sessions[id] != "" || st.Request.Resume != "") {
				reasons[id] = "native session home protected by active or uncertain jobs"
			}
		}
	}
	for _, ids := range groups {
		sort.Strings(ids)
		reason := ""
		for _, id := range ids {
			if reasons[id] != "" {
				reason = "chain protected: " + id + ": " + reasons[id]
				break
			}
		}
		for _, id := range ids {
			reasons[id] = reason
		}
	}
	for i := range p.Locations[0].Items {
		item := &p.Locations[0].Items[i]
		id := filepath.Base(item.Path)
		if st, ok := states[id]; ok {
			_ = st
			item.Chain = find(id)
		}
		if reason := reasons[id]; reason != "" {
			item.Protected = true
			item.Reason = reason
		} else if !item.Protected {
			if item.Reason == "" {
				item.Reason = "finished resume chain older than retention"
			}
			p.Locations[0].Reclaimable += item.Bytes
		}
	}
	busy := ""
	if active {
		busy = "running or queued jobs protect shared caches and homes"
	}
	if uncertain {
		busy = "job inventory incomplete; shared storage protected"
	}
	s.planCache(&p, &scan, busy)
	s.planHomes(&p, &scan, states, sessions, now.Add(-age), busy)
	s.planStorageTrash(&p, &scan)
	if scan.remaining < 0 || walkCtx.Err() != nil {
		p.Failures = append(p.Failures, "storage inventory exceeded its scan budget")
		for li := range p.Locations {
			l := &p.Locations[li]
			l.Reclaimable = 0
			for i := range l.Items {
				l.Items[i].Protected = true
				l.Items[i].Reason = "partial storage scan cannot authorize deletion"
			}
		}
	}
	sort.Strings(p.StaleWorktrees)
	return p, nil
}

func (s Store) planCache(p *StoragePlan, scan *storageScanner, busy string) {
	l := &p.Locations[1]
	var visit func(string)
	visit = func(path string) {
		scan.remaining--
		if scan.remaining < 0 {
			p.Failures = append(p.Failures, "cache scan limit reached")
			return
		}
		if err := scan.ctx.Err(); err != nil {
			p.Failures = append(p.Failures, err.Error())
			return
		}
		info, err := scan.root.Lstat(path)
		if os.IsNotExist(err) {
			return
		}
		if err != nil {
			p.Failures = append(p.Failures, path+": "+err.Error())
			return
		}
		// A module is one eviction unit: partial extracted modules confuse Go.
		unit := !info.IsDir() || strings.Contains(filepath.Base(path), "@")
		if unit {
			size, latest, e := scan.tree(path)
			item := StorageItem{Path: path, Bytes: size, modified: latest}
			if e != nil {
				item.Protected = true
				item.Reason = e.Error()
			}
			if busy != "" {
				item.Protected = true
				item.Reason = busy
			}
			// Tools coordinate through these files (go-mod/cache/lock, npm *.lock);
			// removing one while held lets a second process take a fresh lock.
			if name := filepath.Base(path); !info.IsDir() && (name == "lock" || strings.HasSuffix(name, ".lock")) {
				item.Protected = true
				item.Reason = "tool lock file"
			}
			l.Items = append(l.Items, item)
			l.Bytes += size
			return
		}
		entries, e := storageEntries(scan.root, path)
		if e != nil {
			p.Failures = append(p.Failures, path+": "+e.Error())
			return
		}
		for _, entry := range entries {
			visit(filepath.Join(path, entry.Name()))
		}
	}
	visit("cache")
	sort.SliceStable(l.Items, func(i, j int) bool {
		if l.Items[i].modified.Equal(l.Items[j].modified) {
			return l.Items[i].Path < l.Items[j].Path
		}
		return l.Items[i].modified.Before(l.Items[j].modified)
	})
	remaining := l.Bytes
	for i := range l.Items {
		item := &l.Items[i]
		if item.Protected {
			continue
		}
		if l.Bytes > p.Policy.CacheMaxBytes && remaining > p.Policy.CacheMaxBytes/5*4 {
			item.Reason = "least recently used cache entry; cap exceeded"
			l.Reclaimable += item.Bytes
			remaining -= item.Bytes
		} else {
			item.Protected = true
			item.Reason = "cache below eviction target"
		}
	}
}

// File-level deletion rejects links and credentials again immediately before
// mutation. Root keeps concurrent path replacements confined to this home.
func storageRemove(r *os.Root, path string, expected ...os.FileInfo) error {
	if err := storageRealPath(r, path); err != nil {
		return err
	}
	parent, name, err := storageParent(r, path)
	if err != nil {
		return err
	}
	defer parent.Close()
	return storageRemoveNode(parent, name, path, expected...)
}
func storageRemoveNode(r *os.Root, name, path string, expected ...os.FileInfo) error {
	if storageCredential(path) {
		return fmt.Errorf("credential protected: %s", path)
	}
	info, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if len(expected) > 0 && !os.SameFile(info, expected[0]) {
		return fmt.Errorf("storage item changed: %s", path)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		parts := strings.Split(filepath.ToSlash(path), "/")
		if len(parts) > 3 && parts[0] == "trash" && parts[2] == "wt" {
			return r.Remove(name)
		}
		return fmt.Errorf("symlink protected: %s", path)
	}
	if info.IsDir() {
		d, err := r.OpenRoot(name)
		if err != nil {
			return err
		}
		defer d.Close()
		current, err := d.Stat(".")
		if err != nil || !os.SameFile(info, current) {
			return fmt.Errorf("directory changed: %s", path)
		}
		if err = d.Chmod(".", info.Mode().Perm()|0700); err != nil {
			return err
		}
		file, err := d.Open(".")
		if err != nil {
			return err
		}
		entries, err := file.ReadDir(-1)
		file.Close()
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err = storageRemoveNode(d, entry.Name(), filepath.Join(path, entry.Name())); err != nil {
				return err
			}
		}
	}
	current, err := r.Lstat(name)
	if err != nil {
		return err
	}
	if !os.SameFile(info, current) {
		return fmt.Errorf("storage item changed: %s", path)
	}
	return r.Remove(name)
}

// Open each ancestor relative to its verified descriptor. Keeping these
// handles avoids following a replacement intermediate symlink during mutation.
func storageParent(r *os.Root, path string) (*os.Root, string, error) {
	if !filepath.IsLocal(path) || path == "." {
		return nil, "", fmt.Errorf("unsafe storage path")
	}
	parts := strings.Split(filepath.Clean(path), string(filepath.Separator))
	parent, err := r.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	for _, part := range parts[:len(parts)-1] {
		info, e := parent.Lstat(part)
		if e != nil {
			parent.Close()
			return nil, "", e
		}
		if !info.IsDir() {
			parent.Close()
			return nil, "", fmt.Errorf("non-directory or symlink ancestor protected")
		}
		next, e := parent.OpenRoot(part)
		if e != nil {
			parent.Close()
			return nil, "", e
		}
		opened, e := next.Stat(".")
		after, afterErr := parent.Lstat(part)
		parent.Close()
		if e != nil || afterErr != nil || !os.SameFile(info, opened) || !after.IsDir() || !os.SameFile(info, after) {
			next.Close()
			return nil, "", fmt.Errorf("storage ancestor changed")
		}
		parent = next
	}
	return parent, parts[len(parts)-1], nil
}

// One lock serializes prune processes; a separate gate excludes job creation
// and resume registration while prune takes its complete ownership snapshot.
func storageLock(ctx context.Context, r *os.Root, path string, mode int) (func(), error) {
	if err := storageRealPath(r, path); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	parent, name, err := storageParent(r, path)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	// Open an existing inode first. Concurrent O_CREAT without O_EXCL can
	// return ENOENT on APFS during initial creation; exclusive create followed
	// by opening the winner also makes the ownership decision explicit.
	// O_NONBLOCK keeps a planted FIFO from blocking the open; the regular-file
	// check rejects it (Linux opens and flocks FIFOs with O_RDWR).
	f, err := parent.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if os.IsNotExist(err) {
		f, err = parent.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if os.IsExist(err) {
			f, err = parent.OpenFile(name, os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
		}
	}
	if err != nil {
		return nil, err
	}
	if info, e := f.Stat(); e != nil || !info.Mode().IsRegular() {
		f.Close()
		if e == nil {
			e = fmt.Errorf("storage lock %s is not a regular file", path)
		}
		return nil, e
	}
	for {
		err = syscall.Flock(int(f.Fd()), mode|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }, nil
		}
		if err != syscall.EWOULDBLOCK {
			f.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			f.Close()
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
func (s Store) storageGate(ctx context.Context) (func(), error) {
	noop := func() {}
	if err := os.MkdirAll(s.storageHome(), 0700); err != nil {
		return noop, nil
	}
	r, err := s.openStorage()
	if err != nil {
		return noop, nil
	}
	defer r.Close()
	wait, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	release, err := storageLock(wait, r, "storage-jobs.lock", syscall.LOCK_SH)
	if err != nil {
		return noop, nil
	}
	return release, nil
}

type storageDeps struct {
	idleCheck      func(context.Context) (bool, error)
	deleteSessions func(context.Context, []State) error
	removeJob      func(*os.Root, string, os.FileInfo) error
	due            bool
	trigger        string
}

var ErrStoragePlanChanged = errors.New("storage changed; review a new cleanup plan")

func (s Store) PruneStorage(ctx context.Context, opts PruneOptions) (StorageRun, error) {
	return s.pruneStorage(ctx, opts, storageDeps{})
}
func (s Store) PruneStorageDue(ctx context.Context) (StorageRun, error) {
	return s.pruneStorage(ctx, PruneOptions{}, storageDeps{due: true})
}
func (s Store) pruneStorage(ctx context.Context, opts PruneOptions, deps storageDeps) (run StorageRun, err error) {
	r, err := s.openStorage()
	if os.IsNotExist(err) {
		if deps.due {
			return StorageRun{}, nil
		}
		if err = os.MkdirAll(s.storageHome(), 0700); err != nil {
			return run, err
		}
		r, err = s.openStorage()
	}
	if err != nil {
		return run, err
	}
	defer r.Close()
	checkTrigger := func() error {
		if deps.trigger == "" {
			return nil
		}
		_, e := s.storageState(r, deps.trigger)
		return e
	}
	if err = checkTrigger(); err != nil {
		return run, err
	}
	release, err := storageLock(ctx, r, "storage-prune.lock", syscall.LOCK_EX)
	if err != nil {
		return run, err
	}
	defer release()
	if deps.due {
		policy, _, e := storagePolicy(r, PruneOptions{})
		if e != nil {
			return run, e
		}
		var last StorageRun
		if e = storageReadJSON(r, "storage-status.json", &last); e != nil && !os.IsNotExist(e) {
			return run, e
		}
		interval, _ := time.ParseDuration(policy.Interval)
		if !last.StartedAt.IsZero() && time.Since(last.StartedAt) < interval {
			return run, nil
		}
	}
	gate, err := storageLock(ctx, r, "storage-jobs.lock", syscall.LOCK_EX)
	if err != nil {
		return run, err
	}
	defer func() {
		if gate != nil {
			gate()
		}
	}()
	if err = checkTrigger(); err != nil {
		return run, err
	}
	if deps.due {
		idle, e := s.storageIdleContext(ctx)
		if e != nil {
			return run, e
		}
		if !idle {
			return run, nil
		}
	}
	plan, planErr := s.planStorage(ctx, r, opts, time.Now().UTC())
	if planErr == nil && opts.ExpectedVersion != "" && opts.ExpectedVersion != plan.Version {
		return run, ErrStoragePlanChanged
	}
	run = StorageRun{StartedAt: time.Now().UTC(), Removed: []StorageRemoved{{Location: "jobs"}, {Location: "cache"}, {Location: "harness-homes"}}, Failures: []string{}}
	if err = storageWriteJSON(r, "storage-status.json", run); err != nil {
		return run, err
	}
	defer func() {
		run.EndedAt = time.Now().UTC()
		if err != nil {
			run.Failures = append(run.Failures, err.Error())
		}
		err = errors.Join(err, storageWriteJSON(r, "storage-status.json", run))
	}()
	if planErr != nil {
		return run, planErr
	}
	gate()
	gate = nil
	run.Failures = append(run.Failures, plan.Failures...)
	groups := map[string][]StorageItem{}
	var keys []string
	for _, item := range plan.Locations[0].Items {
		if !item.Protected {
			if _, ok := groups[item.Chain]; !ok {
				keys = append(keys, item.Chain)
			}
			groups[item.Chain] = append(groups[item.Chain], item)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		if e := s.pruneStorageChain(ctx, r, groups[key], opts, deps, &run); e != nil {
			run.Failures = append(run.Failures, key+": "+e.Error())
		}
	}
	// Homes/cache are protected whenever a queued or running job exists. Their
	// own native usage leases also exclude login and managed-home maintenance.
	checkIdle := deps.idleCheck
	if checkIdle == nil {
		checkIdle = s.storageIdleContext
	}
	pass := storageIdlePass{root: r, jobs: filepath.Base(s.Root), check: checkIdle}
	for li := 1; li < len(plan.Locations); li++ {
		for _, item := range plan.Locations[li].Items {
			if item.Protected {
				continue
			}
			removed, e := s.pruneStorageItem(ctx, r, item, li, pass.allow)
			storageAddRemoved(&run.Removed[li], removed)
			if e != nil {
				run.Failures = append(run.Failures, item.Path+": "+e.Error())
				if pass.err != nil || ctx.Err() != nil {
					break
				}
				continue
			}
		}
	}
	return run, nil
}
func (s Store) pruneStorageChain(ctx context.Context, r *os.Root, items []StorageItem, opts PruneOptions, deps storageDeps, run *StorageRun) error {
	sort.Slice(items, func(i, j int) bool { return items[i].Path > items[j].Path })
	identities := map[string]os.FileInfo{}
	for _, item := range items {
		if e := storageRealPath(r, item.Path); e != nil {
			return e
		}
		info, e := r.Lstat(item.Path)
		if e != nil {
			return e
		}
		identities[item.Path] = info
	}
	if len(items) == 1 && items[0].Action == "trash-marker" {
		item := items[0]
		var marker struct {
			Path string `json:"path"`
		}
		if e := storageReadJSON(r, item.Path, &marker); e != nil {
			return e
		}
		if marker.Path != strings.TrimSuffix(item.Path, ".json") {
			return fmt.Errorf("unverified trash marker")
		}
		if _, e := r.Lstat(marker.Path); !os.IsNotExist(e) {
			return fmt.Errorf("trash marker acquired a target")
		}
		if !identities[item.Path].ModTime().Before(time.Now().Add(-time.Hour)) {
			return fmt.Errorf("trash marker within creation grace")
		}
		if e := storageRemove(r, item.Path, identities[item.Path]); e != nil {
			return e
		}
		run.Removed[0].Items++
		run.Removed[0].Bytes += identities[item.Path].Size()
		return nil
	}
	if len(items) == 1 && items[0].Action == "trash-job" {
		item := items[0]
		var marker struct {
			Path string `json:"path"`
		}
		if e := storageReadJSON(r, item.Path+".json", &marker); e != nil {
			return e
		}
		if marker.Path != item.Path {
			return fmt.Errorf("unverified trash entry")
		}
		size, _, e := (&storageScanner{ctx, r, 200000}).tree(item.Path)
		if e != nil {
			return e
		}
		if e = storageRemove(r, item.Path, identities[item.Path]); e != nil {
			return e
		}
		run.Removed[0].Items++
		run.Removed[0].Bytes += size
		return r.Remove(item.Path + ".json")
	}
	if len(items) == 1 && items[0].Action == "incomplete-job" {
		gate, e := storageLock(ctx, r, "storage-jobs.lock", syscall.LOCK_EX)
		if e != nil {
			return e
		}
		defer gate()
		item := items[0]
		if _, e = r.Lstat(filepath.Join(item.Path, "state.json")); !os.IsNotExist(e) {
			return fmt.Errorf("incomplete job acquired state")
		}
		size, _, e := (&storageScanner{ctx, r, 200000}).tree(item.Path)
		if e != nil {
			return e
		}
		if e = storageTrashJob(r, item.Path, identities[item.Path]); e != nil {
			return e
		}
		run.Removed[0].Items++
		run.Removed[0].Bytes += size
		return nil
	}
	resumeRelease, e := s.storageChainResumeLock(ctx, r, items)
	if e != nil {
		return e
	}
	defer resumeRelease()
	var states []State
	// The resume-scope lock excludes resumes. Discard retains its established
	// worktree/state lock order; never call it while holding a state lock.
	for _, item := range items {
		st, err := s.storageState(r, filepath.Base(item.Path))
		if err != nil {
			return err
		}
		if _, err = s.storageResult(r, st); err != nil {
			return err
		}
		if st.Worktree != nil && !st.Worktree.Discarded {
			if err = validJobID(st.Worktree.Owner); err != nil {
				return err
			}
			if !st.Worktree.Applied && !opts.IncludeWorktrees {
				return fmt.Errorf("pending worktree protected")
			}
			if err = storageRealPath(r, filepath.Join(filepath.Base(s.Root), st.Worktree.Owner)); err != nil {
				return err
			}
			expected := filepath.Join(s.Dir(st.Worktree.Owner), "wt")
			a, _ := filepath.Abs(st.Worktree.Path)
			b, _ := filepath.Abs(expected)
			if a != b {
				return fmt.Errorf("unverified worktree path")
			}
			if _, err = os.Lstat(st.Worktree.Path); err == nil {
				if e := storageRealPath(r, filepath.Join(filepath.Base(s.Root), st.Worktree.Owner, "wt")); e != nil {
					return e
				}
			}
			holder := st.ID
			if st.Worktree.Holder != "" {
				holder = st.Worktree.Holder
			}
			if err = s.DiscardWorktree(holder); err != nil {
				return err
			}
		}
	}
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	policy, age, err := storagePolicy(r, opts)
	_ = policy
	if err != nil {
		return err
	}
	for _, item := range items {
		release, err := storageLock(ctx, r, filepath.Join(item.Path, "state.lock"), syscall.LOCK_EX)
		if err != nil {
			return err
		}
		releases = append(releases, release)
		st, e := s.storageState(r, filepath.Base(item.Path))
		if e != nil {
			return e
		}
		if reason := storageJobReason(st, time.Now(), age, false); reason != "" {
			return fmt.Errorf("%s: %s", st.ID, reason)
		}
		scan := storageScanner{ctx, r, 200000}
		if _, _, err = scan.tree(item.Path); err != nil {
			return err
		}
		states = append(states, st)
	}
	if err = s.storageChainUnchanged(r, states, identities); err != nil {
		return err
	}
	if deps.deleteSessions != nil {
		err = deps.deleteSessions(ctx, states)
	} else {
		removed, e := s.deleteStorageSessionsMeasured(ctx, states)
		storageAddRemoved(&run.Removed[2], removed)
		err = e
	}
	if err != nil {
		return err
	}
	gate, e := storageLock(ctx, r, "storage-jobs.lock", syscall.LOCK_EX)
	if e != nil {
		return e
	}
	defer gate()
	if e = s.storageChainUnchanged(r, states, identities); e != nil {
		return e
	}
	byID := map[string]State{}
	for _, st := range states {
		byID[st.ID] = st
	}
	depth := func(id string) int {
		n := 0
		for seen := map[string]bool{}; id != "" && !seen[id]; {
			seen[id] = true
			n++
			st := byID[id]
			if st.ResumeOf != "" {
				id = st.ResumeOf
			} else {
				id = st.FallbackFrom
			}
		}
		return n
	}
	sort.Slice(items, func(i, j int) bool {
		a, b := byID[filepath.Base(items[i].Path)], byID[filepath.Base(items[j].Path)]
		da, db := depth(a.ID), depth(b.ID)
		if da != db {
			return da > db
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.After(b.CreatedAt)
		}
		return a.ID > b.ID
	})
	for _, item := range items {
		size, _, err := (&storageScanner{ctx, r, 200000}).tree(item.Path)
		if err != nil {
			return err
		}
		// Clear forward pointers while the surviving members' state locks are held.
		// The descendant's backward pointers still connect the chain if removal fails.
		id := filepath.Base(item.Path)
		for other, st := range byID {
			if other == id {
				continue
			}
			changed := false
			if st.FallbackTo == id {
				st.FallbackTo = ""
				changed = true
				result, e := s.storageResult(r, st)
				if e != nil {
					return e
				}
				result.FallbackTo = ""
				if e = storageWriteJSON(r, filepath.Join(filepath.Base(s.Root), other, "result.json"), result); e != nil {
					return e
				}
			}
			if st.Worktree != nil && st.Worktree.Discarded && st.Worktree.Holder == id {
				copy := *st.Worktree
				copy.Holder = ""
				st.Worktree = &copy
				changed = true
			}
			if changed {
				if e := storageWriteJSON(r, filepath.Join(filepath.Base(s.Root), other, "state.json"), st); e != nil {
					return e
				}
				byID[other] = st
			}
		}
		remove := deps.removeJob
		if remove == nil {
			remove = storageTrashJob
		}
		if err = remove(r, item.Path, identities[item.Path]); err != nil {
			return err
		}
		delete(byID, id)
		run.Removed[0].Items++
		run.Removed[0].Bytes += size
	}
	return nil
}

func storageWriteJSON(r *os.Root, path string, value any) error {
	if err := storageRealPath(r, path); err != nil && !os.IsNotExist(err) {
		return err
	}
	// Atomic JSON via a new private file, confined to the already opened home.
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	name := fmt.Sprintf(".storage-%d-%d", os.Getpid(), time.Now().UnixNano())
	f, err := r.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer r.Remove(name)
	_, err = f.Write(append(b, '\n'))
	err = errors.Join(err, f.Close())
	if err != nil {
		return err
	}
	return r.Rename(name, path)
}

// Each HTTP mount owns one bounded, one-minute cache. GETs never create files
// or start native harnesses. The CLI/doctor may request a fresh snapshot.
type StorageReader struct {
	mu    sync.Mutex
	value StorageSnapshot
	until time.Time
}

func (c *StorageReader) Invalidate() { c.mu.Lock(); defer c.mu.Unlock(); c.until = time.Time{} }

func (c *StorageReader) Snapshot(ctx context.Context, s Store) (StorageSnapshot, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.until) {
		return c.value, nil
	}
	p, err := s.PlanStorage(ctx, PruneOptions{scanLimit: 200000})
	if err != nil {
		return StorageSnapshot{}, err
	}
	v := StorageSnapshot{StoragePlan: p}
	for li := range v.Locations {
		v.Locations[li].Items = nil
	}
	r, e := s.openStorage()
	if e == nil {
		defer r.Close()
		var run StorageRun
		if e = storageReadJSON(r, "storage-status.json", &run); e == nil {
			v.LastRun = &run
		} else if !os.IsNotExist(e) {
			return v, e
		}
	} else if !os.IsNotExist(e) {
		return v, e
	}
	c.value = v
	c.until = time.Now().Add(time.Minute)
	return v, nil
}

// Move a verified directory atomically out of jobs before removing any files.
// Failed removals stay in trash and cannot leave a partially readable job.
func storageTrashJob(r *os.Root, path string, expected os.FileInfo) error {
	if e := r.Mkdir("trash", 0700); e != nil && !os.IsExist(e) {
		return e
	}
	if e := storageRealPath(r, "trash"); e != nil {
		return e
	}
	current, e := r.Lstat(path)
	if e != nil {
		return e
	}
	if !current.IsDir() || !os.SameFile(current, expected) {
		return fmt.Errorf("job directory changed")
	}
	target := filepath.Join("trash", fmt.Sprintf("%s-%d", filepath.Base(path), time.Now().UnixNano()))
	if e = storageWriteJSON(r, target+".json", struct {
		Path string `json:"path"`
	}{target}); e != nil {
		return e
	}
	if e = r.Rename(path, target); e != nil {
		return e
	}
	moved, e := r.Lstat(target)
	if e != nil || !os.SameFile(moved, expected) {
		_ = r.Rename(target, path)
		return fmt.Errorf("job directory changed during quarantine")
	}
	if e = storageRemove(r, target); e != nil {
		return e
	}
	return r.Remove(target + ".json")
}

func (s Store) storageChainUnchanged(r *os.Root, states []State, identities map[string]os.FileInfo) error {
	members := map[string]bool{}
	sessions := map[string]bool{}
	for _, st := range states {
		members[st.ID] = true
		result, e := s.storageResult(r, st)
		if e != nil {
			return e
		}
		for _, id := range []string{result.SessionID, st.Request.Resume} {
			if id != "" {
				sessions[st.Request.Harness+"\x00"+id] = true
			}
		}
		p := filepath.Join(filepath.Base(s.Root), st.ID)
		info, e := r.Lstat(p)
		if e != nil {
			return e
		}
		if !info.IsDir() || !os.SameFile(info, identities[p]) {
			return fmt.Errorf("job directory changed: %s", p)
		}
		current, e := s.storageState(r, st.ID)
		if e != nil {
			return e
		}
		a, _ := json.Marshal(st)
		b, _ := json.Marshal(current)
		if !bytes.Equal(a, b) {
			return fmt.Errorf("job state changed: %s", st.ID)
		}
	}
	entries, e := storageEntries(r, filepath.Base(s.Root))
	if e != nil {
		return e
	}
	existing := map[string]bool{}
	snapshots := map[string]State{}
	snapshotErrors := map[string]error{}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		st, e := s.storageState(r, entry.Name())
		if e != nil {
			snapshotErrors[entry.Name()] = e
		} else {
			existing[entry.Name()] = true
			snapshots[entry.Name()] = st
		}
	}
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("unverifiable symlinked sibling: %s", entry.Name())
		}
	}
	for _, entry := range entries {
		if !entry.IsDir() || members[entry.Name()] {
			continue
		}
		st, verified := snapshots[entry.Name()]
		e := snapshotErrors[entry.Name()]
		if !verified {
			rel := filepath.Join(filepath.Base(s.Root), entry.Name())
			if os.IsNotExist(e) && storageCrashLeftover(r, rel, time.Now()) {
				continue
			}
			refs, err := storageRawReferences(r, filepath.Join(rel, "state.json"), entry.Name(), existing)
			if err != nil || len(refs) == 0 {
				return fmt.Errorf("unreadable sibling ownership cannot be bounded: %s", entry.Name())
			}
			for ref := range refs {
				if members[ref] {
					return fmt.Errorf("unreadable job %s may reference chain", entry.Name())
				}
			}
			continue
		}
		links := []string{st.ResumeOf, st.FallbackFrom, st.FallbackTo}
		if st.Worktree != nil {
			links = append(links, st.Worktree.Owner, st.Worktree.Holder)
		}
		for _, id := range links {
			if members[id] {
				return fmt.Errorf("chain acquired job %s", st.ID)
			}
		}
		result, e := s.storageResult(r, st)
		if e != nil {
			continue
		}
		for _, id := range []string{result.SessionID, st.Request.Resume} {
			if id != "" && sessions[st.Request.Harness+"\x00"+id] {
				return fmt.Errorf("session acquired job %s", st.ID)
			}
		}
	}
	return nil
}
func (s Store) storageChainResumeLock(ctx context.Context, r *os.Root, items []StorageItem) (func(), error) {
	states := map[string]State{}
	results := map[string]Result{}
	for _, item := range items {
		st, e := s.storageState(r, filepath.Base(item.Path))
		if e != nil {
			return nil, e
		}
		result, e := s.storageResult(r, st)
		if e != nil {
			return nil, e
		}
		states[st.ID] = st
		results[st.ID] = result
	}
	keys := map[string]bool{}
	for _, st := range states {
		at := st
		seen := map[string]bool{}
		for at.ResumeOf != "" {
			if seen[at.ID] {
				return nil, fmt.Errorf("resume cycle")
			}
			seen[at.ID] = true
			parent, ok := states[at.ResumeOf]
			if !ok {
				return nil, fmt.Errorf("missing resume parent")
			}
			at = parent
		}
		sid := at.Request.Resume
		if sid == "" {
			sid = results[at.ID].SessionID
		}
		if sid == "" {
			sid = "job:" + at.ID
		}
		raw, _ := json.Marshal(resumeScope{Cwd: canonicalResumeCwd(at.Request.Cwd), Harness: at.Request.Harness, Session: sid})
		key := fmt.Sprintf("%x", sha256.Sum256(raw))
		keys[key] = true
	}
	dir := filepath.Base(s.Root) + ".resume-locks"
	if e := r.Mkdir(dir, 0700); e != nil && !os.IsExist(e) {
		return nil, e
	}
	if e := storageRealPath(r, dir); e != nil {
		return nil, e
	}
	sorted := []string{}
	for key := range keys {
		sorted = append(sorted, key)
	}
	sort.Strings(sorted)
	releases := []func(){}
	release := func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}
	for _, key := range sorted {
		unlock, e := storageLock(ctx, r, filepath.Join(dir, key+".lock"), syscall.LOCK_EX)
		if e != nil {
			release()
			return nil, e
		}
		releases = append(releases, unlock)
	}
	return release, nil
}

type StorageReasonSummary struct {
	Action   string   `json:"action"`
	Reason   string   `json:"reason"`
	Count    int      `json:"count"`
	Bytes    int64    `json:"bytes"`
	Examples []string `json:"examples"`
}

func SummarizeStorage(items []StorageItem) []StorageReasonSummary {
	groups := map[string]*StorageReasonSummary{}
	keys := []string{}
	for _, i := range items {
		action := "REMOVE"
		if i.Protected {
			action = "KEEP"
		} else if i.Action == "sweep-sessions" {
			action = "SWEEP SESSIONS"
		}
		reason := storageSummaryReason(i.Reason)
		key := action + "\x00" + reason
		g := groups[key]
		if g == nil {
			g = &StorageReasonSummary{Action: action, Reason: reason, Examples: []string{}}
			groups[key] = g
			keys = append(keys, key)
		}
		g.Count++
		g.Bytes += i.Bytes
		if len(g.Examples) < 5 {
			g.Examples = append(g.Examples, i.Path)
		}
	}
	sort.Strings(keys)
	out := []StorageReasonSummary{}
	for _, key := range keys {
		out = append(out, *groups[key])
	}
	return out
}

func storageSummaryReason(reason string) string {
	if rest, ok := strings.CutPrefix(reason, "chain protected: "); ok {
		if _, r, ok := strings.Cut(rest, ": "); ok {
			reason = r
		}
	}
	for _, prefix := range []string{"symlink protected:", "credential protected:", "special file protected:"} {
		if strings.HasPrefix(reason, prefix) {
			return strings.TrimSuffix(prefix, ":")
		}
	}
	return reason
}

// Only cleanup's marked quarantine entries are eligible for recovery. Include
// them in previews and status so execution never adds an unreviewed removal.
func (s Store) planStorageTrash(p *StoragePlan, scan *storageScanner) {
	entries, e := storageEntries(scan.root, "trash")
	if os.IsNotExist(e) {
		return
	}
	if e != nil {
		p.Failures = append(p.Failures, "trash: "+e.Error())
		return
	}
	l := &p.Locations[0]
	for _, entry := range entries {
		scan.remaining--
		if scan.remaining < 0 || scan.ctx.Err() != nil {
			p.Failures = append(p.Failures, "trash scan budget exceeded")
			return
		}

		if !entry.IsDir() {
			path := filepath.Join("trash", entry.Name())
			if !strings.HasSuffix(path, ".json") {
				continue
			}
			var marker struct {
				Path string `json:"path"`
			}
			info, e := scan.root.Lstat(path)
			if e != nil || !info.Mode().IsRegular() || !info.ModTime().Before(p.ComputedAt.Add(-time.Hour)) {
				continue
			}
			if e = storageReadJSON(scan.root, path, &marker); e != nil || marker.Path != strings.TrimSuffix(path, ".json") {
				continue
			}
			if _, e = scan.root.Lstat(marker.Path); !os.IsNotExist(e) {
				continue
			}
			l.Items = append(l.Items, StorageItem{Path: path, Chain: path, Action: "trash-marker", Bytes: info.Size(), Reason: "orphan trash marker older than creation grace"})
			l.Bytes += info.Size()
			l.Reclaimable += info.Size()
			continue
		}
		path := filepath.Join("trash", entry.Name())
		item := StorageItem{Path: path, Chain: path, Action: "trash-job", Protected: true, Reason: "unverified trash entry"}
		var marker struct {
			Path string `json:"path"`
		}
		if e := storageReadJSON(scan.root, path+".json", &marker); e == nil && marker.Path == path {
			size, _, e := scan.tree(path)
			item.Bytes = size
			l.Bytes += size
			if e != nil {
				item.Reason = e.Error()
			} else {
				item.Protected = false
				item.Reason = "retry interrupted job removal"
				l.Reclaimable += size
			}
		}
		l.Items = append(l.Items, item)
	}
}

func storageRawReferences(r *os.Root, path, own string, existing map[string]bool) (map[string]bool, error) {
	raw, e := storageReadBytes(r, path)
	if e != nil {
		return nil, e
	}
	refs := map[string]bool{}
	// Corruption may truncate a field or concatenate an ID with other bytes.
	// Over-matching only retains more data; token boundaries could lose a chain.
	for id, exists := range existing {
		if exists && id != own && bytes.Contains(raw, []byte(id)) {
			refs[id] = true
		}
	}
	return refs, nil
}

// A short creation grace protects jobs between Mkdir and Save. Empty/lock-only
// crash leftovers cannot run or own sessions after that grace.
func storageCrashLeftover(r *os.Root, path string, now time.Time) bool {
	info, e := r.Lstat(path)
	if e != nil || !info.IsDir() {
		return false
	}
	latest := info.ModTime()
	entries, e := storageEntries(r, path)
	if e != nil {
		return false
	}
	for _, entry := range entries {
		if entry.Name() != "state.lock" && entry.Name() != "worktree.lock" {
			return false
		}
		f, e := r.Lstat(filepath.Join(path, entry.Name()))
		if e != nil || !f.Mode().IsRegular() || f.Size() != 0 {
			return false
		}
		latest = maxStorageTime(latest, f.ModTime())
	}
	return latest.Before(now.Add(-time.Hour))
}

// Directory generation catches newly registered jobs cheaply. Re-read state
// snapshots every 64 items or one second to catch in-place lifecycle updates.
type storageIdlePass struct {
	root       *os.Root
	jobs       string
	check      func(context.Context) (bool, error)
	generation os.FileInfo
	checked    time.Time
	items      int
	err        error
}

func (p *storageIdlePass) allow(ctx context.Context) (bool, error) {
	if e := ctx.Err(); e != nil {
		return false, e
	}
	if p.err != nil {
		return false, p.err
	}
	info, e := p.root.Lstat(p.jobs)
	if e != nil && !os.IsNotExist(e) {
		return false, e
	}
	changed := (info == nil) != (p.generation == nil)
	if info != nil && p.generation != nil {
		changed = !os.SameFile(info, p.generation) || !info.ModTime().Equal(p.generation.ModTime())
	}
	if p.checked.IsZero() || changed || p.items >= 64 || time.Since(p.checked) >= time.Second {
		idle, e := p.check(ctx)
		if e != nil {
			p.err = e
			return false, e
		}
		if !idle {
			p.err = fmt.Errorf("new running or queued job protects shared storage")
			return false, p.err
		}
		p.generation = info
		p.checked = time.Now()
		p.items = 0
	}
	p.items++
	return true, nil
}
