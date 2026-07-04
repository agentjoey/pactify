package remoteexec

import (
	"encoding/json"
	"fmt"
)

// pactTypes is the set of rpc `type` discriminators this machine handles — the
// mirror of the pactify members added to the shared wire RpcRequest union (P-b).
// A type outside this set is not for pactify (e.g. a linx machine rpc) and is
// rejected by ParseRPC so it never reaches the pact engine.
var pactTypes = map[string]bool{
	"pact.assign":     true,
	"pact.accept":     true,
	"pact.changes":    true,
	"pact.merge":      true,
	"pact.checkpoint": true,
}

// IsPactType reports whether t is a pactify pact-verb rpc type.
func IsPactType(t string) bool { return pactTypes[t] }

// wireRPC is the on-the-wire JSON shape (cloud/wire RpcRequest pactify members).
// Account is intentionally absent: the relay scopes delivery by the socket's
// authenticated account, and the Executor injects it — the wire never carries it.
type wireRPC struct {
	Type     string   `json:"type"`
	MachineID string  `json:"machineId"`
	Project  string   `json:"project"`
	Task     string   `json:"task"`
	Feature  string   `json:"feature"`
	Branch   string   `json:"branch"`
	Owner    string   `json:"owner"`
	Reviewer string   `json:"reviewer"`
	Spec     string   `json:"spec"`
	Reason   string   `json:"reason"`
	Evidence string   `json:"evidence"`
	Deps     []string `json:"deps"`
}

// ParseRPC decodes a raw wire rpc payload into an RPC, rejecting anything that is
// not a pactify pact-verb type. Account is left empty for the Executor to inject
// from the authenticated connection. It does not validate per-verb required
// fields — that is the pact engine's job (Handle surfaces the verb's own error),
// keeping this a thin transport decode.
func ParseRPC(raw []byte) (RPC, error) {
	var w wireRPC
	if err := json.Unmarshal(raw, &w); err != nil {
		return RPC{}, fmt.Errorf("decode rpc: %w", err)
	}
	if !IsPactType(w.Type) {
		return RPC{}, fmt.Errorf("not a pact rpc type: %q", w.Type)
	}
	return RPC{
		Type:     w.Type,
		Project:  w.Project,
		Task:     w.Task,
		Feature:  w.Feature,
		Branch:   w.Branch,
		Owner:    w.Owner,
		Reviewer: w.Reviewer,
		Spec:     w.Spec,
		Reason:   w.Reason,
		Evidence: w.Evidence,
		Deps:     w.Deps,
	}, nil
}
