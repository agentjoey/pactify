package sub

import (
	"context"
	"fmt"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// Keep the process alive until this job's async wire writer has flushed both
// usage and a terminal marker. Each marker stream is counted independently:
// a prompt may emit both turn.ended and prompt.completed.
func verifyKimiUsage(ctx context.Context, v VerificationInput) Verification {
	if v.Prompts == 0 {
		return Verification{}
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	home := v.Info.Home
	if home == "" {
		var err error
		home, err = kimiHomeDir()
		if err != nil {
			return Verification{Warning: fmt.Sprintf("Kimi wire unavailable; tokens may be incomplete: %v", err)}
		}
	}
	var latest cockpit.KimiRecord
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		record, err := cockpit.ReadKimiRecordSince(home, v.SessionID, v.WireOffset)
		if err == nil {
			latest = record
		}
		result := Verification{InputTokens: latest.InputTokens, OutputTokens: latest.OutputTokens}
		if len(latest.Models) > 0 {
			result.Model = latest.Models[len(latest.Models)-1]
			result.Verified = true
		}
		complete := record.CompletedPrompts >= v.Prompts || record.EndedTurns >= v.Prompts
		if err == nil && complete && record.Turns >= v.Prompts && (result.Verified || v.Prompts == 0) {
			return result
		}
		select {
		case <-ctx.Done():
			result.Warning = fmt.Sprintf("Kimi wire verification timed out; tokens may be incomplete (prompts=%d, prompt.completed=%d, turn.ended=%d, usage records=%d)", v.Prompts, latest.CompletedPrompts, latest.EndedTurns, latest.Turns)
			if err != nil {
				result.Warning += ": " + err.Error()
			}
			return result
		case <-ticker.C:
		}
	}
}
