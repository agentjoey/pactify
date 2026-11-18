package sub

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ensureWorktreeLink repairs the worktree's .git link file when a harness
// deleted or corrupted it. A worktree whose directory is gone is left to the
// callers' existing handling. Only the one-line link file is rewritten;
// worktree contents and a .git directory are never touched.
func ensureWorktreeLink(w *Worktree) error {
	if _, err := os.Lstat(w.Path); err != nil {
		return nil
	}
	link := filepath.Join(w.Path, ".git")
	info, err := os.Lstat(link)
	switch {
	case err == nil && info.IsDir():
		return worktreeLinkError(w, "", "its .git is a directory, not a link file; the harness probably ran git init or cloned inside it")
	case err == nil && worktreeLinkTarget(link):
		return nil
	case err != nil && !os.IsNotExist(err):
		return worktreeLinkError(w, "", err.Error())
	}
	if info, err := os.Stat(w.OriginalRoot); err != nil || !info.IsDir() {
		return worktreeLinkError(w, "", "the original repository "+w.OriginalRoot+" no longer exists")
	}
	admin, err := worktreeAdminDir(w)
	if err != nil {
		return worktreeLinkError(w, "", err.Error())
	}
	// git worktree repair cannot recreate a missing link file, so write the
	// one-line link through a temporary file and an atomic rename.
	tmp := link + ".pactify-tmp"
	if err := os.WriteFile(tmp, []byte("gitdir: "+admin+"\n"), 0o644); err != nil {
		return worktreeLinkError(w, admin, err.Error())
	}
	if err := os.Rename(tmp, link); err != nil {
		_ = os.Remove(tmp)
		return worktreeLinkError(w, admin, err.Error())
	}
	return nil
}

// worktreeAdminDir finds the repository's administrative directory for the
// worktree at w.Path: the <common-dir>/worktrees/<name> whose gitdir file
// points back at this worktree.
func worktreeAdminDir(w *Worktree) (string, error) {
	out, err := runGit(w.OriginalRoot, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(out))
	entries, err := os.ReadDir(filepath.Join(common, "worktrees"))
	if err != nil {
		return "", fmt.Errorf("the repository does not list this worktree")
	}
	for _, entry := range entries {
		admin := filepath.Join(common, "worktrees", entry.Name())
		data, err := os.ReadFile(filepath.Join(admin, "gitdir"))
		if err != nil {
			continue
		}
		if sameResumeDirectory(filepath.Dir(strings.TrimSpace(string(data))), w.Path) {
			return admin, nil
		}
	}
	return "", fmt.Errorf("the repository does not list this worktree")
}

// worktreeLinkTarget reports whether link is a "gitdir: <path>" file whose
// target directory exists.
func worktreeLinkTarget(link string) bool {
	data, err := os.ReadFile(link)
	if err != nil {
		return false
	}
	target, ok := strings.CutPrefix(strings.TrimSpace(string(data)), "gitdir: ")
	if !ok || target == "" {
		return false
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(link), target)
	}
	info, err := os.Stat(target)
	return err == nil && info.IsDir()
}

func worktreeLinkError(w *Worktree, admin, reason string) error {
	hint := "copy any files you need out of the worktree, then discard the job"
	if admin != "" {
		hint = fmt.Sprintf("restore the link with: printf 'gitdir: %%s\\n' %s > %s",
			shellQuote(admin), shellQuote(filepath.Join(w.Path, ".git")))
	}
	return actionConflict("worktree %s has a missing or corrupt .git link and could not be repaired automatically (%s); its files were not changed. To recover, %s",
		w.Path, strings.TrimSpace(reason), hint)
}
