package gitx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTreeFingerprintIgnoresSuccessfulGitStderr(t *testing.T) {
	bin := t.TempDir()
	script := `#!/bin/sh
printf 'warning: cache-%s\n' "$$" >&2
if [ "$1" = rev-parse ]; then printf 'fixed-head\n'; fi
`
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for i := 0; i < 2; i++ {
		if got := TreeFingerprint(t.TempDir()); got != "fixed-head\n" {
			t.Fatalf("successful git stderr polluted idle fingerprint: %q", got)
		}
	}
}

func TestGitRunRetainsFailureDiagnostic(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nprintf 'partial output\\n'\nprintf 'fatal: fixture failure\\n' >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	out, err := run(t.TempDir(), "status")
	if err == nil || !strings.Contains(out, "partial output") || !strings.Contains(out, "fatal: fixture failure") {
		t.Fatalf("failure diagnostic lost: %q %v", out, err)
	}
}

func TestTreeFingerprintIgnoresFailedGitOutput(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\nif [ \"$1\" = rev-parse ]; then printf 'unborn-%s\\n' \"$$\"; printf 'xcrun warning-%s\\n' \"$$\" >&2; exit 1; fi\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for range 2 {
		if got := TreeFingerprint(t.TempDir()); got != "\n" {
			t.Fatalf("failed git output polluted fingerprint: %q", got)
		}
	}
}
