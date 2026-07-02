package sessions

import "testing"

// Seat ids are free-form (w1/w10, claude/claude-2), so the tag must be
// self-delimiting: an open-ended "pact:w1" substring-matched seat w10's session
// title and cleanup for w1 deleted w10's possibly-live session.
func TestSessionTagSelfDelimiting(t *testing.T) {
	if tag := SessionTag("w1"); tag != "[pact:w1]" {
		t.Fatalf("SessionTag(w1) = %q, want [pact:w1]", tag)
	}
	pairs := [][2]string{{"w1", "w10"}, {"claude", "claude-2"}}
	for _, p := range pairs {
		short, long := SessionTag(p[0]), SessionTag(p[1])
		if len(short) <= len(long) && long[:len(short)] == short {
			t.Errorf("tag %q is a prefix of %q — cleanup would cross seats", short, long)
		}
	}
}

func TestCleanupByTitleDoesNotCrossSeats(t *testing.T) {
	list := "Session ID   Title         Updated\n" +
		"────────────────────────────────\n" +
		"ses_aaa111  " + SessionTag("w1") + "   11:00\n" +
		"ses_bbb222  " + SessionTag("w10") + "  10:00\n"
	fake := &fakeRun{out: list}
	m := Manager{Run: fake.Run}
	deleted, skipped, err := m.CleanupByTitle("opencode", SessionTag("w1"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if skipped {
		t.Fatal("expected skipped=false for opencode")
	}
	if !equalSlice(deleted, []string{"ses_aaa111"}) {
		t.Fatalf("deleted = %v, want [ses_aaa111] only — w10's session must survive w1's cleanup", deleted)
	}
}
