package serve

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRecipeList(t *testing.T) {
	srv := New(nil)
	ts := newTestServer(t, srv)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/recipes")
	if err != nil {
		t.Fatalf("GET /api/recipes: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	var items []struct {
		Name        string `json:"name"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(items) < 3 {
		t.Fatalf("want >=3 recipes, got %d", len(items))
	}

	found := false
	for _, it := range items {
		if it.Name == "add-tests" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected add-tests in recipe list")
	}
}

func TestRecipeExpand(t *testing.T) {
	srv := New(nil)
	srv.SetSeat("test")
	ts := newTestServer(t, srv)
	defer ts.Close()

	t.Run("happy path", func(t *testing.T) {
		body := `{"goal":"做个X"}`
		resp, err := http.Post(ts.URL+"/api/recipes/add-tests/expand", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("want 200, got %d", resp.StatusCode)
		}

		var tasks []struct {
			ID   string   `json:"id"`
			Spec string   `json:"spec"`
			Deps []string `json:"deps,omitempty"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if len(tasks) < 1 {
			t.Fatal("want >=1 tasks")
		}
		if !strings.Contains(tasks[0].Spec, "做个X") {
			t.Errorf("spec should contain goal, got %q", tasks[0].Spec)
		}
	})

	t.Run("empty goal -> 400", func(t *testing.T) {
		body := `{"goal":""}`
		resp, err := http.Post(ts.URL+"/api/recipes/add-tests/expand", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("want 400, got %d", resp.StatusCode)
		}
	})

	t.Run("unknown recipe -> 404", func(t *testing.T) {
		body := `{"goal":"whatever"}`
		resp, err := http.Post(ts.URL+"/api/recipes/nope/expand", "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("POST: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("want 404, got %d", resp.StatusCode)
		}
	})
}
