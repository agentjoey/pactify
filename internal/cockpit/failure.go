package cockpit

import (
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Failure is additive metadata; callers retain the original error text.
type Failure struct {
	Class   string
	ResetAt *time.Time
}

type failureRule struct{ text, class string }

func matchFailure(message string, rules []failureRule) Failure {
	message = strings.ToLower(message)
	for _, rule := range rules {
		if strings.Contains(message, rule.text) {
			return Failure{Class: rule.class}
		}
	}
	return Failure{Class: "other"}
}

// ClassifyFailure uses the harness adapter's structured evidence before its text table.
// raw is an error/terminal payload or a codex rate_limits payload, never task output.
func ClassifyFailure(harness, message string, raw json.RawMessage, now time.Time) Failure {
	switch harness {
	case "codex":
		return codexFailure(message, raw, now)
	case "agy":
		return agyFailure(message, now)
	case "kimi":
		return acpFailure(message, raw, kimiFailureRules)
	case "opencode":
		return opencodeFailure(message, raw)
	default:
		return Failure{Class: "other"}
	}
}

// FailureFromError preserves typed ACP codes and model refusals through wrapped errors.
func FailureFromError(harness string, err error, now time.Time) Failure {
	if err == nil {
		return Failure{Class: "other"}
	}
	if errors.Is(err, ErrModelUnavailable) || errors.Is(err, ErrModelUnsupported) {
		return Failure{Class: "model"}
	}
	var httpError *opencodeHTTPError
	if harness == "opencode" && errors.As(err, &httpError) {
		raw, _ := json.Marshal(map[string]any{"name": "APIError", "data": map[string]any{"statusCode": httpError.status, "message": httpError.message}})
		return opencodeFailure(err.Error(), raw)
	}
	var coded interface{ RPCCode() int }
	var raw json.RawMessage
	if errors.As(err, &coded) {
		raw, _ = json.Marshal(map[string]any{"code": coded.RPCCode(), "data": rpcErrorData(err)})
	}
	return ClassifyFailure(harness, err.Error(), raw, now)
}

func rpcErrorData(err error) json.RawMessage {
	var data interface{ RPCData() json.RawMessage }
	if errors.As(err, &data) {
		return data.RPCData()
	}
	return nil
}
