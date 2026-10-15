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
	MCPToolsDiscovered bool
	Models             []string
	InputTokens        int
	OutputTokens       int
	CacheRead          int
	Turns              int
	CompletedPrompts   int
	EndedTurns         int
}

// ReadKimiRecord reads Kimi's session index and the selected main agent's wire log.
func ReadKimiRecord(kimiHome, sessionID string) (KimiRecord, error) {
	return ReadKimiRecordSince(kimiHome, sessionID, 0)
}

// KimiWireOffset snapshots the wire length before a resumed job sends a prompt.
// A session whose wire has not been created yet has an empty baseline.
func KimiWireOffset(kimiHome, sessionID string) (int64, error) {
	dir, err := kimiSessionDir(kimiHome, sessionID)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(filepath.Join(dir, "agents", "main", "wire.jsonl"))
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// ReadKimiRecordSince reads only records appended after the job's baseline.
func ReadKimiRecordSince(kimiHome, sessionID string, offset int64) (KimiRecord, error) {
	var record KimiRecord
	sessionDir, err := kimiSessionDir(kimiHome, sessionID)
	if err != nil {
		return record, err
	}
	seen := map[string]bool{}
	err = readKimiLinesFrom(filepath.Join(sessionDir, "agents", "main", "wire.jsonl"), offset, true, func(line []byte) error {
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
		case "prompt.completed":
			record.CompletedPrompts++
		case "turn.ended":
			record.EndedTurns++
		case "mcp.tools_discovered":
			record.MCPToolsDiscovered = true
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
	return readKimiLinesFrom(path, 0, allowTruncatedTail, visit)
}

func readKimiLinesFrom(path string, offset int64, allowTruncatedTail bool, visit func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if offset < 0 || offset > info.Size() {
		return fmt.Errorf("kimi wire baseline %d is outside %s (%d bytes)", offset, path, info.Size())
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
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

// kimiSessionDir resolves a session's directory through kimi's session_index.jsonl.
func kimiSessionDir(kimiHome, sessionID string) (string, error) {
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
		return "", err
	}
	if sessionDir == "" {
		return "", fmt.Errorf("kimi session %q not found in %s: %w", sessionID, index, os.ErrNotExist)
	}
	return sessionDir, nil
}

// KimiSessionListed inspects only the user session index. An absent index means
// there are no listed sessions; malformed or unreadable indexes fail closed.
func KimiSessionListed(kimiHome, sessionID string) (bool, error) {
	listed := false
	err := readKimiLines(filepath.Join(kimiHome, "session_index.jsonl"), false, func(line []byte) error {
		var entry struct {
			SessionID string `json:"sessionId"`
		}
		if err := json.Unmarshal(line, &entry); err != nil {
			return err
		}
		if entry.SessionID == sessionID {
			listed = true
		}
		return nil
	})
	if os.IsNotExist(err) {
		return false, nil
	}
	return listed, err
}
