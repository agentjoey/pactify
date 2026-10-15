package subpanel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/agentjoey/pactify/internal/sub"
)

const MaxDiffBytes = 512 * 1024

type DiffResponse struct {
	Patch     string          `json:"patch"`
	Files     []sub.FileDelta `json:"files"`
	Truncated bool            `json:"truncated"`
	Note      string          `json:"note"`
}

var errOutputLimit = errors.New("output limit reached")

type limitedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (b *limitedBuffer) Len() int       { return b.buffer.Len() }
func (b *limitedBuffer) String() string { return b.buffer.String() }
func (b *limitedBuffer) Bytes() []byte  { return b.buffer.Bytes() }
func (b *limitedBuffer) Write(p []byte) (int, error) {
	room := b.limit - b.Len()
	if len(p) > room {
		n, _ := b.buffer.Write(p[:room])
		b.truncated = true
		return n, errOutputLimit
	}
	return b.buffer.Write(p)
}
func gitOutput(ctx context.Context, dir string, out io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	cmd.Stdout = out
	var stderr limitedBuffer
	stderr.limit = 4096
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git diff: %w: %s", err, stderr.String())
	}
	return nil
}
func (s *Server) diff(w http.ResponseWriter, r *http.Request) {
	st, err := s.load(r)
	if err != nil {
		apiError(w, err)
		return
	}
	d := DiffResponse{Files: []sub.FileDelta{}}
	if result, err := s.result(st.ID); err == nil && result.Diff.Files != nil {
		d.Files = result.Diff.Files
	}
	if st.Worktree == nil {
		d.Note = "Only the recorded change list is available; this job did not save a full patch."
		jsonResponse(w, d)
		return
	}
	if st.Worktree.Discarded {
		d.Note = "Worktree discarded; showing recorded changes."
		jsonResponse(w, d)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	out := &limitedBuffer{limit: MaxDiffBytes}
	err = gitOutput(ctx, st.Worktree.Path, out, "diff", "--no-ext-diff", "--no-textconv", "--no-renames", st.Worktree.BaseCommit, "--")
	if err == nil {
		paths := &limitedBuffer{limit: MaxDiffBytes}
		err = gitOutput(ctx, st.Worktree.Path, paths, "ls-files", "--others", "--exclude-standard", "-z")
		if paths.truncated {
			out.truncated = true
		}
		if err == nil {
			for _, path := range strings.Split(paths.String(), "\x00") {
				if path == "" || out.truncated {
					continue
				}
				full := filepath.Join(st.Worktree.Path, path)
				info, statErr := os.Lstat(full)
				if statErr != nil {
					err = statErr
					break
				}
				if info.Mode()&os.ModeSymlink != 0 {
					// Display link text, never dereference an untracked link outside the repo.
					target, readErr := os.Readlink(full)
					if readErr != nil {
						err = readErr
						break
					}
					_, _ = fmt.Fprintf(out, "diff --git a/%s b/%s\nnew file mode 120000\n--- /dev/null\n+++ b/%s\n@@ -0,0 +1 @@\n+%s\n", path, path, path, target)
				} else if info.Mode().IsRegular() {
					// --no-index returns 1 for differences. It reads without staging files or
					// writing Git objects, unlike the apply workflow's temporary index.
					e := gitOutput(ctx, st.Worktree.Path, out, "diff", "--no-index", "--no-ext-diff", "--no-textconv", "--", "/dev/null", path)
					var exit *exec.ExitError
					if e != nil && (!errors.As(e, &exit) || exit.ExitCode() != 1) && !out.truncated {
						err = e
						break
					}
				}
			}
		}
	}
	// Repository files need not be UTF-8. Replace invalid bytes without losing
	// later readable hunks, then trim only a potentially split final rune.
	patch := strings.ToValidUTF8(string(out.Bytes()), "�")
	if len(patch) > MaxDiffBytes {
		patch = patch[:MaxDiffBytes]
		for !utf8.ValidString(patch) {
			patch = patch[:len(patch)-1]
		}
		out.truncated = true
	}
	d.Patch = patch
	d.Truncated = out.truncated
	if d.Truncated {
		d.Note = "Diff truncated at 512 KiB."
	} else if err != nil {
		d.Note = "Live diff unavailable; showing recorded changes. " + err.Error()
	}
	jsonResponse(w, d)
}
