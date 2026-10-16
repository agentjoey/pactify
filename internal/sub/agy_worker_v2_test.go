package sub

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAgyV2WorkerPeer(t *testing.T) {
	if os.Getenv("AJ711_PEER") != "1" {
		return
	}
	args := strings.Join(os.Args, " ")
	if !strings.Contains(args, "--model ") {
		os.Exit(91)
	}
	if strings.Contains(args, " models ") {
		fmt.Println("gemini-3.8-flash-low\tFlash")
		os.Exit(0)
	}
	enc := json.NewEncoder(os.Stdout)
	mode := os.Getenv("AJ711_SCENARIO")
	if strings.Contains(args, "-p /usage") {
		fraction := .8
		if mode == "quota" {
			fraction = 0
		}
		_ = enc.Encode(map[string]any{"status": "SUCCESS", "num_turns": 0, "command": map[string]any{"name": "usage", "data": map[string]any{"groups": []any{map[string]any{"name": "Gemini Models", "buckets": []any{map[string]any{"id": "gemini-weekly", "window": "weekly", "remaining_fraction": fraction, "reset_time": "2099-10-23T00:40:01Z"}}}}}}})
		os.Exit(0)
	}
	_ = enc.Encode(map[string]any{"event": "init", "conversation_id": "worker-conversation", "init": map[string]string{"model": "gemini-3.8-flash-low"}})
	scan := bufio.NewScanner(os.Stdin)
	for round := 1; scan.Scan(); round++ {
		var input struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(scan.Bytes(), &input) != nil || input.Event != "user" {
			os.Exit(92)
		}
		if mode == "restart" {
			marker := os.Getenv("AJ711_MARKER")
			if _, err := os.Stat(marker); os.IsNotExist(err) {
				_ = os.WriteFile(marker, []byte("first"), 0600)
				os.Exit(1)
			}
			if !strings.Contains(args, "--conversation worker-conversation") {
				os.Exit(93)
			}
		}
		status, response, failure := "SUCCESS", "Actual result verified", ""
		if mode == "quota" {
			status, response, failure = "ERROR", "", "Individual quota reached"
		}
		if mode == "transient" && round == 1 {
			status, response, failure = "ERROR", "", "503 UNAVAILABLE"
		}
		if mode == "ignored-delete" {
			_ = os.Remove("victim")
		}
		_ = enc.Encode(map[string]any{"event": "step_update", "step_update": map[string]any{"step_index": round, "step_type": "agent_response", "state": "DONE", "text_delta": response, "usage": map[string]int{"input_tokens": 10, "output_tokens": 2}}})
		_ = enc.Encode(map[string]any{"event": "result", "result": map[string]any{"status": status, "response": response, "error": failure, "conversation_id": "worker-conversation", "usage": map[string]int{"input_tokens": 1000 * round, "output_tokens": 200 * round}}})
	}
	os.Exit(0)
}
func TestAgyV2WorkerReplay(t *testing.T) {
	for _, mode := range []string{"write", "readonly", "ignored-delete", "transient", "restart", "quota"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("PACTIFY_HOME", t.TempDir())
			fakeAgyUserHome(t)
			exe, _ := os.Executable()
			bin := t.TempDir()
			script := fmt.Sprintf("#!/bin/sh\nexec '%s' -test.run=^TestAgyV2WorkerPeer$ -- \"$@\"\n", strings.ReplaceAll(exe, "'", "'\\''"))
			if err := os.WriteFile(filepath.Join(bin, "agy"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("AJ711_PEER", "1")
			t.Setenv("AJ711_SCENARIO", mode)
			t.Setenv("AJ711_MARKER", filepath.Join(t.TempDir(), "marker"))
			repo := t.TempDir()
			if err := initDoctorRepo(repo, false); err != nil {
				t.Fatal(err)
			}
			access := "write"
			if mode == "readonly" || mode == "ignored-delete" {
				access = "read-only"
			}
			if mode == "ignored-delete" {
				_ = os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("victim\n"), 0600)
				_ = os.WriteFile(filepath.Join(repo, "victim"), []byte("keep"), 0600)
			}
			store := Store{Root: filepath.Join(os.Getenv("PACTIFY_HOME"), "jobs")}
			st, err := store.Create("job", Request{Harness: "agy", Access: access, Cwd: repo, Brief: "Review actual results"})
			if err != nil {
				t.Fatal(err)
			}
			st.ConfigMode = "minimal"
			_ = store.Save(st)
			h, _ := Lookup("agy")
			h.Version = nil
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := RunJob(ctx, store, "job", h, WorkerConfig{RetryBackoff: func(int) time.Duration { return time.Millisecond }})
			if err != nil {
				t.Fatal(err)
			}
			want := StatusSucceeded
			class := ""
			if mode == "quota" {
				want, class = StatusFailed, "quota"
			}
			if mode == "ignored-delete" {
				want, class = StatusFailed, "readonly_violation"
			}
			if result.Status != want || result.FailureClass != class || result.Sandboxed {
				t.Fatalf("result=%+v", result)
			}
			if len(result.QuotaSnapshots) != 2 || result.RateLimit == "" {
				t.Fatalf("missing snapshots: %+v", result)
			}
			if mode == "quota" && result.QuotaResetAt == nil {
				t.Fatal("missing quota ResetAt")
			}
			if mode == "transient" || mode == "restart" {
				if len(result.Retries) != 1 || result.SessionID != "worker-conversation" {
					t.Fatal(result)
				}
			}
			if mode == "transient" && result.InputTokens != 20 {
				t.Fatal(result.InputTokens)
			}
			report, err := store.CostReport(time.Time{}, "harness", time.Now().Add(time.Second))
			if err != nil || len(report.QuotaSnapshots) != 1 || len(report.QuotaSnapshots[0].Buckets) != 1 {
				t.Fatalf("report=%+v err=%v", report, err)
			}
		})
	}
}
