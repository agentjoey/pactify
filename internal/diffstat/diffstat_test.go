package diffstat

import (
	"errors"
	"testing"
)

func TestNumStatWith_Parse(t *testing.T) {
	cases := []struct {
		name                string
		out                 string
		runErr              error
		wantA, wantD, wantF int
		wantErr             bool
	}{
		{name: "two text files", out: "3\t1\tfoo.go\n10\t0\tbar.go\n", wantA: 13, wantD: 1, wantF: 2},
		{name: "binary line counts file, not lines", out: "-\t-\timg.png\n2\t2\tx.go\n", wantA: 2, wantD: 2, wantF: 2},
		{name: "empty", out: "", wantA: 0, wantD: 0, wantF: 0},
		{name: "git error", runErr: errors.New("boom"), wantErr: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			run := func(_ string, _ ...string) (string, error) { return c.out, c.runErr }
			s, err := numStatWith(run, "/repo", "A", "B")
			if c.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if s.Added != c.wantA || s.Deleted != c.wantD || s.Files != c.wantF {
				t.Fatalf("stat = %+v, want A=%d D=%d F=%d", s, c.wantA, c.wantD, c.wantF)
			}
		})
	}
}
