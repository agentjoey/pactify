package sub

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/agentjoey/pactify/internal/cockpit"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestKimiModelDiscoveryAllowlist(t *testing.T) {
	source := t.TempDir()
	home := t.TempDir()
	// The missing-config case below must not fall back to the developer's own
	// ~/.kimi-code/config.toml.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("KIMI_CODE_HOME", t.TempDir())
	config := filepath.Join(source, "config.toml")
	data := `default_model = "provider/model-b"
[models."provider/model-b"]
model = "backend-secret-ignored"
api_key = "MODEL_KEY_SENTINEL"
[models.'provider/model-a']
[providers.secret]
api_key = "PROVIDER_KEY_SENTINEL"
base_url = "URL_SENTINEL"
[oauth]
access_token = "OAUTH_SENTINEL"
`
	if err := os.WriteFile(config, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(config, filepath.Join(home, "config.toml")); err != nil {
		t.Fatal(err)
	}
	// No credentials/device_id files exist. Discovery must not require them.
	opts := cockpit.StartOpts{Env: []string{"KIMI_CODE_HOME=" + home}}
	got, err := kimiModelDetails(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(got.Items, []string{"provider/model-a", "provider/model-b"}) || got.DefaultModel != "provider/model-b" {
		t.Fatalf("%+v %v", got, err)
	}
	encoded, _ := json.Marshal(got)
	for _, secret := range []string{"SENTINEL", "backend-secret", "api_key", "providers", "oauth"} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal(string(encoded))
		}
	}
	h, _ := Lookup("kimi")
	if h.Models == nil {
		t.Fatal("kimi Models is not registered")
	}
	items, err := h.Models(context.Background(), opts)
	if err != nil || !reflect.DeepEqual(items, got.Items) {
		t.Fatal(items, err)
	}
	for _, invalid := range []string{`[providers.secret]
api_key = "PARSE_ERROR_SECRET`, `default_model="only"`, `models="TYPE_ERROR_SECRET"`} {
		if err := os.WriteFile(config, []byte(invalid), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := kimiModelDetails(context.Background(), opts)
		if err == nil || !strings.Contains(err.Error(), "kimi:") || strings.Contains(err.Error(), "SECRET") {
			t.Fatal(err)
		}
	}
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if _, err := kimiModelDetails(context.Background(), opts); err == nil {
		t.Fatal("missing config accepted")
	}
}

func TestKimiDiscoveryFreshHomeFallsBackWithoutPreparing(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(fmt.Sprint(explicit), func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("PACTIFY_HOME", home)
			user := t.TempDir()
			t.Setenv("HOME", user)
			t.Setenv("KIMI_CODE_HOME", "")
			configHome := filepath.Join(user, ".kimi-code")
			if explicit {
				configHome = t.TempDir()
				t.Setenv("KIMI_CODE_HOME", configHome)
			}
			if err := os.MkdirAll(configHome, 0700); err != nil {
				t.Fatal(err)
			}
			text := `default_model="provider/local"
[models."provider/local"]
[providers.secret]
api_key="DO_NOT_EXPOSE_SECRET"
[oauth]
access_token="DO_NOT_EXPOSE_SECRET"
`
			if err := os.WriteFile(filepath.Join(configHome, "config.toml"), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			h, _ := Lookup("kimi")
			got, err := discoverHarnessModels(t.Context(), h)
			if err != nil || !reflect.DeepEqual(got.Items, []string{"provider/local"}) || got.DefaultModel != "provider/local" {
				t.Fatal(got, err)
			}
			if _, err := os.Stat(filepath.Join(home, "harness-homes")); !os.IsNotExist(err) {
				t.Fatal("discovery created managed directories", err)
			}
			data, _ := json.Marshal(got)
			if strings.Contains(string(data), "SECRET") {
				t.Fatal("sensitive fields leaked")
			}
		})
	}
}
