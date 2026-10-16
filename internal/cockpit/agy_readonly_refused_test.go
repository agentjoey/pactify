package cockpit

import (
	"context"
	"testing"
)

func TestAgyReadOnlyRestored(t *testing.T) {
	b, o := agyV2Fixture(t, "")
	o.Access = AccessReadOnly
	o.MinimalConfig = true
	s, err := b.Start(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ev := agyTurn(t, s)
	if ev[len(ev)-1].State != StateTurnCompleted {
		t.Fatal(ev)
	}
}
