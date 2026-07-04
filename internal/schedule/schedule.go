// Package schedule implements scheduled/recurring orchestrate triggers for
// pactify serve. It parses two hand-written expression forms only — no cron
// dependency:
//
//	daily@HH:MM   fire once a day at a local-time wall clock (24h, HH 0-23)
//	every:Nh      fire every N hours (N >= 1)
//	every:Nm      fire every N minutes (N >= 1)
//
// Schedules are machine-level operational state (not a protocol fact), persisted
// to ~/.pactify/schedules.json. NextFire is a pure function — callers pass `now`;
// nothing here reads the wall clock (the Runner injects its own clock).
package schedule

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Schedule is one scheduled/recurring orchestrate trigger.
type Schedule struct {
	ID      string `json:"id"`
	Project string `json:"project"`
	Feature string `json:"feature,omitempty"`
	Expr    string `json:"expr"`
	Enabled bool   `json:"enabled"`
}

var (
	dailyRe = regexp.MustCompile(`^daily@([01]?\d|2[0-3]):([0-5]\d)$`)
	everyRe = regexp.MustCompile(`^every:(\d+)([hm])$`)
)

// Validate reports whether expr is one of the two supported forms.
func Validate(expr string) error {
	_, err := NextFire(time.Now(), expr)
	return err
}

// NextFire returns the next fire time strictly after now for expr:
//   - daily@HH:MM  → the next local-time occurrence of HH:MM after now (today if
//     that time is still ahead, else tomorrow — cross-midnight handled).
//   - every:Nh|Nm  → now + the interval.
//
// It is pure: the same (now, expr) always yields the same result. An invalid or
// unsupported expr returns an error.
func NextFire(now time.Time, expr string) (time.Time, error) {
	expr = strings.TrimSpace(expr)

	if m := dailyRe.FindStringSubmatch(expr); m != nil {
		hh, _ := strconv.Atoi(m[1])
		mm, _ := strconv.Atoi(m[2])
		cand := time.Date(now.Year(), now.Month(), now.Day(), hh, mm, 0, 0, now.Location())
		if !cand.After(now) {
			cand = cand.AddDate(0, 0, 1)
		}
		return cand, nil
	}

	if m := everyRe.FindStringSubmatch(expr); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n < 1 {
			return time.Time{}, fmt.Errorf("schedule: every interval must be >= 1: %q", expr)
		}
		var d time.Duration
		switch m[2] {
		case "h":
			d = time.Duration(n) * time.Hour
		case "m":
			d = time.Duration(n) * time.Minute
		}
		return now.Add(d), nil
	}

	return time.Time{}, fmt.Errorf("schedule: invalid expression %q (want daily@HH:MM or every:Nh|Nm)", expr)
}

// DefaultPath returns the machine-level schedules file, honoring PACTIFY_HOME
// (mirrors internal/registry) then falling back to ~/.pactify/schedules.json.
func DefaultPath() string {
	if h := os.Getenv("PACTIFY_HOME"); h != "" {
		return filepath.Join(h, "schedules.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".pactify", "schedules.json")
}

// Load reads the schedule list from path. A missing file is an empty list (not
// an error).
func Load(path string) ([]Schedule, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil, nil
	}
	var out []Schedule
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("schedule: parse %s: %w", path, err)
	}
	return out, nil
}

// Save writes the schedule list to path atomically (tmp + rename), creating the
// parent directory if needed.
func Save(path string, scheds []Schedule) error {
	if scheds == nil {
		scheds = []Schedule{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(scheds, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
