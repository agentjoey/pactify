package finish

import "fmt"

type Runner func(dir string, name string, args ...string) (string, error)

type Finisher struct {
	Run   Runner
	HasGH func() bool
}

func (f Finisher) Push(dir, remote, branch string) (string, error) {
	if remote == "" || branch == "" {
		return "", fmt.Errorf("finish: remote and branch are required")
	}
	return f.Run(dir, "git", "push", remote, branch)
}

func (f Finisher) OpenPR(dir, base, head, title, body string) (string, error) {
	if base == "" || head == "" || title == "" {
		return "", fmt.Errorf("finish: base, head and title are required")
	}
	if f.HasGH != nil && !f.HasGH() {
		return fmt.Sprintf("gh not found — run manually:\n  gh pr create --base %s --head %s --title %q --body %q", base, head, title, body), nil
	}
	return f.Run(dir, "gh", "pr", "create", "--base", base, "--head", head, "--title", title, "--body", body)
}
