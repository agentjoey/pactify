package cockpit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// KimiRecord summarizes request models and per-turn usage without counting step duplicates.
type KimiRecord struct {
	Models       []string
	InputTokens  int
	OutputTokens int
	CacheRead    int
	Turns        int
}

// ReadKimiRecord reads Kimi's session index and the selected main agent's wire log.
func ReadKimiRecord(kimiHome, sessionID string) (KimiRecord, error) {
	var record KimiRecord
	index := filepath.Join(kimiHome, "session_index.jsonl")
	sessionDir := ""
	err := readKimiLines(index, false, func(line []byte) error {
		var entry struct {
			SessionID  string `json:"sessionId"`
			SessionDir string `json:"sessionDir"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		if entry.SessionID == sessionID {
			sessionDir = entry.SessionDir
		}
		return nil
	})
	if err != nil {
		return record, err
	}
	if sessionDir == "" {
		return record, fmt.Errorf("kimi session %q not found in %s", sessionID, index)
	}
	seen := map[string]bool{}
	err = readKimiLines(filepath.Join(sessionDir, "agents", "main", "wire.jsonl"), true, func(line []byte) error {
		var entry struct {
			Type       string `json:"type"`
			Model      string `json:"model"`
			ModelAlias string `json:"modelAlias"`
			UsageScope string `json:"usageScope"`
			Usage      struct {
				InputOther         int `json:"inputOther"`
				Output             int `json:"output"`
				InputCacheRead     int `json:"inputCacheRead"`
				InputCacheCreation int `json:"inputCacheCreation"`
			} `json:"usage"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		switch entry.Type {
		case "llm.request":
			model := entry.ModelAlias
			if model == "" {
				model = entry.Model
			}
			if model != "" && !seen[model] {
				record.Models = append(record.Models, model)
				seen[model] = true
			}
		case "usage.record":
			if entry.UsageScope == "turn" {
				record.InputTokens += entry.Usage.InputOther + entry.Usage.InputCacheRead + entry.Usage.InputCacheCreation
				record.OutputTokens += entry.Usage.Output
				record.CacheRead += entry.Usage.InputCacheRead
				record.Turns++
			}
		}
		return nil
	})
	if err != nil {
		return KimiRecord{}, err
	}
	return record, nil
}

func readKimiLines(path string, allowTruncatedTail bool, visit func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	r := bufio.NewReader(f)
	for lineNo := 1; ; lineNo++ {
		line, err := r.ReadBytes('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read %s: %w", path, err)
		}
		if len(line) > 0 && string(line) != "\n" {
			if parseErr := visit(line); parseErr != nil {
				if allowTruncatedTail {
					// Decode distinguishes incomplete JSON from corruption; Peek also
					// detects EOF when the final line has a trailing newline.
					var raw json.RawMessage
					if json.NewDecoder(bytes.NewReader(bytes.TrimSpace(line))).Decode(&raw) == io.ErrUnexpectedEOF {
						if _, peekErr := r.Peek(1); peekErr == io.EOF {
							return nil
						}
					}
				}
				return fmt.Errorf("%s:%d: %w", path, lineNo, parseErr)
			}
		}
		if err == io.EOF {
			return nil
		}
	}
}
