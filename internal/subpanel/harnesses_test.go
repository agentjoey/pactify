package subpanel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &body) != nil || len(body.Harnesses) != 4 {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	for _, h := range body.Harnesses {
		if h.Installed || h.Version != "" || h.Models.Items == nil || len(h.Models.Items) != 0 || h.Models.Error == "" || h.Models.FetchedAt.IsZero() || h.Efforts == nil {
			t.Fatal(h)
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
}
