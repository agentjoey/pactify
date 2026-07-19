package remoteexec

import "testing"

// Security regression — review finding H4 (high).
//
// The remote stint RPC carries a Branch the dispatcher never validates: Handle
// checks Seat/AgentKind/Task non-empty but not Branch. An option-like Branch
// (e.g. "--mirror") flows through StintRequest to serve's runLifecycle →
// gitx.Push(dir, "origin", branch) → `git push origin --mirror`, a destructive
// mirror push that deletes remote refs absent locally (and --upload-pack= enables
// remote command execution). The dispatcher must reject a Branch that is not a
// valid branch name BEFORE reaching the Stinter.
//
// RED until Handle validates rpc.Branch (gitx.ValidBranchName) for pact.stint.
func TestSEC_H4_StintRejectsOptionInjectionBranch(t *testing.T) {
	for _, bad := range []string{"--mirror", "--all", "--upload-pack=x", "-D"} {
		st := &fakeStinter{}
		d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Stint: st}
		r := d.Handle(RPC{
			Type: "pact.stint", Account: "acct1", Project: "known",
			Task: "t1", Seat: "kimi-worker", AgentKind: "kimi-cli", Branch: bad,
		})
		if r.OK {
			t.Errorf("H4: stint accepted option-like Branch %q (want rejected)", bad)
		}
		if st.got.Branch == bad {
			t.Errorf("H4: malicious Branch %q reached the Stinter before validation", bad)
		}
	}

	// A legitimate branch must still pass (guard against over-rejection); an empty
	// Branch (resolve-locally case) must also still be accepted — covered by the
	// existing TestHandle_Stint.
	st := &fakeStinter{}
	d := &Dispatcher{Account: "acct1", Resolve: resolverFor(&fakeEngine{}), Stint: st}
	r := d.Handle(RPC{
		Type: "pact.stint", Account: "acct1", Project: "known",
		Task: "t1", Seat: "kimi-worker", AgentKind: "kimi-cli", Branch: "feat-add-2fa",
	})
	if !r.OK {
		t.Errorf("legit branch rejected: %+v", r)
	}
	if st.got.Branch != "feat-add-2fa" {
		t.Errorf("legit stint didn't reach the Stinter: %+v", st.got)
	}
}
