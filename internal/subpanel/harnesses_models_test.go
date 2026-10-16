package subpanel

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/agentjoey/pactify/internal/sub"
)

func TestHarnessesCatalogProcessHelper(t *testing.T) {
	if os.Getenv("PANEL_CATALOG_HELPER") != "1" {
		return
	}
	log, err := os.OpenFile(os.Getenv("PANEL_CATALOG_CALLS"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		os.Exit(90)
	}
	defer log.Close()
	decoder := json.NewDecoder(os.Stdin)
	for {
		var m struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if decoder.Decode(&m) != nil {
			os.Exit(0)
		}
		fmt.Fprintln(log, m.Method)
		result := any(map[string]any{})
		switch m.Method {
		case "initialize":
		case "initialized":
			continue
		case "model/list":
			if os.Getenv("PANEL_CATALOG_FAIL") == "1" {
				_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": m.ID, "error": map[string]any{"code": -1, "message": "catalog offline"}})
				continue
			}
			result = map[string]any{"data": []any{map[string]any{"model": "gpt-catalog", "supportedReasoningEfforts": []any{map[string]string{"reasoningEffort": "low"}, map[string]string{"reasoningEffort": "high"}}, "providers": map[string]string{"key": "RPC_SECRET_SENTINEL"}}}}
		default:
			os.Exit(91)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"id": m.ID, "result": result})
	}
}

func TestHarnessesCodexKimiModelsAndCache(t *testing.T) {
	home := t.TempDir()
	t.Setenv("PACTIFY_HOME", home)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	t.Setenv("PACTIFY_SUB_FAKE", "")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bins := t.TempDir()
	calls := filepath.Join(t.TempDir(), "calls")
	t.Setenv("PATH", bins)
	t.Setenv("PANEL_CATALOG_HELPER", "1")
	t.Setenv("PANEL_CATALOG_CALLS", calls)
	script := "#!/bin/sh\nif [ \"$1\" = --version ]; then echo 'codex-cli 0.162.1'; exit 0; fi\nexec '" + strings.ReplaceAll(exe, "'", "'\"'\"'") + "' -test.run='^TestHarnessesCatalogProcessHelper$' -- \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bins, "codex"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bins, "kimi"), []byte("#!/bin/sh\n[ \"$1\" = --version ] || exit 92\necho 'kimi 1.0'\n"), 0700); err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(t.TempDir(), "config.toml")
	managed := filepath.Join(home, "harness-homes", "kimi")
	if err := os.MkdirAll(managed, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, filepath.Join(managed, "config.toml")); err != nil {
		t.Fatal(err)
	}
	config := func(model string) {
		t.Helper()
		data := "default_model = \"" + model + "\"\n[models.\"" + model + "\"]\n[providers.secret]\napi_key=\"CONFIG_SECRET_SENTINEL\"\n[oauth]\naccess_token=\"OAUTH_SECRET_SENTINEL\"\n"
		if err := os.WriteFile(source, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	config("provider/first")
	store, _ := sub.DefaultStore()
	server, err := New(store)
	if err != nil {
		t.Fatal(err)
	}
	get := func(query string) map[string]sub.HarnessInfo {
		t.Helper()
		w := request(server, "GET", "/api/harnesses"+query, server.token)
		var response struct {
			Harnesses []sub.HarnessInfo `json:"harnesses"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &response) != nil {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		for _, sensitive := range []string{"SECRET_SENTINEL", "api_key", "providers", "oauth", "access_token"} {
			if strings.Contains(w.Body.String(), sensitive) {
				t.Fatalf("sensitive field in response: %s", sensitive)
			}
		}
		rows := map[string]sub.HarnessInfo{}
		for _, h := range response.Harnesses {
			rows[h.Name] = h
		}
		return rows
	}
	rows := get("")
	codex, kimi := rows["codex"], rows["kimi"]
	if !reflect.DeepEqual(codex.Models.Items, []string{"gpt-catalog"}) || !reflect.DeepEqual(codex.Efforts, []string{"low", "high"}) || codex.Models.Error != "" || !reflect.DeepEqual(kimi.Models.Items, []string{"provider/first"}) || kimi.Default.Model != "provider/first" || kimi.Models.Error != "" {
		t.Fatal(rows)
	}
	config("provider/second")
	rows = get("")
	if rows["kimi"].Default.Model != "provider/first" {
		t.Fatal("configuration bypassed cache", rows["kimi"])
	}
	data, _ := os.ReadFile(calls)
	if string(data) != "initialize\ninitialized\nmodel/list\n" {
		t.Fatal(string(data))
	}
	rows = get("?refresh=1")
	if rows["kimi"].Default.Model != "provider/second" || !reflect.DeepEqual(rows["kimi"].Models.Items, []string{"provider/second"}) {
		t.Fatal(rows["kimi"])
	}
	t.Setenv("PANEL_CATALOG_FAIL", "1")
	if err := os.WriteFile(source, []byte("[providers.secret]\napi_key=\"PARSE_SECRET_SENTINEL"), 0600); err != nil {
		t.Fatal(err)
	}
	rows = get("?refresh=1")
	if rows["codex"].Models.Error == "" || rows["kimi"].Models.Error != "kimi: invalid config.toml" || !reflect.DeepEqual(rows["codex"].Models.Items, codex.Models.Items) || !reflect.DeepEqual(rows["codex"].Efforts, codex.Efforts) || rows["kimi"].Default.Model != "provider/second" {
		t.Fatal(rows)
	}
	get("")
	data, _ = os.ReadFile(calls)
	if strings.Count(string(data), "model/list\n") != 3 {
		t.Fatal("failure was not cached", string(data))
	}
	// Router changes apply immediately without a new native config read.
	if err := os.WriteFile(filepath.Join(home, "router.json"), []byte(`{"defaults":{"kimi":{"model":"router-override"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if got := get("")["kimi"].Default.Model; got != "router-override" {
		t.Fatal(got)
	}
}
