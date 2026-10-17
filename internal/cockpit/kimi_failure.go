package cockpit

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var kimiResetTime = regexp.MustCompile(`\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})`)
var kimiServerError = regexp.MustCompile(`(?i)\b(?:(?:http(?:/\d(?:\.\d)?)?|status(?:\s+code)?|code)\s*[:=]?\s*(?:5\d\d|5xx)\b|503\s+service\s+unavailable\b)`)
var kimiAuthError = regexp.MustCompile(`(?i)\b(?:auth|unauthorized|authentication|auth_error|auth_required|401|403)\b|invalid api key`)

func kimiFailure(message string, raw json.RawMessage, now time.Time) Failure {
	// Preserve ACP terminal/auth evidence separately from wire evidence. Specific
	// ACP codes take precedence; generic end_turn/message errors defer to wire.
	var envelope struct {
		ACP      json.RawMessage `json:"acp"`
		ACPError json.RawMessage `json:"acpError"`
		Wire     json.RawMessage `json:"wire"`
	}
	if json.Unmarshal(raw, &envelope) == nil && len(envelope.Wire) > 0 {
		for _, evidence := range []json.RawMessage{envelope.ACP, envelope.ACPError} {
			if f := acpFailure("", evidence, nil); f.Class != "other" {
				return f
			}
		}
		raw = envelope.Wire
	}
	if f := acpFailure("", raw, nil); f.Class != "other" {
		return f
	}
	var wire struct {
		Error *KimiWireError `json:"error"`
	}
	if json.Unmarshal(raw, &wire) == nil && wire.Error != nil {
		code := strings.ToLower(wire.Error.Code)
		switch {
		case strings.Contains(code, "context") && strings.Contains(code, "overflow"):
			return Failure{Class: "other"}
		case kimiAuthError.MatchString(strings.ReplaceAll(code, ".", " ")):
			return Failure{Class: "auth"}
		case strings.Contains(code, "filtered") || strings.Contains(code, "content_filter"):
			return Failure{Class: "refusal"}
		case strings.Contains(code, "connection_error") || strings.Contains(code, "overloaded") || strings.Contains(code, "timeout"):
			return Failure{Class: "transient"}
		}
		if d := wire.Error.Details; d != nil && d.StatusCode >= 500 && d.StatusCode <= 599 {
			return Failure{Class: "transient"}
		}
		message = wire.Error.Code + ": " + wire.Error.Message
	}
	lower := strings.ToLower(message)
	if strings.Contains(lower, "context") && (strings.Contains(lower, "overflow") || strings.Contains(lower, "exceed")) {
		return Failure{Class: "other"}
	}
	// Connection retry exhaustion says nothing about subscription balance.
	for _, word := range []string{"connection_error", "connection error", "overloaded", "timeout", "timed out"} {
		if strings.Contains(lower, word) {
			return Failure{Class: "transient"}
		}
	}
	if kimiServerError.MatchString(lower) {
		return Failure{Class: "transient"}
	}
	if kimiAuthError.MatchString(lower) || strings.Contains(lower, "provided authorization grant is invalid") {
		return Failure{Class: "auth"}
	}
	for _, word := range []string{"quota", "insufficient balance", "credit balance", "usage limit", "weekly limit", "daily limit", "subscription limit", "insufficient credits"} {
		if strings.Contains(lower, word) {
			f := Failure{Class: "quota"}
			if stamp := kimiResetTime.FindString(message); stamp != "" {
				if t, err := time.Parse(time.RFC3339Nano, stamp); err == nil && t.After(now) {
					f.ResetAt = &t
				}
			}
			return f
		}
	}
	for _, word := range []string{"filtered", "content_filter"} {
		if strings.Contains(lower, word) {
			return Failure{Class: "refusal"}
		}
	}
	for _, word := range []string{"rate_limit", "rate limit", "429"} {
		if strings.Contains(lower, word) {
			return Failure{Class: "transient"}
		}
	}
	return acpFailure(message, raw, kimiFailureRules)
}
