package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/acp"
	"os"
	"os/exec"
	"time"
)

type opencodeMessage struct {
	Info struct {
		Role       string  `json:"role"`
		ProviderID string  `json:"providerID"`
		ModelID    string  `json:"modelID"`
		Cost       float64 `json:"cost"`
	} `json:"info"`
}

func opencodeExport(ctx context.Context, v VerificationInput) ([]opencodeMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// opencode exits before a piped stdout drains, cutting large exports at
	// 64/128 KiB (AJ-653); a regular file always receives the full JSON.
	file, err := os.CreateTemp("", "pactify-opencode-export-*.json")
	if err != nil {
		return nil, fmt.Errorf("opencode export unavailable: %w", err)
	}
	defer os.Remove(file.Name())
	defer file.Close()
	cmd := exec.CommandContext(ctx, "opencode", "export", v.SessionID)
	cmd.Dir = v.Cwd
	cmd.Env = acp.CommandEnv(v.Env)
	cmd.Stdout = file
	if err = cmd.Run(); err != nil {
		return nil, fmt.Errorf("opencode export unavailable: %w", err)
	}
	out, err := os.ReadFile(file.Name())
	if err != nil {
		return nil, fmt.Errorf("opencode export unavailable: %w", err)
	}
	var record struct {
		Messages []opencodeMessage `json:"messages"`
	}
	if err = json.Unmarshal(out, &record); err != nil {
		return nil, fmt.Errorf("opencode export invalid JSON: %w", err)
	}
	return record.Messages, nil
}
func opencodeBaseline(ctx context.Context, v VerificationInput) (int64, error) {
	messages, err := opencodeExport(ctx, v)
	return int64(len(messages)), err
}
func verifyOpencode(ctx context.Context, v VerificationInput) Verification {
	cost := 0.0
	result := Verification{CostUSD: &cost}
	if v.WireOffset < 0 {
		result.Warning = "opencode export baseline unavailable; model and job cost could not be verified"
		return result
	}
	messages, err := opencodeExport(ctx, v)
	if err != nil {
		result.Warning = err.Error()
		return result
	}
	if v.WireOffset > int64(len(messages)) {
		result.Warning = "opencode export history changed; model and job cost could not be verified"
		return result
	}
	for _, m := range messages[v.WireOffset:] {
		if m.Info.Role != "assistant" {
			continue
		}
		cost += m.Info.Cost
		if m.Info.ModelID != "" && m.Info.ProviderID != "" {
			result.Model = m.Info.ProviderID + "/" + m.Info.ModelID
			result.Verified = true
		}
	}
	if !result.Verified {
		result.Warning = "opencode export contains no new assistant model; model and job cost could not be verified"
	}
	return result
}
