package pact

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var slugRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// IsSlug reports whether s matches the protocol slug pattern used for seat and
// task ids: a lowercase alphanumeric start followed by lowercase alphanumerics
// or dashes. Exported so callers (e.g. the serve author API) validate ids with
// the same pattern instead of duplicating the regex.
func IsSlug(s string) bool { return slugRe.MatchString(s) }

type Seat struct {
	ID    string
	Roles []string
	Entry string
	Kind  string // optional: agent kind for MCP wiring ("" = shell/legacy)
}

// ParseSeat parses "id:role1,role2:entry[:kind]" with validation (3 or 4
// non-empty fields, slug id, entry is a repo-relative path without "..").
func ParseSeat(s string) (Seat, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 3 && len(parts) != 4 {
		return Seat{}, fmt.Errorf("seat must be 'id:roles:entry[:kind]' (3 or 4 fields): %q", s)
	}
	id, roles, entry := parts[0], parts[1], parts[2]
	kind := ""
	if len(parts) == 4 {
		kind = parts[3]
	}
	if id == "" || roles == "" || entry == "" {
		return Seat{}, fmt.Errorf("seat fields must be non-empty: %q", s)
	}
	if !slugRe.MatchString(id) {
		return Seat{}, fmt.Errorf("seat id %q is not a slug", id)
	}
	if strings.HasPrefix(entry, "/") || strings.Contains(entry, "..") {
		return Seat{}, fmt.Errorf("seat entry %q must be a repo-relative path without '..'", entry)
	}
	return Seat{ID: id, Roles: strings.Split(roles, ","), Entry: entry, Kind: kind}, nil
}

const blockBegin = "<!-- pact:begin (managed by pactify — edit outside this block) -->"
const blockEnd = "<!-- pact:end -->"

// stripBlock removes an existing pact:begin..pact:end block (inclusive) and
// trailing blank lines, returning the preserved content.
func stripBlock(content string) string {
	lines := strings.Split(content, "\n")
	var out []string
	inBlock := false
	for _, ln := range lines {
		if strings.Contains(ln, "pact:begin") {
			inBlock = true
		}
		if !inBlock {
			out = append(out, ln)
		}
		if strings.Contains(ln, blockEnd) {
			inBlock = false
		}
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return strings.Join(out, "\n")
}

// bakeBlock writes `block` into `path` inside the managed markers, preserving
// any pre-existing content and never following a symlink. Byte-idempotent.
func bakeBlock(path, block string) error {
	if fi, err := os.Lstat(path); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		_ = os.Remove(path)
	}
	existing := ""
	if b, err := os.ReadFile(path); err == nil {
		existing = stripBlock(string(b))
	}
	var out string
	if strings.TrimSpace(existing) != "" {
		out = existing + "\n\n" + block + "\n"
	} else {
		out = block + "\n"
	}
	return os.WriteFile(path, []byte(out), 0o644)
}

// BakeManagedBlock writes body inside the managed markers at path, preserving
// any surrounding content. Idempotent and symlink-safe (see bakeBlock). body
// must not itself contain the managed markers, or a later stripBlock pass would
// mis-detect the region boundary and corrupt it.
func BakeManagedBlock(path, body string) error {
	if strings.Contains(body, "pact:begin") || strings.Contains(body, "pact:end") {
		return fmt.Errorf("managed-block body must not contain the pact:begin/pact:end markers")
	}
	return bakeBlock(path, blockBegin+"\n"+body+"\n"+blockEnd)
}

// BakeEntry writes a seat's vendor entry file (managed block).
func BakeEntry(dir string, s Seat) error {
	roles := strings.Join(s.Roles, ",")
	block := blockBegin + "\n" +
		"# pact protocol — seat `" + s.ID + "`\n\n" +
		"> On session start, run this. If your shell does NOT persist state between commands,\n" +
		"> prefix every pact command with the export + source.\n\n" +
		"```bash\n" +
		"export PACT_AGENT_ID=" + s.ID + "\n" +
		"pactify join " + s.ID + " --roles " + roles + "\n" +
		"```\n\n" +
		"Then read `.pact/PROJECT.md` and `.pact/STATE.yml`. Run `pactify help` for the verbs.\n" +
		blockEnd
	return bakeBlock(filepath.Join(dir, s.Entry), block)
}

// BakeProject writes .pact/PROJECT.md charter (managed block) with the seat table.
func BakeProject(pactDir, project string, seats []Seat, protocolVersion int) error {
	var sb strings.Builder
	for _, s := range seats {
		fmt.Fprintf(&sb, "- `%s` — roles: %s — entry: %s\n", s.ID, strings.Join(s.Roles, ", "), s.Entry)
	}
	block := blockBegin + "\n" +
		fmt.Sprintf("# %s — Pact Charter (protocol_version: %d)\n\n", project, protocolVersion) +
		fmt.Sprintf("This repo uses the **pact protocol** (v%d). Any agent that can read files + run git can participate.\n\n", protocolVersion) +
		"## Roles\n" +
		"- **orchestrator** — split spec→tasks; assign; merge; maintain charter\n" +
		"- **worker** — implement; at checkpoint set awaiting_review + write evidence\n" +
		"- **reviewer** — verify diff+evidence → accept / changes_requested\n" +
		"- **human** — start button + final authority\n\n" +
		"## The two rules (the pact)\n" +
		"1. A worker cannot self-accept. Only a task's reviewer may accept it (owner != reviewer), and only when awaiting_review.\n" +
		"2. A feature cannot merge until all its tasks are accepted.\n\n" +
		"## Seats\n" + strings.TrimRight(sb.String(), "\n") + "\n\n" +
		"## Commands\nRun `pactify help` for the verb reference.\n" +
		blockEnd
	return bakeBlock(filepath.Join(pactDir, "PROJECT.md"), block)
}
