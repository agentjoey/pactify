package cockpit

import (
	"context"
	"github.com/agentjoey/pactify/internal/acp"
	"reflect"
	"testing"
)

func TestKimiReadonlyUsesDefaultACPMode(t *testing.T) {
	for _, resume := range []bool{false, true} {
		f := &fakeConfigurer{}
		var info SessionInfo
		var err error
		if resume {
			info, err = applyACPResumeOpts(context.Background(), f, "s", StartOpts{Access: AccessReadOnly})
		} else {
			res := kimiLikeSession()
			res.CurrentModeID = "yolo"
			info, err = applyACPOpts(context.Background(), f, "s", res, StartOpts{Access: AccessReadOnly})
		}
		if err != nil || !reflect.DeepEqual(f.modes, []string{"default"}) || info.Access != "default" {
			t.Errorf("resume=%t modes=%v access=%s err=%v", resume, f.modes, info.Access, err)
		}
	}
}

func TestKimiReadonlyResetsUnadvertisedUnsafeMode(t *testing.T) {
	res := kimiLikeSession()
	res.CurrentModeID, res.Modes = "yolo", nil
	f := &fakeConfigurer{}
	info, err := applyACPOpts(context.Background(), f, "s", res, StartOpts{Access: AccessReadOnly})
	if err != nil || !reflect.DeepEqual(f.modes, []string{"default"}) || info.Access != "default" {
		t.Fatalf("modes=%v access=%s err=%v", f.modes, info.Access, err)
	}
}

func TestDenyNeverExitsPlanMode(t *testing.T) {
	for _, dangerous := range []acp.PermissionOption{{OptionID: "plan_reject_and_exit", Kind: "reject_once"}, {OptionID: "exit", Kind: "plan_reject_and_exit"}} {
		out := MapDecisionToOutcome(DecisionDeny, []acp.PermissionOption{dangerous, {OptionID: "reject", Kind: "reject_once"}})
		if out.OptionID != "reject" || out.Cancelled {
			t.Errorf("unsafe deny outcome: %+v", out)
		}
		out = MapDecisionToOutcome(DecisionDeny, []acp.PermissionOption{dangerous})
		if !out.Cancelled {
			t.Errorf("unsafe fallback: %+v", out)
		}
	}
}

func TestDenyPrefersPlainRejectOverPlanRevision(t *testing.T) {
	for _, plain := range []bool{false, true} {
		opts := []acp.PermissionOption{{OptionID: "plan_revise", Kind: "reject_once"}}
		if plain {
			opts = append(opts, acp.PermissionOption{OptionID: "reject", Kind: "reject_once"})
		}
		out := MapDecisionToOutcome(DecisionDeny, opts)
		if plain && (out.OptionID != "reject" || out.Cancelled) || !plain && !out.Cancelled {
			t.Errorf("plain=%t outcome=%+v", plain, out)
		}
	}
}
