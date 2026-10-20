package cockpit

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestAgyUnknownModelFailureClass(t *testing.T) {
	b, o := agyFixture(t, "", AccessWrite)
	o.Model = "unknown"
	_, err := b.Start(context.Background(), o)
	if !errors.Is(err, ErrModelUnavailable) || FailureFromError("agy", err, time.Now()).Class != "model" {
		t.Fatalf("unknown model must be classified as model: %v", err)
	}
	if !strings.Contains(err.Error(), `unknown agy model "unknown"; available: model-a, model-b`) {
		t.Fatalf("lost model diagnostic: %v", err)
	}
}
