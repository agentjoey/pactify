package subpanel

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestHarnessesReadOnlyAPI(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("PATH", t.TempDir())
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(`{"defaults":{"codex":{"model":"selected","effort":"high"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	store, _ := sub.DefaultStore()
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/harnesses?refresh=1", s.token)
	var body struct {
		Harnesses []sub.HarnessInfo `json:"harnesses"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Harnesses) != 5 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, h := range body.Harnesses {
		if h.Installed || h.Version != "" || h.Models.Items == nil || len(h.Models.Items) != 0 || h.Models.Error == "" || h.Models.FetchedAt.IsZero() || h.Efforts == nil {
			t.Fatal(h)
		}
		if h.Experimental != (h.Name == "dsh") || !h.ReadOnly || h.Sandboxed != (h.Name == "codex" || h.Name == "dsh") {
			t.Fatalf("promotion or access flags: %+v", h)
		}
		if h.Name == "codex" && (h.Default.Model != "selected" || !h.ReadOnly || h.Experimental) {
			t.Fatal(h)
		}
	}
	for _, action := range []string{"cancel", "resume", "apply", "discard"} {
		w := request(s, "POST", "/api/jobs/example/"+action, s.token)
		if w.Code != 405 && w.Code != 404 {
			t.Fatal(action, w.Code)
		}
	}
	if w := request(s, "POST", "/api/harnesses/refresh", s.token); w.Code != 405 && w.Code != 404 {
		t.Fatalf("panel POST /api/harnesses/refresh: %d %s", w.Code, w.Body.String())
	}
}

func TestHarnessesGetIgnoresRefreshAndPanelRejectsPostRefresh(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	bins := t.TempDir()
	marker := filepath.Join(t.TempDir(), "executed")
	codexPath := filepath.Join(bins, "codex")
	if err := os.WriteFile(codexPath, []byte("#!/bin/sh\nprintf executed > '"+marker+"'\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bins)
	resolved, err := filepath.EvalSymlinks(codexPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	catalog := map[string]any{}
	for _, name := range sub.Names() {
		if name == "codex" {
			catalog[name] = map[string]any{
				"Executable": map[string]any{"Path": resolved, "Size": info.Size(), "ModTime": info.ModTime().UTC()},
				"Version":    "0.162.1",
				"Models":     map[string]any{"items": []string{"cached-model"}, "error": "", "fetchedAt": now},
			}
			continue
		}
		catalog[name] = map[string]any{
			"Models": map[string]any{"items": []string{}, "error": "harness is not installed", "fetchedAt": now},
		}
	}
	catalogFile := filepath.Join(home, "harness-catalog.json")
	data, err := json.Marshal(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(catalogFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	beforeStat, err := os.Stat(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sub.DefaultStore()
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	w := request(s, "GET", "/api/harnesses?refresh=1", s.token)
	var body struct {
		Harnesses []sub.HarnessInfo `json:"harnesses"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	foundCodex := false
	for _, h := range body.Harnesses {
		if h.Name == "codex" {
			foundCodex = true
			if !h.Installed || h.Version != "0.162.1" || len(h.Models.Items) != 1 || h.Models.Items[0] != "cached-model" {
				t.Fatalf("unexpected cached codex row: %+v", h)
			}
		}
	}
	if !foundCodex {
		t.Fatal("missing codex row")
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("GET /api/harnesses?refresh=1 executed harness CLI")
	}
	afterBytes, err := os.ReadFile(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	afterStat, err := os.Stat(catalogFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(afterBytes, data) || !afterStat.ModTime().Equal(beforeStat.ModTime()) {
		t.Fatal("GET /api/harnesses?refresh=1 rewrote harness-catalog.json")
	}
	if w := request(s, "POST", "/api/harnesses/refresh", s.token); w.Code != 405 && w.Code != 404 {
		t.Fatalf("panel POST /api/harnesses/refresh: %d %s", w.Code, w.Body.String())
	}
	// Also verify ReadOnlyHandler directly rejects POST /harnesses/refresh.
	ro := ReadOnlyHandler(store)
	rw := httptest.NewRecorder()
	ro.ServeHTTP(rw, httptest.NewRequest("POST", "/harnesses/refresh", nil))
	if rw.Code != 405 && rw.Code != 404 {
		t.Fatalf("ReadOnlyHandler POST /harnesses/refresh: %d %s", rw.Code, rw.Body.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatal("panel POST /harnesses/refresh executed harness CLI")
	}
}
