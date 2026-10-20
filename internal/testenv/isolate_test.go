package testenv

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsolatePactifyHome(t *testing.T) {
	user := t.TempDir()
	t.Setenv("HOME", user)
	explicit := filepath.Join(t.TempDir(), "persistent")
	for _, tc := range []struct {
		name, home string
		keep       bool
	}{
		{"unset", "", false},
		{"explicit", explicit, true},
		{"user store", filepath.Join(user, ".pactify"), false},
		{"user store spelled differently", filepath.Join(user, "x", "..", ".pactify") + "/", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", tc.home)
			var seen string
			code := isolate(func() int {
				seen = os.Getenv("PACTIFY_HOME")
				if !tc.keep {
					// Product code creates the home on first write.
					if err := os.MkdirAll(filepath.Join(seen, "jobs"), 0700); err != nil {
						t.Error(err)
					}
				}
				return 7
			})
			if code != 7 {
				t.Fatalf("exit code = %d, want the run result", code)
			}
			if tc.keep {
				if seen != tc.home {
					t.Fatalf("PACTIFY_HOME = %q, want explicit %q kept", seen, tc.home)
				}
				return
			}
			if seen == "" || seen == tc.home || filepath.Dir(seen) == user {
				t.Fatalf("PACTIFY_HOME = %q, want a disposable directory", seen)
			}
			if _, err := os.Stat(seen); !os.IsNotExist(err) {
				t.Fatalf("disposable PACTIFY_HOME %s survived the run: %v", seen, err)
			}
		})
	}
}
