package orchestrate

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// knowledgeBudget caps the total bytes of the injected "项目知识" section so a
// large memory.md or many skills can never blow up a briefing (spec §3, §7).
const knowledgeBudget = 4096

// KnowledgeFor builds the git-native knowledge section appended to a briefing:
// the full text of `.pact/memory.md` (if present) first, then the body of every
// matching `.pact/skills/*.md`. A skill matches when its frontmatter `roles`
// includes role (empty roles = all roles) AND one of its `keywords` is a
// case-insensitive substring of taskHint (empty keywords = always). The whole
// section is truncated to knowledgeBudget bytes with a `…(truncated)` marker.
//
// It is defensive by contract: missing files, an unreadable skills dir, or a
// malformed skill frontmatter are skipped silently and never error. When
// nothing applies it returns "" so callers can leave the briefing untouched.
func KnowledgeFor(dir, role, taskHint string) string {
	pactDir := filepath.Join(dir, ".pact")

	var parts []string
	if mem := strings.TrimRight(readFileOrEmpty(filepath.Join(pactDir, "memory.md")), "\n"); mem != "" {
		parts = append(parts, mem)
	}
	parts = append(parts, matchingSkills(filepath.Join(pactDir, "skills"), role, taskHint)...)

	if len(parts) == 0 {
		return ""
	}

	section := "## 项目知识\n" + strings.Join(parts, "\n\n") + "\n"
	return truncateKnowledge(section, knowledgeBudget)
}

// matchingSkills reads skillsDir/*.md and returns the bodies of skills whose
// frontmatter matches role + taskHint, in sorted filename order for stable
// output. Files with malformed or missing frontmatter are skipped.
func matchingSkills(skillsDir, role, taskHint string) []string {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		return nil // no skills dir = nothing to inject
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	hint := strings.ToLower(taskHint)
	var out []string
	for _, name := range names {
		raw := readFileOrEmpty(filepath.Join(skillsDir, name))
		roles, keywords, body, ok := parseSkill(raw)
		if !ok {
			continue // malformed frontmatter → skip, never error
		}
		if !roleMatches(roles, role) || !keywordMatches(keywords, hint) {
			continue
		}
		if body = strings.TrimSpace(body); body != "" {
			out = append(out, body)
		}
	}
	return out
}

// parseSkill splits a hand-written frontmatter skill file into its roles and
// keywords lists plus the body. Frontmatter is a leading `---` line, some
// `key: [a, b]` lines, and a closing `---` line. Only `roles` and `keywords`
// are recognised; unknown keys are ignored. ok is false when the file has no
// well-formed frontmatter block (skip the file, don't error).
func parseSkill(raw string) (roles, keywords []string, body string, ok bool) {
	// Normalise line endings and require a leading `---` fence.
	s := strings.ReplaceAll(raw, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, nil, "", false
	}
	closeIdx := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			closeIdx = i
			break
		}
	}
	if closeIdx == -1 {
		return nil, nil, "", false // no closing fence = malformed
	}
	for _, line := range lines[1:closeIdx] {
		key, val, found := strings.Cut(line, ":")
		if !found {
			continue
		}
		switch strings.TrimSpace(key) {
		case "roles":
			roles = parseList(val)
		case "keywords":
			keywords = parseList(val)
		}
	}
	body = strings.Join(lines[closeIdx+1:], "\n")
	return roles, keywords, body, true
}

// parseList parses a `[a, b, c]` inline list (brackets optional) into a slice
// of trimmed, non-empty items.
func parseList(v string) []string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "[")
	v = strings.TrimSuffix(v, "]")
	var out []string
	for _, item := range strings.Split(v, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// roleMatches reports whether a skill declaring roles applies to role. An empty
// roles list means "all roles".
func roleMatches(roles []string, role string) bool {
	if len(roles) == 0 {
		return true
	}
	for _, r := range roles {
		if strings.EqualFold(r, role) {
			return true
		}
	}
	return false
}

// keywordMatches reports whether any keyword is a case-insensitive substring of
// the (already lower-cased) hint. An empty keywords list means "always".
func keywordMatches(keywords []string, lowerHint string) bool {
	if len(keywords) == 0 {
		return true
	}
	for _, k := range keywords {
		if strings.Contains(lowerHint, strings.ToLower(strings.TrimSpace(k))) {
			return true
		}
	}
	return false
}

// truncateKnowledge caps s to n bytes, replacing the tail with a marker when it
// overflows so the reader knows the section was cut.
func truncateKnowledge(s string, n int) string {
	if len(s) <= n {
		return s
	}
	const marker = "…(truncated)"
	cut := n - len(marker)
	if cut < 0 {
		cut = 0
	}
	return s[:cut] + marker
}

// readFileOrEmpty returns the file's contents, or "" if it cannot be read.
func readFileOrEmpty(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
