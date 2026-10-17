package e2e

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"syscall"
)

type meta struct {
	Inode       uint64
	Mode        fs.FileMode
	Size, Mtime int64
	Target      string
}

func metadata(roots []string, ignoreGit bool) (map[string]meta, error) {
	result := map[string]meta{}
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, e error) error {
			if os.IsNotExist(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if ignoreGit && d.Name() == ".git" {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			st, e := os.Lstat(path)
			if e != nil {
				return e
			}
			m := meta{Mode: st.Mode(), Size: st.Size(), Mtime: st.ModTime().UnixNano()}
			if stat, ok := st.Sys().(*syscall.Stat_t); ok {
				m.Inode = stat.Ino
			}
			if st.Mode()&os.ModeSymlink != 0 {
				m.Target, e = os.Readlink(path)
				if e != nil {
					return e
				}
			}
			result[path] = m
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}
func compareMetadata(a, b map[string]meta, h string) (changed, refresh []string) {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	for k := range keys {
		old, ok := a[k]
		now, present := b[k]
		if ok == present && reflect.DeepEqual(old, now) {
			continue
		}
		credential := h == "codex" && filepath.Base(k) == "auth.json" || h == "opencode" && filepath.Base(k) == "auth.json" || h == "agy" && filepath.Base(k) == "antigravity-oauth-token" || h == "kimi" && strings.Contains(k, string(filepath.Separator)+"credentials"+string(filepath.Separator))
		temporary := strings.Contains(filepath.Base(k), ".tmp.")
		credential = credential && !temporary && ok && present && old.Mode == 0600 && now.Mode == 0600
		lock := h == "kimi" && strings.Contains(filepath.ToSlash(k), "/oauth/") && !temporary
		if lock {
			// Lock files can be created, removed or rewritten; they must remain
			// regular files. Preserve the CLI's existing mode (some lock
			// implementations use 0644); never exempt a permission change.
			regular := func(mode fs.FileMode) bool { return mode == 0600 || mode == 0644 }
			lock = (!ok || regular(old.Mode)) && (!present || regular(now.Mode)) && (!ok || !present || old.Mode == now.Mode)
		}
		lockDir := h == "kimi" && strings.HasSuffix(filepath.ToSlash(k), "/oauth") && present && now.Mode.IsDir()
		if lockDir && !ok {
			lockDir = now.Mode.Perm() == 0700
		} else if lockDir {
			x, y := old, now
			x.Size, x.Mtime, y.Size, y.Mtime = 0, 0, 0, 0
			lockDir = x == y
		}
		if credential || lock || lockDir {
			refresh = append(refresh, k)
		} else {
			changed = append(changed, k)
		}
	}
	// Atomic login refreshes also update parent-directory size/mtime. Exempt
	// those metadata-only parent changes only when every changed descendant is
	// already a known credential path; never exempt inode/mode/link changes.
	sort.Slice(changed, func(i, j int) bool { return len(changed[i]) > len(changed[j]) })
	remaining := map[string]bool{}
	for _, p := range changed {
		remaining[p] = true
	}
	for _, p := range changed {
		old, ok := a[p]
		now, present := b[p]
		if !ok || !present || !old.Mode.IsDir() || !now.Mode.IsDir() {
			continue
		}
		old.Size, old.Mtime = 0, 0
		now.Size, now.Mtime = 0, 0
		if old != now {
			continue
		}
		credentialChild := false
		for _, r := range refresh {
			if strings.HasPrefix(r, p+string(filepath.Separator)) {
				credentialChild = true
				break
			}
		}
		if !credentialChild {
			continue
		}
		otherChild := false
		for c := range remaining {
			if c != p && strings.HasPrefix(c, p+string(filepath.Separator)) {
				otherChild = true
				break
			}
		}
		if !otherChild {
			delete(remaining, p)
			refresh = append(refresh, p)
		}
	}
	changed = nil
	for p := range remaining {
		changed = append(changed, p)
	}
	sort.Strings(changed)
	sort.Strings(refresh)
	return
}
