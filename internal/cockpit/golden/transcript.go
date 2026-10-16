// Package golden records sanitized protocol traffic. It never opens credentials.
package golden

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const Redacted = "[REDACTED]"

var sensitiveKey = regexp.MustCompile(`(?i)^(authorization|proxy-authorization|cookie|set-cookie|password|passwd|token|access[_-]?token|refresh[_-]?token|api[_-]?key|key|secret|client[_-]?secret|device[_-]?id|email)$`)
var patterns = []*regexp.Regexp{
	regexp.MustCompile(`\b(?:sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{20,}|github_pat_[A-Za-z0-9_]{20,}|eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)\b`),
	regexp.MustCompile(`(?i)(Bearer|Basic)\s+[A-Za-z0-9+/_=.\-]+`),
	regexp.MustCompile(`(?i)(password|passwd|token|access_token|refresh_token|api[_-]?key|device[_-]?id|authorization|cookie)\s*[=:]\s*[^\s,;"'{}]+`),
	regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	regexp.MustCompile(`/(Users|home)/[^\s"'<>\\]+`),
	regexp.MustCompile(`(?i)[A-Z]:\\Users\\[^\s"'<>]+`),
}

// localUser matches the recording machine's login name as a whole word, so
// tool output such as `ls -l` owner columns does not publish it.
var localUser = sync.OnceValue(func() *regexp.Regexp {
	name := os.Getenv("USER")
	if u, err := user.Current(); err == nil && u.Username != "" {
		name = u.Username
	}
	if len(name) < 3 || name == "root" {
		return nil
	}
	return regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
})

const UserPlaceholder = "<user>"

func Sanitize(s string) string {
	var value any
	if json.Unmarshal([]byte(s), &value) == nil {
		var visit func(any) any
		visit = func(v any) any {
			switch x := v.(type) {
			case map[string]any:
				for k, v := range x {
					if sensitiveKey.MatchString(k) {
						x[k] = Redacted
					} else {
						x[k] = visit(v)
					}
				}
			case []any:
				for i, v := range x {
					x[i] = visit(v)
				}
			case string:
				return SanitizeText(x)
			}
			return v
		}
		b, _ := json.Marshal(visit(value))
		return string(b)
	}
	return SanitizeText(s)
}
func SanitizeText(s string) string {
	if strings.Contains(s, "\n") {
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			lines[i] = Sanitize(line)
		}
		return strings.Join(lines, "\n")
	}
	if strings.Contains(s, "data: ") {
		lines := strings.Split(s, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "data: ") {
				lines[i] = "data: " + Sanitize(strings.TrimPrefix(line, "data: "))
			}
		}
		return strings.Join(lines, "\n")
	}
	// Embedded JSON (including captured JSON-RPC) receives key-aware redaction.
	if strings.HasPrefix(strings.TrimSpace(s), "{") || strings.HasPrefix(strings.TrimSpace(s), "[") && strings.TrimSpace(s) != Redacted {
		var v any
		if json.Unmarshal([]byte(s), &v) == nil {
			return Sanitize(s)
		}
	}
	if re := localUser(); re != nil {
		s = re.ReplaceAllString(s, UserPlaceholder)
	}
	for _, p := range patterns {
		s = p.ReplaceAllStringFunc(s, func(match string) string {
			if strings.Contains(match, Redacted) {
				return match
			}
			return Redacted
		})
	}
	return s
}
func Scan(s string) error {
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return scanValue(v)
	}
	if strings.Contains(s, "\n") {
		for _, line := range strings.Split(s, "\n") {
			if err := Scan(line); err != nil {
				return err
			}
		}
		return nil
	}
	if strings.HasPrefix(s, "data: ") {
		return Scan(strings.TrimPrefix(s, "data: "))
	}
	if re := localUser(); re != nil && re.MatchString(s) {
		return fmt.Errorf("transcript contains the local user name")
	}
	for _, p := range patterns {
		for _, match := range p.FindAllString(s, -1) {
			if !strings.Contains(match, Redacted) {
				return fmt.Errorf("transcript contains unsanitized sensitive data")
			}
		}
	}
	return nil
}
func scanValue(v any) error {
	switch x := v.(type) {
	case map[string]any:
		for key, value := range x {
			if sensitiveKey.MatchString(key) && value != Redacted {
				return fmt.Errorf("transcript contains sensitive field %s", key)
			}
			if err := scanValue(value); err != nil {
				return fmt.Errorf("field %s: %w", key, err)
			}
		}
	case []any:
		for i, value := range x {
			if err := scanValue(value); err != nil {
				return fmt.Errorf("item %d: %w", i, err)
			}
		}
	case string:
		// Only structured embedded values recur; a JSON scalar string must not loop.
		var embedded any
		if (strings.HasPrefix(strings.TrimSpace(x), "{") || strings.HasPrefix(strings.TrimSpace(x), "[")) && json.Unmarshal([]byte(x), &embedded) == nil {
			return scanValue(embedded)
		}
		if strings.Contains(x, "\n") || strings.HasPrefix(x, "data: ") {
			return Scan(x)
		}
		if re := localUser(); re != nil && re.MatchString(x) {
			return fmt.Errorf("transcript contains the local user name")
		}
		for i, p := range patterns {
			for _, match := range p.FindAllString(x, -1) {
				if !strings.Contains(match, Redacted) {
					return fmt.Errorf("transcript contains unsanitized sensitive data (pattern %d)", i)
				}
			}
		}
	}
	return nil
}

type Frame struct {
	Channel string `json:"channel"` // stdin, stdout, wire/<agent>, http, sse
	Method  string `json:"method,omitempty"`
	Path    string `json:"path,omitempty"`
	Status  int    `json:"status,omitempty"`
	Request string `json:"request,omitempty"`
	Data    string `json:"data"`
}
type Transcript struct {
	Notes    string          `json:"notes,omitempty"`
	Harness  string          `json:"harness"`
	Version  string          `json:"version"`
	Scenario string          `json:"scenario"`
	Resume   string          `json:"resume,omitempty"`
	Prompt   string          `json:"prompt"`
	Model    string          `json:"model,omitempty"`
	Frames   []Frame         `json:"frames"`
	Expected json.RawMessage `json:"expected"`
}
type Recorder struct {
	mu     sync.Mutex
	frames []Frame
}

func (r *Recorder) Snapshot() []Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Frame(nil), r.frames...)
}
func (r *Recorder) Add(f Frame) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f.Data = Sanitize(f.Data)
	f.Request = Sanitize(f.Request)
	r.frames = append(r.frames, f)
}
func (r *Recorder) Observe(channel string, data []byte) {
	r.Add(Frame{Channel: channel, Data: string(data)})
}
func Save(path string, t Transcript) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	b = []byte(Sanitize(string(b)))
	var pretty bytes.Buffer
	if err = json.Indent(&pretty, b, "", "  "); err != nil {
		return err
	}
	if err = Scan(pretty.String()); err != nil {
		return err
	}
	return os.WriteFile(path, append(pretty.Bytes(), '\n'), 0600)
}

// Transport captures response bodies as they are consumed. Headers are omitted:
// credentials are never required to replay an authenticated local transport.
type Transport struct {
	Base     http.RoundTripper
	Recorder *Recorder
}

func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	res, err := t.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	f := Frame{Channel: "http", Method: req.Method, Path: req.URL.Path, Status: res.StatusCode, Request: string(body)}
	res.Body = &captureBody{ReadCloser: res.Body, recorder: t.Recorder, frame: f}
	return res, nil
}

type captureBody struct {
	io.ReadCloser
	recorder *Recorder
	frame    Frame
	buf      bytes.Buffer
	once     sync.Once
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, e := b.ReadCloser.Read(p)
	b.buf.Write(p[:n])
	return n, e
}
func (b *captureBody) Close() error {
	e := b.ReadCloser.Close()
	b.once.Do(func() { b.frame.Data = b.buf.String(); b.recorder.Add(b.frame) })
	return e
}

// ManagedDirectory validates the recording boundary using metadata only. Leaf
// credential links inside it are left to the harness's normal login mechanism.
func ManagedDirectory(root, path string) error {
	if !filepath.IsAbs(root) || !filepath.IsAbs(path) {
		return fmt.Errorf("record directories must be absolute")
	}
	for _, dir := range []string{root, path} {
		info, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("record directory must be a real directory: %s", dir)
		}
	}
	resolvedRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	resolvedPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(resolvedRoot, resolvedPath)
	if err != nil || rel == "." || !filepath.IsLocal(rel) {
		return fmt.Errorf("record directory must stay below the isolated record root")
	}
	return nil
}
