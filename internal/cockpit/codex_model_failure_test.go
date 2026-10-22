package cockpit

import (
	"context"
	"testing"
	"time"
)

// A server that starts a different model than requested is a model refusal, so
// the CLI can report it as an invalid argument without matching error text.
func TestCodexModelMismatchClassifiesAsModel(t *testing.T) {
	s := newFakeCodexServer(t)
	s.handle("thread/start", func(wireMessage) any {
		return map[string]any{"thread": map[string]any{"id": "t"}, "model": "served-model"}
	})
	s.handle("model/list", func(wireMessage) any {
		return map[string]any{"data": []map[string]string{{"model": "served-model"}}}
	})
	_, _, err := s.client.threadStartOpts(context.Background(), StartOpts{RepoDir: "/repo", Model: "wanted-model"})
	if err == nil {
		t.Fatal("expected a model mismatch")
	}
	if got := FailureFromError("codex", err, time.Now()).Class; got != "model" {
		t.Fatalf("class = %q for %v", got, err)
	}
}
