package subpanel

import (
	"os"
	"strings"
	"testing"
)

func TestStructuredEventsReadOnlyAndSafe(t *testing.T) {
	s, store := fixture(t)
	records := []string{
		`{"kind":"tool","tool":{"phase":"start","name":"bash"},"raw":{"part":{"id":"oc1","state":{"status":"running","input":{"command":"go test ./... --token FLAG_SECRET"}}},"api_key":"RAW_SECRET"}}`,
		`{"kind":"tool","tool":{"phase":"end","name":"bash"},"raw":{"part":{"id":"oc1","state":{"status":"completed"}}}}`,
		`{"kind":"tool","tool":{"phase":"start","name":"shell"},"raw":{"item":{"id":"call","type":"commandExecution"},"api_key":"RAW_SECRET"}}`,
		`{"kind":"tool","tool":{"phase":"end","name":"shell"},"raw":{"item":{"id":"call","type":"commandExecution"}}}`,
		`{"kind":"tool","tool":{"phase":"output","name":""},"raw":{"toolCallId":"k1","kind":"read","status":"in_progress","title":"Reading file"}}`,
		`{"kind":"tool","tool":{"phase":"output","name":""},"raw":{"toolCallId":"k1","status":"completed"}}`,
		`{"kind":"message","text":"api_key=TEXT_SECRET password=PASS_SECRET Authorization: Bearer AUTH_SECRET TOKEN=TOKEN_SECRET","raw":{"access_token":"RAW_SECRET"}}`,
		`{"kind":"usage","usage":{"inputTokens":1,"outputTokens":2},"raw":{"threadId":"main","tokenUsage":{"total":{"inputTokens":100,"outputTokens":20}}}}`,
		`{"kind":"diff","raw":{"diff":"diff --git a/a b/a\n--- a/a\n+++ b/a\n@@ -1 +1 @@\n+new\n-old\n","secret":"RAW_SECRET"}}`,
		`{"kind":"system","state":"rate_limits","raw":{"rateLimits":{"secondary":{"usedPercent":35,"windowDurationMins":10080}}}}`,
		`{"kind":"state","state":"turn_completed"}`,
		`{"kind":"unknown","raw":{"secret":"RAW_SECRET"}}`,
		`malformed`,
	}
	if err := os.WriteFile(store.LogPath("job-00"), []byte(strings.Join(records, "\n")+"\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"", "wrong"} {
		if w := request(s, "GET", "/api/jobs/job-00/events", token); w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	if w := request(s, "POST", "/api/jobs/job-00/events", s.token); w.Code != 405 {
		t.Fatal(w.Code)
	}
	w := request(s, "GET", "/api/jobs/job-00/events", s.token)
	if w.Code != 200 || w.Header().Get("Content-Type") != "text/event-stream" {
		t.Fatal(w.Code, w.Body.String())
	}
	body := w.Body.String()
	for _, secret := range []string{"RAW_SECRET", "TEXT_SECRET", "PASS_SECRET", "AUTH_SECRET", "FLAG_SECRET", "TOKEN_SECRET", `"raw"`, "unknown", "malformed"} {
		if strings.Contains(body, secret) {
			t.Fatalf("unsafe field %s in %s", secret, body)
		}
	}
	for _, expected := range []string{`"inputTokens":100`, `"outputTokens":20`, `"type":"shell"`, `"id":"/oc1"`, `"text":"go test ./... --token [redacted]"`, `"type":"read"`, `"status":"completed"`, `"files":1,"added":1,"deleted":1`, "7-day window: 35% used", "event: done"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("missing %s in %s", expected, body)
		}
	}
}

func TestStructuredEventTailUsesBoundedReader(t *testing.T) {
	s, store := fixture(t)
	// The first fragment in the 1 MiB window must never be treated as a record.
	data := `{"kind":"message","text":"OUTSIDE_WINDOW"}` + "\n" + strings.Repeat("x", 1<<20) + "\n"
	for i := 0; i < 110; i++ {
		data += `{"kind":"message","text":"tail","final":true}` + "\n"
	}
	data += `{"kind":"message","text":"unfinished`
	if err := os.WriteFile(store.LogPath("job-00"), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	body := request(s, "GET", "/api/jobs/job-00/events", s.token).Body.String()
	if strings.Contains(body, "OUTSIDE_WINDOW") || strings.Contains(body, "unfinished") || strings.Count(body, `"text":"tail"`) != 100 {
		t.Fatal("incorrect initial tail", body)
	}
}
