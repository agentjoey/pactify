package sub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

func (s Store) planHomes(p *StoragePlan, scan *storageScanner, states map[string]State, sessions map[string]string, cutoff time.Time, busy string) {
	l := &p.Locations[2]
	keep := map[string]bool{}
	for id, st := range states {
		for _, sid := range []string{sessions[id], st.Request.Resume} {
			if sid != "" {
				keep[sid] = true
			}
		}
	}
	var visit func(string)
	visit = func(path string) {
		scan.remaining--
		if scan.remaining < 0 || scan.ctx.Err() != nil {
			p.Failures = append(p.Failures, "harness home scan limit reached")
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
		item := StorageItem{Path: path, Protected: true, Reason: "configuration or shared harness data"}
		if info.Mode()&os.ModeSymlink != 0 {
			item.Reason = "symlink protected"
			l.Items = append(l.Items, item)
			return
		}
		if storageCredential(path) {
			item.Reason = "credential protected"
			if info.Mode().IsRegular() {
				item.Bytes = info.Size()
				l.Bytes += item.Bytes
			}
			l.Items = append(l.Items, item)
			return
		}
		parts := strings.Split(filepath.ToSlash(path), "/")
		// Recognize session units only in documented private layouts. Never guess
		// ownership of databases or remove whole native homes.
		sid := ""
		if len(parts) == 5 && (parts[1] == "kimi" || parts[1] == "kimi-readonly") && parts[2] == "sessions" && info.IsDir() {
			sid = parts[4]
		}
		if len(parts) >= 6 && parts[1] == "agy" && parts[2] == ".gemini" && parts[3] == "antigravity-cli" && parts[4] == "conversations" && len(parts) == 6 {
			sid, _ = agyConversationID(parts[5])
		}
		if sid != "" {
			size, _, e := scan.tree(path)
			item.Bytes = size
			item.Session = sid
			l.Bytes += size
			switch {
			case e != nil:
				item.Reason = e.Error()
			case busy != "":
				item.Reason = busy
			case keep[sid]:
				item.Reason = "session referenced by an existing job"
			default:
				item.Protected = false
				item.Reason = "orphan session in private harness home"
				l.Reclaimable += size
			}
			l.Items = append(l.Items, item)
			return
		}
		if info.IsDir() {
			entries, e := storageEntries(scan.root, path)
			if e != nil {
				p.Failures = append(p.Failures, path+": "+e.Error())
				return
			}
			for _, entry := range entries {
				visit(filepath.Join(path, entry.Name()))
			}
			return
		}
		if !info.Mode().IsRegular() {
			item.Reason = "special file protected"
			l.Items = append(l.Items, item)
			return
		}
		item.Bytes = info.Size()
		l.Bytes += item.Bytes
		if len(parts) == 5 && (parts[1] == "opencode" || parts[1] == "opencode2") && parts[2] == "share" && parts[3] == "opencode" && parts[4] == "opencode.db" {
			item.Action = "sweep-sessions"
			item.Reason = "native orphan-session sweep; reclaimable bytes unknown until execution"
			if busy != "" {
				item.Reason = busy
			} else {
				item.Protected = false
			}
			l.Items = append(l.Items, item)
			return
		}
		isLog := strings.HasSuffix(info.Name(), ".log") || strings.Contains(filepath.ToSlash(path), "/logs/") || strings.Contains(filepath.ToSlash(path), "/log/")
		if isLog && info.ModTime().Before(cutoff) {
			if busy != "" {
				item.Reason = busy
			} else {
				item.Protected = false
				item.Reason = "harness log older than retention"
				l.Reclaimable += item.Bytes
			}
		}
		l.Items = append(l.Items, item)
	}
	visit("harness-homes")
}

func (s Store) privateHomeRel(st State) (string, error) {
	if st.Request.WithUserConfig {
		return "", fmt.Errorf("user-config sessions cannot be removed from real homes")
	}
	candidates := []string{"harness-homes/" + st.Request.Harness}
	if st.Request.Harness == "kimi" && st.Request.Access == "read-only" {
		candidates = []string{"harness-homes/kimi-readonly"}
	}
	if st.Request.Harness == "opencode" {
		candidates = []string{"harness-homes/opencode/share", "harness-homes/opencode2/share"}
	}
	if st.HarnessHome != "" {
		saved, e := filepath.Abs(st.HarnessHome)
		if e == nil {
			for _, base := range candidates {
				want, e := filepath.Abs(filepath.Join(s.storageHome(), base))
				if e == nil && storageSamePath(saved, want) {
					return filepath.FromSlash(base), nil
				}
			}
		}
	}
	return "", fmt.Errorf("unverified private %s home; keeping session", st.Request.Harness)
}

// Match the managed-home mutation/usage lock order. Native startup and login
// retain the same leases. Only an exclusive lease can authorize removal.
func storageHomeLease(ctx context.Context, r *os.Root, home string) (func(), error) {
	if err := storageRealPath(r, home); err != nil {
		return nil, err
	}
	wait, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	release, err := storageLock(wait, r, home+".lock", syscall.LOCK_EX)
	if err != nil {
		return nil, err
	}
	use, err := storageLock(wait, r, home+".use.lock", syscall.LOCK_EX)
	if err != nil {
		release()
		return nil, fmt.Errorf("private home is in use: %w", err)
	}
	return func() { use(); release() }, nil
}
func (s Store) deleteStorageSessions(ctx context.Context, states []State) error {
	_, e := s.deleteStorageSessionsMeasured(ctx, states)
	return e
}
func (s Store) deleteStorageSessionsMeasured(ctx context.Context, states []State) (out StorageRemoved, err error) {
	r, err := s.openStorage()
	if err != nil {
		return out, err
	}
	defer r.Close()
	codex := map[string]bool{}
	homes := map[string][]string{}
	harness := map[string]string{}
	for _, st := range states {
		result, e := s.storageResult(r, st)
		if e != nil {
			return out, e
		}
		for _, sid := range []string{result.SessionID, st.Request.Resume} {
			if sid == "" {
				continue
			}
			if err = validJobID(sid); err != nil {
				return out, err
			}
			if st.Request.Harness == "codex" {
				codex[sid] = true
				continue
			}
			rel, e := s.privateHomeRel(st)
			if e != nil {
				return out, e
			}
			homes[rel] = append(homes[rel], sid)
			harness[rel] = st.Request.Harness
		}
	}
	if len(codex) > 0 {
		if _, e := exec.LookPath("codex"); e != nil {
			return out, fmt.Errorf("codex app-server unavailable: %w", e)
		}
	}
	// Complete local verification before making the irreversible remote calls.
	var releases []func()
	defer func() {
		for i := len(releases) - 1; i >= 0; i-- {
			releases[i]()
		}
	}()
	var keys []string
	for home := range homes {
		keys = append(keys, home)
	}
	sort.Strings(keys)
	for _, home := range keys {
		release, e := storageHomeLease(ctx, r, home)
		if e != nil {
			return out, e
		}
		releases = append(releases, release)
	}

	for _, home := range keys {
		switch harness[home] {
		case "kimi":
			if _, e := storageKimiSessions(r, home, homes[home]); e != nil {
				return out, e
			}
		case "opencode":
			if e := s.verifyOpencodeStorage(ctx, r, home); e != nil {
				return out, e
			}
			exclude := map[string]bool{}
			for _, st := range states {
				exclude[st.ID] = true
			}
			if _, e := s.storageOpencodeKeep(r, home, exclude); e != nil {
				return out, e
			}
		case "agy":
			if e := storageVerifyAgy(ctx, r, home, homes[home]); e != nil {
				return out, e
			}
		default:
			return out, fmt.Errorf("unsupported session cleanup: %s", harness[home])
		}
	}

	for _, home := range keys {
		var keep map[string]bool
		if harness[home] == "opencode" {
			exclude := map[string]bool{}
			for _, st := range states {
				exclude[st.ID] = true
			}
			keep, err = s.storageOpencodeKeep(r, home, exclude)
			if err != nil {
				return out, err
			}
		}
		removed, e := s.deletePrivateSessionsMeasured(ctx, r, harness[home], home, homes[home], keep)
		storageAddRemoved(&out, removed)
		if e != nil {
			return out, e
		}
	}
	if len(codex) > 0 {
		var ids []string
		for sid := range codex {
			ids = append(ids, sid)
		}
		sort.Strings(ids)
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		err = cockpit.DeleteCodexThreads(call, ids)
		cancel()
		if err != nil {
			return out, err
		}
	}
	return out, nil
}
func (s Store) deletePrivateSessions(ctx context.Context, r *os.Root, harness, home string, ids []string, keep map[string]bool) error {
	_, e := s.deletePrivateSessionsMeasured(ctx, r, harness, home, ids, keep)
	return e
}
func (s Store) deletePrivateSessionFiles(ctx context.Context, r *os.Root, harness, home string, ids []string) error {
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	switch harness {
	case "kimi":
		paths, err := storageKimiSessions(r, home, ids)
		if err != nil {
			return err
		}
		// Native deletion also maintains kimi's metadata/search index. File fallback
		// is confined to verified session directories in this private home.
		if _, e := exec.LookPath("kimi"); e == nil {
			if e = cockpit.DeleteKimiSessionsViaServer(call, filepath.Join(s.storageHome(), home), ids); e != nil {
				return e
			}
		}
		for _, path := range paths {
			if e := storageRemove(r, path); e != nil && !os.IsNotExist(e) {
				return e
			}
		}
		return nil
	case "agy":
		done, err := storageMigrateAgyReceipts(r, home)
		if err != nil {
			return err
		}
		for _, id := range ids {
			found := false
			for _, name := range agyConversationNames(id) {
				path := filepath.Join(home, ".gemini", "antigravity-cli", "conversations", name)
				if err := storageRealPath(r, path); os.IsNotExist(err) {
					continue
				} else if err != nil {
					return err
				}
				scan := storageScanner{ctx, r, 200000}
				if _, _, err := scan.tree(path); err != nil {
					return err
				}
				if err := storageRemove(r, path); err != nil {
					return err
				}
				found = true
			}
			// An unknown native database layout must remain retryable, not silently
			// abandon a session in a shared database.
			if !found {
				if !done[id] {
					return fmt.Errorf("agy session %s has no verified private session files", id)
				}
			} else {
				done[id] = true
				if e := r.Mkdir("storage-receipts", 0700); e != nil && !os.IsExist(e) {
					return e
				}
				if e := storageWriteJSON(r, "storage-receipts/agy.json", done); e != nil {
					return e
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unsupported session cleanup: %s", harness)
	}
}
func (s Store) storageOpencodeOpts(home string) cockpit.StartOpts {
	base := filepath.Dir(home)
	env := []string{"XDG_DATA_HOME=" + filepath.Join(s.storageHome(), home), "XDG_CONFIG_HOME=" + filepath.Join(s.storageHome(), base, "config"), "XDG_STATE_HOME=" + filepath.Join(s.storageHome(), base, "state"), "XDG_CACHE_HOME=" + filepath.Join(s.storageHome(), base, "cache"), "OPENCODE_CONFIG_CONTENT={}", "OPENCODE_CONFIG=", "OPENCODE_CONFIG_DIR=", "OPENCODE_DB=opencode.db", "OPENCODE_DISABLE_CHANNEL_DB=1", "OPENCODE_DISABLE_PROJECT_CONFIG=1"}
	return cockpit.StartOpts{RepoDir: s.storageHome(), MinimalConfig: true, Env: env, Stderr: io.Discard}
}
func storageKimiSessions(r *os.Root, home string, ids []string) ([]string, error) {
	wanted := map[string]bool{}
	for _, id := range ids {
		wanted[id] = true
	}
	paths := map[string]bool{}
	// Validate every index directory before starting native APIs, which trust
	// their own index. Never read a credential or follow a supplied index path.
	index := filepath.Join(home, "session_index.jsonl")
	if err := storageRealPath(r, index); err == nil {
		parent, name, e := storageParent(r, index)
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
			return nil, fmt.Errorf("invalid kimi session index")
		}
		dec := json.NewDecoder(io.LimitReader(f, 8<<20))
		for {
			var entry struct {
				SessionID  string `json:"sessionId"`
				SessionDir string `json:"sessionDir"`
			}
			e = dec.Decode(&entry)
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, e
			}
			rel, e := filepath.Rel(r.Name(), entry.SessionDir)
			if e != nil || !filepath.IsLocal(rel) || !strings.HasPrefix(rel, home+string(filepath.Separator)+"sessions"+string(filepath.Separator)) {
				rel = ""
				workspaces, err := storageEntries(r, filepath.Join(home, "sessions"))
				if err != nil {
					return nil, err
				}
				name := filepath.Base(entry.SessionDir)
				if validJobID(name) != nil {
					return nil, fmt.Errorf("unsafe kimi session index directory")
				}
				for _, ws := range workspaces {
					if !ws.IsDir() {
						continue
					}
					candidate := filepath.Join(home, "sessions", ws.Name(), name)
					if err := storageRealPath(r, filepath.Dir(candidate)); err != nil {
						return nil, err
					}
					expected := filepath.Join(r.Name(), candidate)
					if storageSamePath(entry.SessionDir, expected) || filepath.Base(entry.SessionDir) == name && storageSamePath(filepath.Dir(entry.SessionDir), filepath.Dir(expected)) {
						rel = candidate
						break
					}
				}
				if rel == "" {
					return nil, fmt.Errorf("unsafe kimi session index directory")
				}
			}
			if e = storageRealPath(r, rel); e != nil && !os.IsNotExist(e) {
				return nil, e
			}
			if wanted[entry.SessionID] && e == nil {
				paths[rel] = true
			}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	workspaces, err := storageEntries(r, filepath.Join(home, "sessions"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, ws := range workspaces {
		if !ws.IsDir() {
			if ws.Type()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("unverified kimi workspace")
			}
			continue
		}
		for id := range wanted {
			p := filepath.Join(home, "sessions", ws.Name(), id)
			if err = storageRealPath(r, p); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, err
			}
			if _, _, err = (&storageScanner{context.Background(), r, 200000}).tree(p); err != nil {
				return nil, err
			}
			paths[p] = true
		}
	}
	out := []string{}
	for p := range paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}
func (s Store) pruneStorageItem(ctx context.Context, r *os.Root, item StorageItem, location int, checkIdle func(context.Context) (bool, error)) (out StorageRemoved, err error) {
	if checkIdle == nil {
		checkIdle = s.storageIdleContext
	}
	idle, e := checkIdle(ctx)
	if e != nil {
		return out, e
	}
	if !idle {
		return out, fmt.Errorf("new running or queued job protects shared storage")
	}
	size, _, e := (&storageScanner{ctx, r, 200000}).tree(item.Path)
	if e != nil {
		return out, e
	}
	remove := func() (StorageRemoved, error) {
		if e := storageRemove(r, item.Path); e != nil {
			return out, e
		}
		out.Items = 1
		out.Bytes = size
		return out, nil
	}
	if location == 1 {
		return remove()
	}
	parts := strings.Split(filepath.ToSlash(item.Path), "/")
	if len(parts) < 3 {
		return out, fmt.Errorf("unverified harness home")
	}
	home := filepath.Join("harness-homes", parts[1])
	if strings.HasPrefix(parts[1], "opencode") {
		home = filepath.Join(home, parts[2])
	}
	release, e := storageHomeLease(ctx, r, home)
	if e != nil {
		return out, e
	}
	defer release()
	if item.Action == "sweep-sessions" {
		keep, e := s.storageOpencodeKeep(r, home, nil)
		if e != nil {
			return out, e
		}
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if e = s.verifyOpencodeStorage(call, r, home); e != nil {
			return out, e
		}
		before, e := storageOpencodeDatabaseBytes(r, home)
		if e != nil {
			return out, e
		}
		n, err := cockpit.PruneOpencodeOrphans(call, s.storageOpencodeOpts(home), parts[1] == "opencode2", keep)
		out.Items = n
		out.Sessions = n
		after, e := storageOpencodeDatabaseBytes(r, home)
		if e == nil {
			out.DatabaseMeasured = true
			out.DatabaseBytes = before - after
			if n > 0 {
				out.Bytes = max(int64(0), out.DatabaseBytes)
			}
		}
		return out, errors.Join(err, e)
	}
	if item.Session != "" {
		h := parts[1]
		if h == "kimi-readonly" {
			h = "kimi"
		}
		return s.deletePrivateSessionsMeasured(ctx, r, h, home, []string{item.Session}, nil)
	}
	return remove()
}

func (s Store) storageOpencodeKeep(r *os.Root, home string, exclude map[string]bool) (map[string]bool, error) {
	states, err := s.storageStates()
	if err != nil {
		return nil, err
	}
	keep := map[string]bool{}
	expected, err := filepath.Abs(filepath.Join(s.storageHome(), home))
	if err != nil {
		return nil, err
	}
	for _, st := range states {
		if exclude[st.ID] {
			continue
		}
		if !Terminal(st.Status) {
			return nil, fmt.Errorf("active job protects private homes")
		}
		saved, e := filepath.Abs(st.HarnessHome)
		if e != nil {
			return nil, e
		}
		if st.Request.Harness != "opencode" || st.Request.WithUserConfig {
			continue
		}
		result, e := s.storageResult(r, st)
		if e != nil {
			if os.IsNotExist(e) && st.Request.Resume == "" && st.HarnessHome == "" {
				continue
			}
			return nil, e
		}
		if result.SessionID == "" && st.Request.Resume == "" {
			continue
		}
		rel, e := s.privateHomeRel(st)
		if e != nil {
			return nil, e
		}
		if rel != home {
			continue
		} // An explicitly verified home of the other major.
		if !storageSamePath(saved, expected) {
			return nil, fmt.Errorf("unverified opencode home for %s", st.ID)
		}
		if result.SessionID != "" {
			keep[result.SessionID] = true
		}
		if st.Request.Resume != "" {
			keep[st.Request.Resume] = true
		}
	}
	return keep, nil
}

func (s Store) verifyOpencodeStorage(ctx context.Context, r *os.Root, home string) error {
	for _, path := range []string{filepath.Join(home, "opencode"), filepath.Join(home, "opencode/opencode.db"), filepath.Join(home, "opencode/opencode.db-wal"), filepath.Join(home, "opencode/opencode.db-shm"), filepath.Join(home, "opencode/log"), filepath.Join(filepath.Dir(home), "config/opencode"), filepath.Join(filepath.Dir(home), "state"), filepath.Join(filepath.Dir(home), "cache")} {
		if err := storageRealPath(r, path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	info, err := r.Lstat(filepath.Join(home, "opencode/opencode.db"))
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("private opencode database must be a regular file")
	}
	version, err := s.storageOpencodeVersion(ctx, r, home)
	if err != nil {
		return fmt.Errorf("cannot verify opencode major before cleanup: %w", err)
	}
	want := 1
	if strings.Contains(filepath.ToSlash(home), "/opencode2/") {
		want = 2
	}
	if opencodeMajor(version) != want {
		return fmt.Errorf("opencode %s cannot clean a private %d.x database; install the matching major", version, want)
	}
	return nil
}

// Keep version discovery in this Store, rather than cliVersion's default home.
// The disposable HOME/XDG roots cannot open either persistent database, and
// Root confines both creating and removing the owned probe directory.
func (s Store) storageOpencodeVersion(ctx context.Context, r *os.Root, home string) (version string, err error) {
	name := filepath.Join(home, fmt.Sprintf(".pactify-version-%d-%d", os.Getpid(), time.Now().UnixNano()))
	if err = r.Mkdir(name, 0700); err != nil {
		return "", err
	}
	defer func() { err = errors.Join(err, storageRemove(r, name)) }()
	if err = storageRealPath(r, name); err != nil {
		return "", err
	}
	probe := filepath.Join(r.Name(), name)
	env := append(opencodeVendorStrip(), "HOME="+probe, "OPENCODE_CONFIG=", "OPENCODE_CONFIG_DIR=", "OPENCODE_CONFIG_CONTENT={}", "OPENCODE_CONFIG=", "OPENCODE_CONFIG_DIR=", "OPENCODE_DB=opencode.db", "OPENCODE_DISABLE_CHANNEL_DB=1", "OPENCODE_DISABLE_PROJECT_CONFIG=1")
	for _, key := range []string{"XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME"} {
		env = append(env, key+"="+filepath.Join(probe, key))
	}
	call, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(call, "opencode", "--version")
	cmd.Dir = probe
	cmd.Env = cockpit.OpencodeCommandEnv(env, true)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(string(out), "\n")
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return "", fmt.Errorf("opencode --version returned no version")
	}
	return fields[len(fields)-1], nil
}

func storageSamePath(a, b string) bool {
	if a == b {
		return true
	}
	x, e := os.Stat(a)
	if e != nil {
		return false
	}
	y, e := os.Stat(b)
	return e == nil && os.SameFile(x, y)
}
func storageVerifyAgy(ctx context.Context, r *os.Root, home string, ids []string) error {
	for _, id := range ids {
		found := false
		for _, name := range agyConversationNames(id) {
			p := filepath.Join(home, ".gemini/antigravity-cli/conversations", name)
			if e := storageRealPath(r, p); os.IsNotExist(e) {
				continue
			} else if e != nil {
				return e
			}
			if _, _, e := (&storageScanner{ctx, r, 200000}).tree(p); e != nil {
				return e
			}
			found = true
		}
		if !found {
			done, e := storageAgyReceipts(r, home)
			if e != nil {
				return e
			}
			if !done[id] {
				return fmt.Errorf("agy session %s has no verified private session files", id)
			}
		}
	}
	return nil
}

func storageAgyReceipts(r *os.Root, home string) (map[string]bool, error) {
	done := map[string]bool{}
	// Planning and preflight can consult either generation without mutating it.
	for _, path := range []string{filepath.Join(home, ".pactify-pruned-sessions.json"), "storage-receipts/agy.json"} {
		var receipts map[string]bool
		e := storageReadJSON(r, path, &receipts)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if receipts == nil {
			return nil, fmt.Errorf("invalid agy prune receipts")
		}
		for id, deleted := range receipts {
			done[id] = done[id] || deleted
		}
	}
	return done, nil
}

// Migration belongs to execution: persist the combined receipts before removing
// the old file, so an interrupted migration can be retried without losing IDs.
func storageMigrateAgyReceipts(r *os.Root, home string) (map[string]bool, error) {
	done, err := storageAgyReceipts(r, home)
	if err != nil {
		return nil, err
	}
	legacy := filepath.Join(home, ".pactify-pruned-sessions.json")
	if _, err = r.Lstat(legacy); os.IsNotExist(err) {
		return done, nil
	} else if err != nil {
		return nil, err
	}
	if err = r.Mkdir("storage-receipts", 0700); err != nil && !os.IsExist(err) {
		return nil, err
	}
	if err = storageWriteJSON(r, "storage-receipts/agy.json", done); err != nil {
		return nil, err
	}
	if err = storageRemove(r, legacy); err != nil {
		return nil, err
	}
	return done, nil
}

func storageAddRemoved(dst *StorageRemoved, src StorageRemoved) {
	dst.Items += src.Items
	dst.Bytes += src.Bytes
	dst.Sessions += src.Sessions
	dst.DatabaseBytes += src.DatabaseBytes
	dst.DatabaseMeasured = dst.DatabaseMeasured || src.DatabaseMeasured
}
func storageOpencodeDatabaseBytes(r *os.Root, home string) (int64, error) {
	var total int64
	for _, name := range []string{"opencode.db", "opencode.db-wal", "opencode.db-shm"} {
		p := filepath.Join(home, "opencode", name)
		if e := storageRealPath(r, p); os.IsNotExist(e) {
			continue
		} else if e != nil {
			return 0, e
		}
		info, e := r.Lstat(p)
		if e != nil {
			return 0, e
		}
		if !info.Mode().IsRegular() {
			return 0, fmt.Errorf("unverified opencode database file: %s", p)
		}
		total += info.Size()
	}
	return total, nil
}
func (s Store) deletePrivateSessionsMeasured(ctx context.Context, r *os.Root, harness, home string, ids []string, keep map[string]bool) (out StorageRemoved, err error) {
	unique := map[string]bool{}
	for _, id := range ids {
		unique[id] = true
	}
	ids = make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	if harness == "opencode" {
		call, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		if e := s.verifyOpencodeStorage(call, r, home); e != nil {
			return out, e
		}
		before, e := storageOpencodeDatabaseBytes(r, home)
		if e != nil {
			return out, e
		}
		n, e := cockpit.PruneOpencodeSessionsCount(call, s.storageOpencodeOpts(home), strings.Contains(home, "opencode2"), ids, keep)
		out.Items = n
		out.Sessions = n
		after, measureErr := storageOpencodeDatabaseBytes(r, home)
		if measureErr == nil {
			out.DatabaseMeasured = true
			out.DatabaseBytes = before - after
			if n > 0 {
				out.Bytes = max(int64(0), out.DatabaseBytes)
			}
		}
		return out, errors.Join(e, measureErr)
	}
	type file struct {
		path, id string
		size     int64
	}
	files := []file{}
	add := func(path, id string) error {
		size, _, e := (&storageScanner{ctx, r, 200000}).tree(path)
		if os.IsNotExist(e) {
			return nil
		}
		if e != nil {
			return e
		}
		files = append(files, file{path, id, size})
		return nil
	}
	switch harness {
	case "kimi":
		paths, e := storageKimiSessions(r, home, ids)
		if e != nil {
			return out, e
		}
		for _, path := range paths {
			if e = add(path, filepath.Base(path)); e != nil {
				return out, e
			}
		}
	case "agy":
		for _, id := range ids {
			for _, name := range agyConversationNames(id) {
				if e := add(filepath.Join(home, ".gemini/antigravity-cli/conversations", name), id); e != nil {
					return out, e
				}
			}
		}
	default:
		return out, fmt.Errorf("unsupported session cleanup: %s", harness)
	}
	err = s.deletePrivateSessionFiles(ctx, r, harness, home, ids)
	allGone := map[string]bool{}
	for _, f := range files {
		if _, ok := allGone[f.id]; !ok {
			allGone[f.id] = true
		}
		if _, e := r.Lstat(f.path); os.IsNotExist(e) {
			out.Items++
			out.Bytes += f.size
		} else {
			allGone[f.id] = false
		}
	}
	for _, gone := range allGone {
		if gone {
			out.Sessions++
		}
	}
	return out, err
}

// agy stores a conversation as <session>.db (1.4.x), <session>.pb (earlier) or a
// bare <session> entry.
var agyConversationSuffixes = []string{".db", ".pb", ""}

func agyConversationNames(id string) []string {
	names := make([]string, 0, len(agyConversationSuffixes))
	for _, suffix := range agyConversationSuffixes {
		names = append(names, id+suffix)
	}
	return names
}

// agyConversationID recognizes only the documented names; any other extension is
// not treated as a session, so it is never classified as an orphan.
func agyConversationID(name string) (string, bool) {
	for _, suffix := range agyConversationSuffixes[:2] {
		if id, ok := strings.CutSuffix(name, suffix); ok && id != "" && !strings.Contains(id, ".") {
			return id, true
		}
	}
	if name != "" && !strings.Contains(name, ".") {
		return name, true
	}
	return "", false
}
