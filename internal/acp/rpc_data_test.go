package acp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRPCErrorDataV2(t *testing.T) {
	var e rpcError
	if err := json.Unmarshal([]byte(`{"code":-32603,"message":"Provider failed","data":{"errorName":"APIError","statusCode":503,"isRetryable":true,"message":"Try later"}}`), &e); err != nil {
		t.Fatal(err)
	}
	for _, part := range []string{"APIError", "503", "Try later"} {
		if !strings.Contains(e.Error(), part) {
			t.Errorf("error missing %q: %s", part, e.Error())
		}
	}
}
