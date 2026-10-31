package sub

import (
	"strings"
	"testing"
)

func TestRouterDefaults(t *testing.T) {
	registry["constrained"] = Harness{Name: "constrained", EffortValues: []string{"low", "high"}}
	t.Cleanup(func() { delete(registry, "constrained") })
	r, err := ParseRouter("router.json", []byte(`{"defaults":{"codex":{"model":"base","effort":"medium"},"kimi":{}},"aliases":{"partial":{"harness":"codex","model":"alias"}},"fallback":{"codex":["@partial","kimi"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]Route{"codex": {Harness: "codex", Model: "base", Effort: "medium"}, "@partial": {Harness: "codex", Model: "alias", Effort: "medium"}, "kimi": {Harness: "kimi"}} {
		got, err := r.Resolve(name)
		if err != nil || got != want {
			t.Errorf("%s: %+v %v", name, got, err)
		}
	}
	first, _ := r.Resolve("codex")
	chain := r.Chain(first)
	if len(chain) != 2 || chain[1].Model != "alias" {
		t.Fatal(chain)
	}
	for _, tc := range []struct{ config, field string }{
		{`{"defaults":{"unknown":{}}}`, "defaults.unknown"},
		{`{"defaults":{"constrained":{"effort":"bogus"}}}`, "defaults.constrained.effort"},
		{`{"defaults":{"codex":{"harness":"kimi"}}}`, "defaults.codex"},
	} {
		_, err := ParseRouter("router.json", []byte(tc.config))
		if err == nil || !strings.Contains(err.Error(), tc.field) {
			t.Errorf("%s: %v", tc.config, err)
		}
	}
}

// Codex 0.162.1's vendored ReasoningEffort schema is a nonempty string,
// advertised by the selected model, rather than a closed enum.
func TestCodexDefaultsRetainAdvertisedEffort(t *testing.T) {
	if _, err := ParseRouter("router.json", []byte(`{"defaults":{"codex":{"effort":"model-specific-level"}}}`)); err != nil {
		t.Fatal(err)
	}
}

func TestRouterTieredModelDoesNotInheritEffort(t *testing.T) {
	r, err := ParseRouter("router.json", []byte(`{"defaults":{"agy":{"model":"default-model","effort":"high"}},"aliases":{"tier":{"harness":"agy","model":"gemini-3.8-flash-low"},"effort":{"harness":"agy","model":"gemini-3.8-flash-low","effort":"low"},"no-model":{"harness":"agy"}},"fallback":{"agy":["@tier","@effort","codex"]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for name, effort := range map[string]string{"agy": "high", "@tier": "", "@effort": "low", "@no-model": "high"} {
		got, err := r.Resolve(name)
		if err != nil || got.Effort != effort {
			t.Errorf("%s: %+v, %v; want effort %q", name, got, err, effort)
		}
	}
	chain := r.Chain(Route{Harness: "agy"})
	if len(chain) != 3 || chain[1].Effort != "" || chain[2].Effort != "low" {
		t.Fatalf("fallback routes: %+v", chain)
	}
}
