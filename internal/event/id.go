package event

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// NewEventID returns a globally-unique opaque id (128-bit random hex).
func NewEventID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// NowUTC returns the current time as UTC ISO-8601 (seconds precision, "Z").
func NowUTC() string { return time.Now().UTC().Format("2006-01-02T15:04:05Z") }
