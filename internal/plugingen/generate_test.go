package plugingen

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func generated(t *testing.T) map[string]File {
	t.Helper()
	files, err := Generate(repoRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestOutputRoots(t *testing.T) {
	files := generated(t)
	for _, path := range []string{
		"plugins/agent-plugin/pactify/plugin.json",
		"plugins/agent-plugin/README.md",
		"plugins/agents-skills/delegate/SKILL.md",
	} {
		if _, ok := files[path]; !ok {
			t.Errorf("missing output at new root: %s", path)
		}
	}
	rootMarketplaces := map[string]bool{
		".agents/plugins/marketplace.json": true,
		".github/plugin/marketplace.json":  true,
		".cursor-plugin/marketplace.json":  true,
	}
	for path := range files {
		if !strings.HasPrefix(path, "plugins/agent-plugin/") &&
			!strings.HasPrefix(path, "plugins/agents-skills/") &&
			!strings.HasPrefix(path, "plugins/pactify/") && !rootMarketplaces[path] {
			t.Errorf("output outside plugin roots: %s", path)
		}
	}
	root := t.TempDir()
	if _, err := Sync(root, files, false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "dist")); !os.IsNotExist(err) {
		t.Errorf("generated unexpected repository-root path dist: %v", err)
	}
	for _, dir := range []string{".agents", ".github", ".cursor-plugin"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			if !rootMarketplaces[filepath.ToSlash(rel)] {
				t.Errorf("generated unexpected repository-root file %s", rel)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestExpansion(t *testing.T) {
	root := t.TempDir()
	for name, text := range map[string]string{
		"skill":  `before{{include "shared"}}after`,
		"shared": `{{if eq .Host "claude"}}C{{else}}P{{end}}{{command "delegate"}}`,
	} {
		if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct{ host, want string }{
		{"claude", "beforeC`/delegate` or `/pactify:delegate`after"},
		{"portable", "beforeP`/delegate`after"},
	} {
		r := renderer{root, context{Host: tc.host}, map[string]bool{}}
		got, err := r.render("skill")
		if err != nil || got != tc.want {
			t.Fatalf("%s: %q, %v", tc.host, got, err)
		}
		for _, name := range []string{"../outside", "missing"} {
			if _, err := r.render(name); err == nil {
				t.Fatalf("accepted %s", name)
			}
		}
	}
	if err := os.WriteFile(filepath.Join(root, "loop"), []byte(`{{include "loop"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	r := renderer{root, context{}, map[string]bool{}}
	if _, err := r.render("loop"); err == nil {
		t.Fatal("accepted recursive include")
	}
}

func TestSkillValidation(t *testing.T) {
	valid := "---\nname: delegate\ndescription: Useful skill\nlicense: FSL-1.1-MIT\ncompatibility: Requires pactify\n---\nBody\n"
	if err := ValidateSkill("delegate", valid, true); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		strings.Replace(valid, "name: delegate", "name: other", 1),
		strings.Replace(valid, "Useful skill", strings.Repeat("x", 1025), 1),
		strings.Replace(valid, "Useful skill", "", 1),
		strings.Replace(valid, "Requires pactify", strings.Repeat("x", 501), 1),
		strings.Replace(valid, "license: FSL-1.1-MIT\n", "", 1),
		"name: delegate\n",
	} {
		if err := ValidateSkill("delegate", bad, true); err == nil {
			t.Errorf("accepted invalid skill: %.80q", bad)
		}
	}
	for _, name := range []string{"-delegate", "delegate-", "delegate--foo", "Delegate", "delegate_foo", strings.Repeat("a", 65)} {
		text := strings.Replace(valid, "name: delegate", "name: "+name, 1)
		if err := ValidateSkill(name, text, true); err == nil {
			t.Errorf("accepted invalid name %q", name)
		}
	}
	for _, term := range forbidden {
		if err := ValidateSkill("delegate", valid+term, true); err == nil {
			t.Errorf("accepted %s", term)
		}
	}
}

func sharedBlock(t *testing.T, text string) string {
	t.Helper()
	_, text, ok := strings.Cut(text, "<!-- BEGIN SHARED-DELEGATION -->")
	if !ok {
		t.Fatal("missing shared start")
	}
	text, _, ok = strings.Cut(text, "<!-- END SHARED-DELEGATION -->")
	if !ok {
		t.Fatal("missing shared end")
	}
	return text
}

func TestDistributions(t *testing.T) {
	files := generated(t)
	counts := map[string]int{}
	shared := map[string]string{}
	checker := files["plugins/pactify/bin/pactify-check"].Data
	for path, f := range files {
		if strings.HasPrefix(path, portableRoot+"/pactify/") {
			rel := strings.TrimPrefix(path, portableRoot+"/pactify/")
			for _, forbiddenDir := range []string{"hooks/", "agents/", "bin/", ".claude-plugin/"} {
				if strings.HasPrefix(rel, forbiddenDir) {
					t.Errorf("forbidden portable file %s", path)
				}
			}
			for _, term := range []string{"CLAUDE_PLUGIN_ROOT", "run_in_background", "7200000", "Claude Code", "subagent"} {
				if strings.Contains(string(f.Data), term) {
					t.Errorf("%s contains %s", path, term)
				}
			}
		}
		if !strings.HasSuffix(path, "/SKILL.md") {
			continue
		}
		folder := filepath.Base(filepath.Dir(path))
		host := "claude"
		if strings.HasPrefix(path, portableRoot+"/") || strings.HasPrefix(path, plainRoot+"/") {
			host = "portable"
		}
		counts[host]++
		if err := ValidateSkill(folder, string(f.Data), host == "portable"); err != nil {
			t.Error(err)
		}
		if strings.HasPrefix(folder, "delegate") {
			block := sharedBlock(t, string(f.Data))
			if prev, ok := shared[host]; ok && prev != block {
				t.Errorf("%s shared block differs", path)
			}
			shared[host] = block
		}
		if host == "portable" {
			plain := files[plainRoot+"/"+folder+"/SKILL.md"]
			if !bytes.Equal(f.Data, plain.Data) {
				t.Errorf("plain copy differs: %s", folder)
			}
			script := strings.TrimSuffix(path, "SKILL.md") + "scripts/pactify-check"
			if !bytes.Equal(files[script].Data, checker) {
				t.Errorf("checker differs: %s", script)
			}
			if !strings.Contains(string(f.Data), "sh scripts/pactify-check") {
				t.Errorf("no relative check: %s", path)
			}
		}
	}
	if counts["claude"] != 6 || counts["portable"] != 12 {
		t.Fatalf("wrong skills: %v", counts)
	}
	portable := shared["portable"]
	for _, term := range []string{"--timeout 540", "exit 3", "complete diff", "ls-files --others", "full test suite", "independent reviewer", "sub apply", "sub discard", "--resume", "--worktree-from"} {
		if !strings.Contains(portable, term) {
			t.Errorf("missing portable review/polling rule %q", term)
		}
	}
	var manifest map[string]any
	if err := json.Unmarshal(files[portableRoot+"/pactify/plugin.json"].Data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest["$schema"] != "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json" {
		t.Errorf("wrong schema %v", manifest["$schema"])
	}
	allowed := map[string]bool{"$schema": true, "name": true, "version": true, "description": true, "author": true, "homepage": true, "repository": true, "license": true, "keywords": true, "extensions": true}
	for k := range manifest {
		if !allowed[k] {
			t.Errorf("unknown manifest field %s", k)
		}
	}
	var cfg config
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "plugins/src/config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatal(err)
	}
	if manifest["version"] != cfg.Version || manifest["license"] != cfg.License {
		t.Fatal("manifest metadata differs from source")
	}
	for _, path := range []string{"plugins/pactify/.claude-plugin/plugin.json", portableRoot + "/pactify/.cursor-plugin/plugin.json"} {
		var m map[string]any
		if err := json.Unmarshal(files[path].Data, &m); err != nil {
			t.Fatal(err)
		}
		if m["version"] != cfg.Version {
			t.Errorf("version differs: %s", path)
		}
	}
	// Git hosts treat the repository root as the marketplace root, even with a
	// sparse checkout, so the root copies point into the bundle.
	for prefix, plugin := range map[string]string{portableRoot + "/": "./pactify", "": "./" + portableRoot + "/pactify"} {
		for _, host := range []string{".agents/plugins", ".github/plugin", ".cursor-plugin"} {
			var m struct {
				Plugins []struct{ Source json.RawMessage }
			}
			path := prefix + host + "/marketplace.json"
			if err := json.Unmarshal(files[path].Data, &m); err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if len(m.Plugins) != 1 {
				t.Fatalf("wrong marketplace: %s", path)
			}
			if host == ".agents/plugins" {
				var source map[string]string
				if err := json.Unmarshal(m.Plugins[0].Source, &source); err != nil {
					t.Fatal(err)
				}
				if source["source"] != "local" || source["path"] != plugin {
					t.Fatalf("%s: %v", path, source)
				}
			} else if string(m.Plugins[0].Source) != `"`+plugin+`"` {
				t.Errorf("wrong marketplace path in %s: %s", path, m.Plugins[0].Source)
			}
		}
	}
}

func TestZipDeterminismAndLayout(t *testing.T) {
	files := generated(t)
	first, err := Agensi(files)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Agensi(files)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("zip is not deterministic")
	}
	r, err := zip.NewReader(bytes.NewReader(first), int64(len(first)))
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, f := range r.File {
		names = append(names, f.Name)
		if f.Mode().Perm() != 0644 || !f.Mode().IsRegular() {
			t.Errorf("invalid zip mode: %s %v", f.Name, f.Mode())
		}
		if !f.Modified.Equal(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)) {
			t.Errorf("invalid timestamp: %v", f.Modified)
		}
		in, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(in)
		in.Close()
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte{0}) {
			t.Errorf("binary entry %s", f.Name)
		}
		if f.Name == "pactify-delegate/SKILL.md" {
			if err := ValidateSkill("pactify-delegate", string(data), true); err != nil {
				t.Fatal(err)
			}
			for _, term := range []string{"command -v pactify", "pactify version", "Never run these commands or any installer yourself", "curl -fsSL", "go install", "sub apply", "sub discard", "--timeout 540"} {
				if !strings.Contains(string(data), term) {
					t.Errorf("zip guide missing %s", term)
				}
			}
		}
	}
	if !reflect.DeepEqual(names, []string{"pactify-delegate/SKILL.md", "pactify-delegate/scripts/pactify-check"}) {
		t.Fatal(names)
	}
}

func TestSyncCheckWriteAndObsoleteFiles(t *testing.T) {
	root := t.TempDir()
	files := map[string]File{"plugins/agents-skills/delegate/SKILL.md": {[]byte("expected"), 0644}}
	stale, err := Sync(root, files, true)
	if err != nil || len(stale) != 1 {
		t.Fatalf("%v %v", stale, err)
	}
	if _, err := os.Stat(filepath.Join(root, "plugins")); !os.IsNotExist(err) {
		t.Fatal("check wrote files")
	}
	if _, err := Sync(root, files, false); err != nil {
		t.Fatal(err)
	}
	stale, err = Sync(root, files, true)
	if err != nil || len(stale) != 0 {
		t.Fatalf("%v %v", stale, err)
	}
	path := filepath.Join(root, "plugins/agents-skills/delegate/obsolete")
	if err := os.WriteFile(path, []byte("old"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "plugins/agents-skills/delegate/SKILL.md"), []byte("edited"), 0644); err != nil {
		t.Fatal(err)
	}
	stale, err = Sync(root, files, true)
	if err != nil || len(stale) != 2 || stale[0] > stale[1] {
		t.Fatalf("%v %v", stale, err)
	}
	if _, err := Sync(root, files, false); err != nil {
		t.Fatal(err)
	}
	stale, err = Sync(root, files, true)
	if err != nil || len(stale) != 0 {
		t.Fatalf("%v %v", stale, err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("obsolete file retained")
	}
}
