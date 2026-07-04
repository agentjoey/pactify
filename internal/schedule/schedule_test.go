package schedule

import (
	"path/filepath"
	"testing"
	"time"
)

func TestValidateTable(t *testing.T) {
	valid := []string{
		"daily@03:00",
		"daily@0:00",
		"daily@23:59",
		"daily@9:05",
		"every:1h",
		"every:6h",
		"every:24h",
		"every:1m",
		"every:90m",
		"  daily@12:30  ", // trimmed
	}
	for _, e := range valid {
		if err := Validate(e); err != nil {
			t.Errorf("Validate(%q) = %v, want nil", e, err)
		}
	}

	invalid := []string{
		"",
		"daily",
		"daily@24:00",    // hour out of range
		"daily@12:60",    // minute out of range
		"daily@12",       // no minute
		"daily@12:5",     // minute must be 2 digits
		"every:0h",       // interval must be >= 1
		"every:0m",       // interval must be >= 1
		"every:h",        // no number
		"every:5",        // no unit
		"every:5s",       // seconds unsupported
		"every:-1h",      // negative
		"weekly@03:00",   // unsupported form
		"daily@03:00:00", // seconds unsupported
		"0 3 * * *",      // cron unsupported
	}
	for _, e := range invalid {
		if err := Validate(e); err == nil {
			t.Errorf("Validate(%q) = nil, want error", e)
		}
	}
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	// Fixed-zone parse so tests don't depend on the host TZ database.
	ts, err := time.Parse("2006-01-02T15:04:05", s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ts
}

func TestNextFireDaily(t *testing.T) {
	cases := []struct {
		now, expr, want string
	}{
		// later today
		{"2026-07-05T02:00:00", "daily@03:00", "2026-07-05T03:00:00"},
		// already passed today → tomorrow (cross-midnight)
		{"2026-07-05T04:00:00", "daily@03:00", "2026-07-06T03:00:00"},
		// exactly at fire time → next day (strictly after now)
		{"2026-07-05T03:00:00", "daily@03:00", "2026-07-06T03:00:00"},
		// midnight target, evening now → next midnight
		{"2026-07-05T23:30:00", "daily@00:00", "2026-07-06T00:00:00"},
		// month boundary
		{"2026-07-31T12:00:00", "daily@06:00", "2026-08-01T06:00:00"},
	}
	for _, c := range cases {
		got, err := NextFire(mustTime(t, c.now), c.expr)
		if err != nil {
			t.Fatalf("NextFire(%s,%s): %v", c.now, c.expr, err)
		}
		if want := mustTime(t, c.want); !got.Equal(want) {
			t.Errorf("NextFire(%s,%s) = %s, want %s", c.now, c.expr, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestNextFireEvery(t *testing.T) {
	now := mustTime(t, "2026-07-05T02:00:00")
	cases := []struct {
		expr string
		want string
	}{
		{"every:6h", "2026-07-05T08:00:00"},
		{"every:1h", "2026-07-05T03:00:00"},
		{"every:24h", "2026-07-06T02:00:00"}, // crosses midnight
		{"every:30m", "2026-07-05T02:30:00"},
		{"every:1m", "2026-07-05T02:01:00"},
		{"every:90m", "2026-07-05T03:30:00"},
	}
	for _, c := range cases {
		got, err := NextFire(now, c.expr)
		if err != nil {
			t.Fatalf("NextFire(%s): %v", c.expr, err)
		}
		if want := mustTime(t, c.want); !got.Equal(want) {
			t.Errorf("NextFire(%s) = %s, want %s", c.expr, got.Format(time.RFC3339), c.want)
		}
	}
}

func TestNextFireInvalid(t *testing.T) {
	if _, err := NextFire(time.Now(), "nonsense"); err == nil {
		t.Fatal("NextFire(nonsense) = nil error, want error")
	}
}

func TestLoadSaveRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "schedules.json")

	// Missing file → empty, no error.
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load(missing): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Load(missing) = %v, want empty", got)
	}

	in := []Schedule{
		{ID: "s1", Project: "linx", Feature: "feat-x", Expr: "daily@03:00", Enabled: true},
		{ID: "s2", Project: "pactify", Expr: "every:6h", Enabled: false},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != 2 || out[0].ID != "s1" || out[0].Feature != "feat-x" || out[1].Expr != "every:6h" || out[1].Enabled {
		t.Fatalf("round-trip mismatch: %+v", out)
	}
}
