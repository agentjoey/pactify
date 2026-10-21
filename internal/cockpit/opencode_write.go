package cockpit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

// OpencodeWritableRoots keeps literal absolute roots and their filesystem aliases.
// Never turn a directory name containing wildcard syntax into a native allow rule.
func OpencodeWritableRoots(paths ...string) []string {
	var roots []string
	for _, path := range paths {
		if !literalOpencodePath(path) {
			continue
		}
		path = filepath.Clean(path)
		resolved, err := resolveOpencodePath(path)
		if err != nil || !literalOpencodePath(resolved) {
			continue
		}
		for _, root := range []string{path, resolved} {
			if !slices.Contains(roots, root) {
				roots = append(roots, root)
			}
		}
	}
	return roots
}

func literalOpencodePath(path string) bool {
	return filepath.IsAbs(path) && !strings.ContainsAny(path, "*?[]{}\\\x00")
}

// Resolve the longest existing prefix, retaining nonexistent suffixes for new
// files/directories. Broken links and other lookup errors fail closed.
func resolveOpencodePath(path string) (string, error) {
	prefix := path
	var suffix []string
	for {
		if _, err := os.Lstat(prefix); err == nil {
			resolved, err := filepath.EvalSymlinks(prefix)
			if err != nil {
				return "", err
			}
			for i := len(suffix) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, suffix[i])
			}
			return resolved, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		parent := filepath.Dir(prefix)
		if parent == prefix {
			return "", fmt.Errorf("no existing path prefix")
		}
		suffix = append(suffix, filepath.Base(prefix))
		prefix = parent
	}
}

func opencodePathAllowed(path string, roots []string) bool {
	if !filepath.IsAbs(path) || strings.ContainsRune(path, 0) {
		return false
	}
	resolved, err := resolveOpencodePath(filepath.Clean(path))
	if err != nil {
		return false
	}
	for _, root := range roots {
		if !filepath.IsAbs(root) || strings.ContainsRune(root, 0) {
			continue
		}
		canonical, err := resolveOpencodePath(filepath.Clean(root))
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(canonical, resolved)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return true
		}
	}
	return false
}

func opencodeDirectoryPath(pattern string) string {
	for _, suffix := range []string{"/**", "/*"} {
		if path, ok := strings.CutSuffix(pattern, suffix); ok {
			return path
		}
	}
	return pattern
}

// Native permissions are a guardrail, not an OS sandbox. Check requests again
// before handing any approval to the worker, including recovered/child requests.
func (s *opencodeSession) allowWritePermission(name string, raw json.RawMessage, v2 bool) bool {
	if s.opts.Access != AccessWrite {
		return true
	}
	warning := ""
	allow := false
	switch name {
	case "external_directory", "edit", "write", "patch", "read":
		var request struct {
			Patterns  []string `json:"patterns"`
			Resources []string `json:"resources"`
			Metadata  struct {
				Filepath string          `json:"filepath"`
				Files    json.RawMessage `json:"files"`
			} `json:"metadata"`
		}
		err := json.Unmarshal(raw, &request)
		paths := request.Patterns
		if v2 {
			paths = request.Resources
		} else if name != "external_directory" {
			files, filesErr := opencodeV1FilePaths(request.Metadata.Files)
			if filesErr != nil {
				err = filesErr
			} else if len(files) > 0 {
				// apply_patch patterns and its comma-joined filepath are relative
				// to Instance.worktree, not necessarily this job's cwd.
				paths = files
			} else if filepath.IsAbs(request.Metadata.Filepath) {
				// V1 patterns are rule keys relative to Instance.worktree, which
				// can differ from RepoDir for linked worktrees. The absolute tool
				// filepath is authoritative when present.
				paths = []string{request.Metadata.Filepath}
			} else if request.Metadata.Filepath != "" {
				paths = append(paths, request.Metadata.Filepath)
			}
		}
		roots := append([]string{}, s.opts.WritableRoots...)
		// The cwd remains a literal adapter root even when its name cannot safely
		// be represented in a native wildcard rule. Keep its filesystem alias too.
		if filepath.IsAbs(s.opts.RepoDir) {
			roots = append(roots, filepath.Clean(s.opts.RepoDir))
			if resolved, err := resolveOpencodePath(filepath.Clean(s.opts.RepoDir)); err == nil {
				roots = append(roots, resolved)
			}
		}
		allow = err == nil && len(paths) > 0
		denied := ""
		for _, path := range paths {
			if name == "external_directory" {
				path = opencodeDirectoryPath(path)
				// External-directory resources are patterns: only the two directory
				// suffixes above are accepted, even if a literal cwd contains glob syntax.
				if !literalOpencodePath(path) {
					allow = false
					denied = path
					break
				}
			} else {
				// File paths are literal, including framework names like [slug].
				// V2 may describe a directory with a trailing /* or /** resource.
				if v2 {
					path = opencodeDirectoryPath(path)
				}
				if path != "" && !filepath.IsAbs(path) && filepath.IsAbs(s.opts.RepoDir) {
					path = filepath.Join(s.opts.RepoDir, path)
				}
			}
			if !opencodePathAllowed(path, roots) {
				allow = false
				denied = path
				break
			}
		}
		if !allow {
			kind := "file"
			if name == "external_directory" {
				kind = "external directory"
			} else if name == "read" {
				kind = "read"
			}
			warning = "pactify denied " + kind + " access: " + opencodeWarningPath(denied)
		}
	case "bash", "shell", "webfetch", "websearch", "task", "todowrite":
		allow = true
	case "doom_loop", "question":
		// A noninteractive job cannot answer questions or authorize looping tools.
	default:
		warning = "pactify denied unknown permission: " + opencodeWarningPath(name)
	}
	if warning != "" {
		s.dispatch(Event{Kind: EventSystem, State: "warning", Text: warning})
	}
	return allow
}

// A present files value must be an array. An empty array preserves the legacy
// filepath/pattern fallback; every nonempty entry requires an absolute filePath.
func opencodeV1FilePaths(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var files []struct {
		FilePath string          `json:"filePath"`
		MovePath json.RawMessage `json:"movePath"`
	}
	if err := json.Unmarshal(raw, &files); err != nil {
		return nil, err
	}
	if files == nil {
		return nil, fmt.Errorf("metadata.files must be an array")
	}
	var paths []string
	for _, file := range files {
		if !filepath.IsAbs(file.FilePath) {
			return nil, fmt.Errorf("metadata.files filePath must be absolute")
		}
		paths = append(paths, file.FilePath)
		if len(file.MovePath) > 0 {
			var move string
			if err := json.Unmarshal(file.MovePath, &move); err != nil || !filepath.IsAbs(move) {
				return nil, fmt.Errorf("metadata.files movePath must be absolute")
			}
			paths = append(paths, move)
		}
	}
	return paths, nil
}

func opencodePermissions(opts StartOpts) []opencodePermission {
	if opts.Access == AccessReadOnly {
		return opencodeReadonlyPermissions()
	}
	rules := []opencodePermission{{"edit", "*", "ask"}, {"bash", "*", "ask"}}
	if opts.Access == AccessWrite {
		rules = append(rules, opencodePermission{"external_directory", "*", "deny"})
		for _, root := range OpencodeWritableRoots(opts.WritableRoots...) {
			rules = append(rules, opencodePermission{"external_directory", strings.TrimRight(root, "/") + "/*", "ask"})
		}
		rules = append(rules, opencodePermission{"doom_loop", "*", "deny"})
	}
	return rules
}

// Warning text is persisted in results and may contain model-controlled paths.
func opencodeWarningPath(path string) string {
	path = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			return ' '
		}
		return r
	}, path))
	if path == "" {
		return "(empty)"
	}
	runes := []rune(path)
	if len(runes) > 240 {
		path = string(runes[:237]) + "..."
	}
	return path
}
