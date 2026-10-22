// Package plugingen generates plugin distributions from plugins/src.
// It is used only by the development command, never by the pactify CLI.
package plugingen

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"
	"unicode/utf8"
)

const Schema = "https://agent-plugins.org/schemas/1.0.0/plugin.schema.json"
const portableRoot = "plugins/agent-plugin"
const plainRoot = "plugins/agents-skills"

type config struct {
	Version           string
	MinCLI            string
	License           string
	Description       string
	ClaudeDescription string
	Name              string
	Author            string
	Repository        string
}

type context struct {
	config
	Host              string
	InstallCommands   string
	InstallCommandsSH string
}

type File struct {
	Data []byte
	Mode fs.FileMode
}

type renderer struct {
	root    string
	context context
	active  map[string]bool
}

func (r *renderer) render(name string) (string, error) {
	if !fs.ValidPath(name) || r.active[name] {
		return "", fmt.Errorf("invalid or recursive include: %s", name)
	}
	r.active[name] = true
	defer delete(r.active, name)
	data, err := os.ReadFile(filepath.Join(r.root, filepath.FromSlash(name)))
	if err != nil {
		return "", err
	}
	t, err := template.New(name).Option("missingkey=error").Funcs(template.FuncMap{
		"include": r.render,
		"command": func(name string) string {
			if r.context.Host == "claude" {
				return "`/" + name + "` or `/pactify:" + name + "`"
			}
			return "`/" + name + "`"
		},
	}).Parse(string(data))
	if err != nil {
		return "", err
	}
	var out bytes.Buffer
	if err := t.Execute(&out, r.context); err != nil {
		return "", err
	}
	return out.String(), nil
}

// Generate builds every committed output in memory before anything is written.
func Generate(root string) (map[string]File, error) {
	source := filepath.Join(root, "plugins/src")
	raw, err := os.ReadFile(filepath.Join(source, "config.json"))
	if err != nil {
		return nil, err
	}
	var cfg config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil, err
	}
	if cfg.Name == "" || cfg.Version == "" || cfg.MinCLI == "" || cfg.License == "" || cfg.Author == "" || cfg.Repository == "" {
		return nil, fmt.Errorf("incomplete plugins/src/config.json")
	}
	commands, err := os.ReadFile(filepath.Join(source, "install-commands.txt"))
	if err != nil {
		return nil, err
	}
	c := context{config: cfg, InstallCommands: string(commands)}
	var shellLines []string
	for _, line := range strings.Split(strings.TrimSpace(string(commands)), "\n") {
		shellLines = append(shellLines, "    '  "+strings.ReplaceAll(line, "'", "'\\''")+"'")
	}
	c.InstallCommandsSH = strings.Join(shellLines, " \\\n")
	files := map[string]File{}
	add := func(name, text string, mode fs.FileMode) { files[name] = File{[]byte(text), mode} }
	addJSON := func(name string, value any) error {
		data, err := json.MarshalIndent(value, "", "  ")
		if err != nil {
			return err
		}
		add(name, string(data)+"\n", 0644)
		return nil
	}
	skills, err := filepath.Glob(filepath.Join(source, "skills/*.md.tmpl"))
	if err != nil {
		return nil, err
	}
	if len(skills) == 0 {
		return nil, fmt.Errorf("no skill sources")
	}
	for _, host := range []string{"claude", "portable"} {
		c.Host = host
		r := renderer{source, c, map[string]bool{}}
		checker, err := r.render("pactify-check.sh.tmpl")
		if err != nil {
			return nil, err
		}
		if host == "claude" {
			add("plugins/pactify/bin/pactify-check", checker, 0755)
		}
		for _, path := range skills {
			name := strings.TrimSuffix(filepath.Base(path), ".md.tmpl")
			text, err := r.render("skills/" + name + ".md.tmpl")
			if err != nil {
				return nil, err
			}
			if err := ValidateSkill(name, text, host == "portable"); err != nil {
				return nil, err
			}
			if host == "claude" {
				add("plugins/pactify/skills/"+name+"/SKILL.md", text, 0644)
			} else {
				for _, base := range []string{portableRoot + "/pactify/skills/", plainRoot + "/"} {
					add(base+name+"/SKILL.md", text, 0644)
					add(base+name+"/scripts/pactify-check", checker, 0755)
				}
			}
		}
	}
	manifest := map[string]any{
		"name": cfg.Name, "version": cfg.Version, "description": cfg.ClaudeDescription,
		"author": map[string]string{"name": cfg.Author}, "homepage": cfg.Repository,
		"repository": cfg.Repository, "license": cfg.License,
	}
	if err := addJSON("plugins/pactify/.claude-plugin/plugin.json", manifest); err != nil {
		return nil, err
	}
	manifest["$schema"] = Schema
	manifest["description"] = cfg.Description
	if err := addJSON(portableRoot+"/pactify/plugin.json", manifest); err != nil {
		return nil, err
	}
	cursor := map[string]any{"name": cfg.Name, "version": cfg.Version, "description": cfg.Description, "author": manifest["author"]}
	if err := addJSON(portableRoot+"/pactify/.cursor-plugin/plugin.json", cursor); err != nil {
		return nil, err
	}
	// The bundle is a marketplace root of its own; Git hosts use the repository
	// root even with a sparse checkout, so the root copies point into the bundle.
	for prefix, plugin := range map[string]string{portableRoot + "/": "./pactify", "": "./" + portableRoot + "/pactify"} {
		marketplace := map[string]any{"name": cfg.Name, "owner": manifest["author"], "plugins": []any{
			map[string]any{"name": cfg.Name, "source": plugin, "description": cfg.Description},
		}}
		for _, host := range []string{".github/plugin", ".cursor-plugin"} {
			if err := addJSON(prefix+host+"/marketplace.json", marketplace); err != nil {
				return nil, err
			}
		}
		codex := map[string]any{"name": cfg.Name, "interface": map[string]string{"displayName": "Pactify"}, "plugins": []any{
			map[string]any{"name": cfg.Name, "source": map[string]string{"source": "local", "path": plugin},
				"policy": map[string]string{"installation": "AVAILABLE", "authentication": "ON_INSTALL"}, "category": "Productivity"},
		}}
		if err := addJSON(prefix+".agents/plugins/marketplace.json", codex); err != nil {
			return nil, err
		}
	}
	r := renderer{source, c, map[string]bool{}}
	readme, err := r.render("portable-README.md.tmpl")
	if err != nil {
		return nil, err
	}
	add(portableRoot+"/README.md", readme, 0644)
	return files, nil
}

var skillName = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
var forbidden = []string{"CLAUDE_PLUGIN_ROOT", "run_in_background", "7200000", "Claude", "subagent", "main thread", "main-thread", "/pactify:", "disable-model-invocation"}

// ValidateSkill checks the constrained frontmatter emitted by our templates.
func ValidateSkill(folder, text string, portable bool) error {
	parts := strings.SplitN(text, "---\n", 3)
	if len(parts) != 3 || parts[0] != "" {
		return fmt.Errorf("%s: missing frontmatter", folder)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(parts[1]), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || fields[key] != "" {
			return fmt.Errorf("%s: invalid frontmatter line %q", folder, line)
		}
		fields[key] = strings.Trim(value, "'\"")
	}
	if fields["name"] != folder || len(folder) > 64 || !skillName.MatchString(folder) {
		return fmt.Errorf("%s: invalid name or folder mismatch", folder)
	}
	if fields["description"] == "" || utf8.RuneCountInString(fields["description"]) > 1024 {
		return fmt.Errorf("%s: invalid description", folder)
	}
	if portable {
		if fields["license"] == "" || fields["compatibility"] == "" || utf8.RuneCountInString(fields["compatibility"]) > 500 {
			return fmt.Errorf("%s: invalid portable metadata", folder)
		}
		for _, s := range forbidden {
			if strings.Contains(text, s) {
				return fmt.Errorf("%s: forbidden portable text %q", folder, s)
			}
		}
	}
	return nil
}

// Agensi returns a deterministic text-only archive of the portable delegate skill.
func Agensi(files map[string]File) ([]byte, error) {
	var out bytes.Buffer
	w := zip.NewWriter(&out)
	base := plainRoot + "/delegate/"
	var names []string
	for name := range files {
		if strings.HasPrefix(name, base) {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return nil, fmt.Errorf("missing portable delegate skill")
	}
	for _, name := range names {
		rel := strings.TrimPrefix(name, base)
		data := files[name].Data
		if rel == "SKILL.md" {
			data = bytes.Replace(data, []byte("name: delegate\n"), []byte("name: pactify-delegate\n"), 1)
			if err := ValidateSkill("pactify-delegate", string(data), true); err != nil {
				return nil, err
			}
		}
		if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
			return nil, fmt.Errorf("non-text zip entry: %s", name)
		}
		h := &zip.FileHeader{Name: "pactify-delegate/" + rel, Method: zip.Deflate}
		h.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		h.SetMode(0644)
		entry, err := w.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write(data); err != nil {
			return nil, err
		}
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// Sync writes generated files or returns sorted paths that need regeneration.
// Only the fully generated skill/distribution trees are pruned of obsolete files.
func Sync(root string, files map[string]File, check bool) ([]string, error) {
	stale := map[string]bool{}
	for _, base := range []string{portableRoot, plainRoot, "plugins/pactify/skills"} {
		err := filepath.WalkDir(filepath.Join(root, filepath.FromSlash(base)), func(path string, d fs.DirEntry, err error) error {
			if os.IsNotExist(err) {
				return nil
			}
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			if _, ok := files[rel]; !ok {
				stale[rel] = true
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	for name, want := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		got, err := os.ReadFile(path)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		info, statErr := os.Lstat(path)
		if statErr != nil && !os.IsNotExist(statErr) {
			return nil, statErr
		}
		if err == nil && info.Mode().IsRegular() && bytes.Equal(got, want.Data) && info.Mode().Perm() == want.Mode {
			continue
		}
		stale[name] = true
	}
	names := make([]string, 0, len(stale))
	for name := range stale {
		names = append(names, name)
	}
	sort.Strings(names)
	if check {
		return names, nil
	}
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		want, exists := files[name]
		if !exists {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return nil, err
		}
		// Remove an existing symlink instead of writing through it.
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if err := os.Remove(path); err != nil {
				return nil, err
			}
		}
		if err := os.WriteFile(path, want.Data, want.Mode); err != nil {
			return nil, err
		}
		if err := os.Chmod(path, want.Mode); err != nil {
			return nil, err
		}
	}
	return names, nil
}
