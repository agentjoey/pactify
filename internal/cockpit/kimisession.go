package cockpit

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"syscall"
	"time"
)

// kimi-code 2.x keeps archive state in two places (verified with kimi 2.2.0,
// 2026-10-15): each session's state.json ("archived" + "archivedAt"), which the
// CLI and ACP session/list read, and the persistent search index behind
// `kimi web` and Kimi Remote Control, which only kimi's own server API updates.
// ArchiveKimiSessionsViaServer is the complete, official path; ArchiveKimiSession
// is the file-level fallback that hides a session from the CLI lists only. kimi
// clears the archive itself when a session is loaded again.

// ArchiveKimiSession writes kimi's archive fields into sessionID's state.json the
// way kimi's own archive does, keeping every other field, and returns the session
// directory.
func ArchiveKimiSession(kimiHome, sessionID string) (string, error) {
	dir, err := kimiSessionDir(kimiHome, sessionID)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, "state.json")
	var state map[string]any
	if err := readJSONFile(path, &state); err != nil {
		return "", err
	}
	state["archived"] = true
	state["archivedAt"] = time.Now().UnixMilli()
	return dir, writeJSONFile(path, state)
}

func readJSONFile(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// writeJSONFile replaces path atomically, keeping its permissions.
func writeJSONFile(path string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	mode := os.FileMode(0o600)
	if fi, err := os.Stat(path); err == nil {
		mode = fi.Mode().Perm()
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pactify-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(mode); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// ArchiveKimiSessionsViaServer archives sessions through kimi's own server API
// (the call the kimi web UI makes), so the CLI, the web UI and Remote Control all
// agree. It starts a private, loopback-only `kimi web --no-open` on a free port,
// reads the bearer token from its startup output, and stops it afterwards.
func ArchiveKimiSessionsViaServer(ctx context.Context, ids []string) error {
	port, err := freeLoopbackPort()
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "kimi", "web", "--no-open", "--port", strconv.Itoa(port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start kimi web: %w", err)
	}
	defer func() {
		// SIGTERM lets kimi remove its server/instances record.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	}()
	token := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		for sc.Scan() {
			if t := parseKimiWebToken(sc.Text()); t != "" {
				token <- t
				break
			}
		}
		_, _ = io.Copy(io.Discard, out)
	}()
	select {
	case t := <-token:
		return archiveKimiSessionsHTTP(ctx, fmt.Sprintf("http://127.0.0.1:%d", port), t, ids)
	case <-ctx.Done():
		return fmt.Errorf("kimi web did not report its token: %w", ctx.Err())
	}
}

var (
	kimiTokenLine = regexp.MustCompile(`Token:\s+([A-Za-z0-9_-]{20,})`)
	ansiEscape    = regexp.MustCompile(`\x1b\[[0-9;]*[A-Za-z]`)
)

// parseKimiWebToken reads the token from kimi web's startup banner, which may be
// colored.
func parseKimiWebToken(line string) string {
	if m := kimiTokenLine.FindStringSubmatch(ansiEscape.ReplaceAllString(line, "")); m != nil {
		return m[1]
	}
	return ""
}

// archiveKimiSessionsHTTP calls POST /api/v2/sessions:archive and requires every
// id to succeed.
func archiveKimiSessionsHTTP(ctx context.Context, baseURL, token string, ids []string) error {
	body, _ := json.Marshal(map[string]any{"ids": ids})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v2/sessions:archive", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	// The server may still be binding its port right after printing the token.
	var resp *http.Response
	for attempt := 0; ; attempt++ {
		resp, err = http.DefaultClient.Do(req)
		if err == nil || attempt == 20 || ctx.Err() != nil {
			break
		}
		time.Sleep(250 * time.Millisecond)
		req, _ = http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/v2/sessions:archive", bytes.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Type", "application/json")
	}
	if err != nil {
		return fmt.Errorf("kimi archive API: %w", err)
	}
	defer resp.Body.Close()
	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Succeeded int `json:"succeeded"`
			Failed    int `json:"failed"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return fmt.Errorf("kimi archive API: HTTP %d: %w", resp.StatusCode, err)
	}
	if resp.StatusCode != http.StatusOK || r.Code != 0 || r.Data.Succeeded != len(ids) {
		return fmt.Errorf("kimi archive API: HTTP %d code %d %q (%d/%d archived)", resp.StatusCode, r.Code, r.Msg, r.Data.Succeeded, len(ids))
	}
	return nil
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
