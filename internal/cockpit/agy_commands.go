package cockpit

import (
	"slices"
	"strings"

	"github.com/mattn/go-shellwords"
)

// Native plan-mode permissions are prefixes. The hook is the authority for
// arguments; opencode deliberately retains its independent exact patterns.
var agyReadPrograms = []string{"ls", "cat", "head", "tail", "wc", "stat", "file", "pwd", "du", "grep", "egrep", "fgrep", "rg", "find", "git", "go"}

func agyReadCommand(command string) ([]string, bool) {
	// Reject even quoted shell control syntax. Dollar expansion is also denied:
	// validation must see the actual arguments, never environment-derived flags.
	if strings.ContainsAny(command, ";|&><$`\r\n\x00") {
		return nil, false
	}
	parser := shellwords.NewParser()
	parser.ParseEnv, parser.ParseBacktick = false, false
	args, err := parser.Parse(command)
	if err != nil || len(args) == 0 || !slices.Contains(agyReadPrograms, args[0]) {
		return nil, false
	}
	switch args[0] {
	case "grep", "egrep", "fgrep":
		if !agyReadOptions(args[1:], "rRnilLcwxEFvHhosq", "eABC", map[string]string{"--include": "*", "--exclude": "*", "--exclude-dir": "*", "--color": "never"}) {
			return nil, false
		}
	case "rg":
		if !agyReadOptions(args[1:], "nilcwFSsu", "egtTABC", map[string]string{"--glob": "*", "--type": "*", "--hidden": "", "--no-ignore": "", "--files": "", "--json": ""}) {
			return nil, false
		}
	case "find":
		if !agyReadFind(args[1:]) {
			return nil, false
		}
	case "file":
		if !agyReadFile(args[1:]) {
			return nil, false
		}
	case "wc":
		for _, arg := range args[1:] {
			if arg == "--" {
				break
			}
			if agyLongOptionMatches(arg, "--files0-from") {
				return nil, false
			}
		}
	}
	if args[0] == "git" {
		for _, arg := range args[1:] {
			if agyUnsafeGitOption(arg) {
				return nil, false
			}
		}
		subcommand, ok := agyGitSubcommand(args)
		if !ok {
			return nil, false
		}
		if args[subcommand] == "branch" {
			for _, arg := range args[subcommand+1:] {
				if !slices.Contains([]string{"--list", "-a", "-r", "-v"}, arg) {
					return nil, false
				}
			}
		} else if !slices.Contains([]string{"status", "log", "show", "diff", "ls-files", "ls-tree", "grep", "blame", "rev-parse", "describe", "shortlog", "cat-file", "for-each-ref"}, args[subcommand]) {
			return nil, false
		}
	}
	if args[0] == "go" {
		if len(args) < 2 || !slices.Contains([]string{"doc", "env", "version"}, args[1]) {
			return nil, false
		}
		if args[1] == "env" {
			for _, arg := range args[2:] {
				if !agyEnvVariable(arg) {
					return nil, false
				}
			}
		}
	}
	return args, true
}

// Each short option is either a switch or consumes one value (possibly attached).
// Long options consume a value only when declared: "*" accepts any value,
// a literal restricts it, and "" denotes a switch. Unknown options fail closed.
func agyReadOptions(args []string, switches, values string, long map[string]string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return true
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			continue
		}
		if strings.HasPrefix(arg, "--") {
			name, value, attached := strings.Cut(arg, "=")
			rule, ok := long[name]
			if !ok {
				return false
			}
			if rule == "" {
				if attached {
					return false
				}
				continue
			}
			if !attached {
				i++
				if i >= len(args) {
					return false
				}
				value = args[i]
			}
			if rule != "*" && value != rule {
				return false
			}
			continue
		}
		for j := 1; j < len(arg); j++ {
			if strings.ContainsRune(values, rune(arg[j])) {
				if j == len(arg)-1 {
					i++
					if i >= len(args) {
						return false
					}
				}
				break
			}
			if !strings.ContainsRune(switches, rune(arg[j])) {
				return false
			}
		}
	}
	return true
}

func agyReadFind(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "-name", "-iname", "-path", "-ipath", "-type", "-maxdepth", "-mindepth", "-size", "-mtime", "-newer":
			i++
			if i >= len(args) {
				return false
			}
		case "-print", "-print0", "-prune", "-not", "!", "-o", "-a", "(", ")":
		default:
			if strings.HasPrefix(arg, "-") {
				return false
			}
		}
	}
	return true
}

func agyReadFile(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			return true
		}
		if strings.HasPrefix(arg, "--") {
			if agyLongOptionMatches(arg, "--compile") || agyLongOptionMatches(arg, "--magic-file") {
				return false
			}
			continue
		}
		if !strings.HasPrefix(arg, "-") {
			continue
		}
		for j := 1; j < len(arg); j++ {
			if arg[j] == 'C' || arg[j] == 'm' {
				return false
			}
			if strings.ContainsRune("fFeP", rune(arg[j])) {
				if j == len(arg)-1 {
					i++
				}
				break
			}
		}
	}
	return true
}

func agyEnvVariable(value string) bool {
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for _, c := range value {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return false
		}
	}
	return true
}

func agyGitSubcommand(args []string) (int, bool) {
	for i := 1; i < len(args); i++ {
		arg := args[i]
		if !strings.HasPrefix(arg, "-") {
			return i, true
		}
		switch arg {
		case "--no-pager", "--no-optional-locks", "--literal-pathspecs", "--no-replace-objects":
		default:
			return 0, false
		}
	}
	return 0, false
}

func agyUnsafeGitOption(arg string) bool {
	if strings.HasPrefix(arg, "-O") {
		return true
	}
	// Git accepts abbreviated long options. cat-file filters execute configured
	// smudge commands and must not escape the read-only helper restrictions.
	// Repository redirection is also refused after the subcommand, so safety does
	// not depend on git rejecting it there.
	for _, denied := range []string{"--output", "--open-files-in-pager", "--ext-diff", "--textconv", "--filters", "--no-index", "--git-dir", "--work-tree"} {
		if agyLongOptionMatches(arg, denied) {
			return true
		}
	}
	return false
}

func agyLongOptionMatches(arg, denied string) bool {
	if !strings.HasPrefix(arg, "--") || arg == "--" {
		return false
	}
	option, _, _ := strings.Cut(arg, "=")
	return strings.HasPrefix(denied, option) || strings.HasPrefix(option, denied)
}

func agySafeGit(args []string) string {
	subcommand, _ := agyGitSubcommand(args) // Already validated by agyReadCommand.
	safe := []string{"git", "--no-pager", "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null"}
	safe = append(safe, args[1:subcommand+1]...)
	if slices.Contains([]string{"diff", "show", "log"}, args[subcommand]) {
		safe = append(safe, "--no-ext-diff", "--no-textconv")
	}
	if args[subcommand] == "blame" {
		safe = append(safe, "--no-textconv")
	}
	safe = append(safe, args[subcommand+1:]...)
	return agyQuoteArgs(safe)
}

func agyQuoteArgs(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", "'\\''") + "'"
	}
	return strings.Join(quoted, " ")
}
