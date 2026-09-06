package ledger

import (
	"fmt"
	"os"
	"strings"
)

// WS-B ships the ref store DARK.
//
// The file remains authoritative; the ref is written alongside it only when
// PACTIFY_LEDGER_REF is set. That ordering is deliberate: a defect in brand-new
// ledger storage must not be able to cost anyone an event, and the migration
// plan (spec §4) wants a period where both stores run so their agreement can be
// measured before WS-C flips which one is canonical.
//
// Everything here is best-effort by construction — see ShadowAppend.

// ShadowEnabled reports whether ref-backed shadow writes are on. Off unless the
// operator opts in with an affirmative value; anything unrecognised is off, so a
// typo fails safe rather than silently enabling an experiment.
func ShadowEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PACTIFY_LEDGER_REF"))) {
	case "1", "true", "yes", "on", "shadow":
		return true
	}
	return false
}

// ShadowAppend mirrors one event line into the ledger ref.
//
// It NEVER returns an error to the caller. By the time it runs, the file write
// has already succeeded and the protocol verb is done; failing the verb because
// an experimental mirror had a problem would make enabling the flag strictly
// worse than leaving it off. Failures go to stderr so the operator can see the
// mirror is unhealthy without any run depending on it.
func ShadowAppend(dir, line string) error {
	if !ShadowEnabled() {
		return nil
	}
	if err := seedRefIfEmpty(dir); err != nil {
		fmt.Fprintf(os.Stderr, "pactify: ledger ref seed failed (file ledger is unaffected): %v\n", err)
		return nil
	}
	if err := AppendRef(dir, line); err != nil {
		fmt.Fprintf(os.Stderr, "pactify: ledger ref mirror failed (file ledger is unaffected): %v\n", err)
	}
	return nil
}

// seedRefIfEmpty backfills the ref from the file ledger the first time the
// mirror runs in a repo that already has history.
//
// Without it, enabling the flag mid-life produces a ref holding only the events
// appended "from now on", and Verify then reports drift on every such repo —
// which is not drift, it is a missing backfill. The dual-read check would be
// useless from day one, which is the entire point of WS-B.
//
// It seeds everything EXCEPT the line about to be appended (that line is already
// in the file by the time the mirror runs, and AppendRef adds it next).
func seedRefIfEmpty(dir string) error {
	head, err := RefHead(dir)
	if err != nil || head != "" {
		return err // not a git repo, or already seeded
	}
	existing, err := os.ReadFile(Path(dir))
	if err != nil {
		return nil // no file ledger yet: nothing to backfill
	}
	lines := strings.Split(strings.TrimRight(string(existing), "\n"), "\n")
	if len(lines) <= 1 {
		return nil // only the event we are about to mirror
	}
	for _, l := range lines[:len(lines)-1] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		if err := AppendRef(dir, l); err != nil {
			return err
		}
	}
	return nil
}

// Verify compares the two stores and returns a human-readable drift report, or
// "" when they agree. This is the dual-READ half of WS-B: it answers "could the
// ref be trusted as canonical yet?" with evidence rather than hope.
func Verify(dir string) (string, error) {
	fileEvents, err := Read(dir)
	if err != nil {
		return "", err
	}
	refLines, err := ReadRef(dir)
	if err != nil {
		return "", err
	}
	if len(fileEvents) != len(refLines) {
		return fmt.Sprintf("ledger drift: file has %d event(s), ref %s has %d",
			len(fileEvents), RefName, len(refLines)), nil
	}
	for i, ev := range fileEvents {
		if !strings.Contains(refLines[i], ev.EventID) {
			return fmt.Sprintf("ledger drift at position %d: file event_id %s is not in the ref's line",
				i, ev.EventID), nil
		}
	}
	return "", nil
}
