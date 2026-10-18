package cockpit

import (
	"testing"
	"time"
)

func TestKimiReviewClassificationCounterexamples(t *testing.T) {
	for _, tc := range []struct{ message, class string }{
		{"Max retries exhausted: Connection error.", "transient"},
		{"503 Service Unavailable: upstream capacity exhausted", "transient"},
		{"429 RESOURCE_EXHAUSTED RPM limit", "transient"},
		{"HTTP 429", "transient"},
		{"status code: 429", "transient"},
		{"error 429: slow down", "transient"},
		{"429 Too Many Requests", "transient"},
		{"rate_limit_exceeded", "transient"},
		{"too many requests, retry later", "transient"},
		// AJ-739: digits inside a quoted path are not a status code.
		{"kimi credentials unavailable; run kimi login first: stat /tmp/TestX1429567/001/credentials: no such file or directory", "auth"},
		{"failed to read /tmp/run-4290/notes.txt", "other"},
		{"max_tokens must be <= 512", "other"},
		{"max_tokens <= 512", "other"},
		{"failed to read AUTHORS file", "other"},
		{"resource exhausted", "other"},
		{"HTTP status 503", "transient"},
		{"HTTP 5xx", "transient"},
		{"status 503", "transient"},
		{"code: 503", "transient"},
		{"quota exhausted: timeout while contacting provider", "transient"},
		{"authentication failed", "auth"},
		{"unauthorized (401)", "auth"},
		{"403 forbidden", "auth"},
		{"subscription quota exhausted", "quota"},
	} {
		t.Run(tc.message, func(t *testing.T) {
			if got := ClassifyFailure("kimi", tc.message, nil, time.Now()); got.Class != tc.class {
				t.Fatalf("%q: got %s want %s", tc.message, got.Class, tc.class)
			}
		})
	}
}
