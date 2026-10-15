package sub

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// JobSummary is the shared contract for status, /jobs and the local panel.
// Empty metadata stays empty for historical jobs; it is never guessed from ids.
type JobSummary struct {
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	Issue          string           `json:"issue"`
	Harness        string           `json:"harness"`
	ModelRequested string           `json:"modelRequested"`
	ModelActual    string           `json:"modelActual"`
	Access         string           `json:"access"`
	ConfigMode     string           `json:"configMode"`
	Repo           string           `json:"repo"`
	Cwd            string           `json:"cwd"`
	Worktree       *SummaryWorktree `json:"worktree,omitempty"`
	Status         Status           `json:"status"`
	CreatedAt      time.Time        `json:"createdAt"`
	StartedAt      *time.Time       `json:"startedAt,omitempty"`
	FinishedAt     *time.Time       `json:"finishedAt,omitempty"`
	Duration       string           `json:"duration"`
	Attention      []Attention      `json:"attention"`
	Activity       *Activity        `json:"activity,omitempty"`
	ResumeOf       string           `json:"resumeOf"`
	ResumedBy      []string         `json:"resumedBy"`
	InputTokens    int              `json:"inputTokens"`
	OutputTokens   int              `json:"outputTokens"`
	ChangedFiles   int              `json:"changedFiles"`
	AddedLines     int              `json:"addedLines"`
	DeletedLines   int              `json:"deletedLines"`
	WarningCount   int              `json:"warningCount"`
	SessionID      string           `json:"sessionId"`
}

type SummaryWorktree struct {
	Worktree
	Status WorktreeStatus `json:"status"`
}

type WorktreeStatus string

const (
	WorktreePending   WorktreeStatus = "pending"
	WorktreeApplied   WorktreeStatus = "applied"
	WorktreeDiscarded WorktreeStatus = "discarded"
)

type AttentionKind string

const (
	AttentionWorktreePending AttentionKind = "worktree_pending"
	AttentionFailed          AttentionKind = "failed"
	AttentionWarnings        AttentionKind = "warnings"
)

type Attention struct {
	Kind    AttentionKind `json:"kind"`
	Message string        `json:"message"`
}

type Activity struct {
	Summary      string `json:"summary"`
	InputTokens  *int   `json:"inputTokens,omitempty"`
	OutputTokens *int   `json:"outputTokens,omitempty"`
	UsagePartial bool   `json:"usagePartial,omitempty"`
}

var briefIssue = regexp.MustCompile(`\b[A-Z]+-[0-9]+\b`)

func briefSummary(brief string) (title, issue string) {
	// Only the first ten physical lines are issue metadata, not the task body.
	foundTitle := false
	i := 0
	for line := range strings.SplitSeq(brief, "\n") {
		if !foundTitle && strings.TrimSpace(line) != "" && !briefLabel.MatchString(strings.TrimSpace(line)) {
			foundTitle = true
			clean := strings.TrimSpace(line)
			if strings.HasPrefix(clean, "#") {
				clean = strings.TrimSpace(strings.TrimLeft(clean, "#"))
				if end := strings.TrimRight(clean, "#"); len(end) < len(clean) && (end == "" || strings.HasSuffix(end, " ") || strings.HasSuffix(end, "\t")) {
					clean = strings.TrimSpace(end)
				}
			}
			// Strip emphasis markers but keep underscores inside identifiers
			// such as KIMI_CODE_HOME.
			clean = strings.Trim(strings.NewReplacer("**", "", "__", "", "`", "").Replace(clean), "*_ ")
			title = truncateSummary(strings.Join(strings.Fields(clean), " "), 80)
		}
		if i < 10 && issue == "" {
			issue = briefIssue.FindString(line)
		}
		if foundTitle && (issue != "" || i >= 9) {
			break
		}
		i++
	}
	return
}

func truncateSummary(text string, limit int) string {
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit-1]) + "…"
	}
	return text
}

// JobRepo returns the original repository, even for an isolated run. The git
// fallback supports old jobs while their source directory still exists.
func JobRepo(st State) string {
	if st.Worktree != nil {
		return st.Worktree.OriginalRoot
	}
	if st.Repo != "" {
		return st.Repo
	}
	if st.Request.Cwd == "" {
		return ""
	}
	root, _ := repositoryRoot(st.Request.Cwd)
	return root
}

// NormalizeRepo resolves a repository path (including a subdirectory) to its
// root. Nonexistent historical paths still compare by their absolute spelling.
func NormalizeRepo(path string) string {
	if path == "" {
		return ""
	}
	if root, err := repositoryRoot(path); err == nil {
		path = root
	}
	if absolute, err := filepath.Abs(path); err == nil {
		path = absolute
	}
	if canonical, err := filepath.EvalSymlinks(path); err == nil {
		path = canonical
	}
	return filepath.Clean(path)
}

// Summarize reads optional result/log data without mutating a job. State must
// come from Load/Recover (atomic replacement), and peers supply reverse links.
// A missing or damaged result does not hide a job, matching JSON status history.
func (s Store) Summarize(st State, peers []State, now time.Time) JobSummary {
	title, issue := briefSummary(st.Request.Brief)
	row := JobSummary{ID: st.ID, Title: title, Issue: issue, Harness: st.Request.Harness, ModelRequested: st.Request.Model, Access: st.Request.Access, ConfigMode: st.ConfigMode, Repo: JobRepo(st), Cwd: st.Request.Cwd, Status: st.Status, CreatedAt: st.CreatedAt, ResumeOf: st.ResumeOf, Attention: []Attention{}, ResumedBy: []string{}}
	end := st.FinishedAt
	if end.IsZero() {
		end = now
	} else {
		row.FinishedAt = &end
	}
	row.Duration = end.Sub(st.CreatedAt).Round(time.Second).String()
	if !st.StartedAt.IsZero() {
		started := st.StartedAt
		row.StartedAt = &started
	}
	if st.Worktree != nil {
		w := SummaryWorktree{Worktree: *st.Worktree, Status: WorktreePending}
		if w.Applied {
			w.Status = WorktreeApplied
		}
		if w.Discarded {
			w.Status = WorktreeDiscarded
		}
		row.Worktree = &w
		row.Cwd = w.Cwd
	}
	for _, peer := range peers {
		if peer.ResumeOf == st.ID && st.ID != "" {
			row.ResumedBy = append(row.ResumedBy, peer.ID)
		}
	}
	sort.Strings(row.ResumedBy)
	var result Result
	if data, err := os.ReadFile(s.ResultPath(st.ID)); err == nil {
		var parsed Result
		if json.Unmarshal(data, &parsed) == nil {
			result = parsed
		}
	}
	row.ModelActual = result.ModelActual
	row.InputTokens, row.OutputTokens = result.InputTokens, result.OutputTokens
	row.ChangedFiles, row.AddedLines, row.DeletedLines = len(result.Diff.Files), result.Diff.Added, result.Diff.Deleted
	row.WarningCount, row.SessionID = len(result.Warnings), result.SessionID
	if Terminal(st.Status) {
		if row.Worktree != nil && row.Worktree.Status == WorktreePending {
			row.Attention = append(row.Attention, Attention{AttentionWorktreePending, "Worktree is awaiting apply or discard"})
		}
		if st.Status == StatusFailed {
			row.Attention = append(row.Attention, Attention{AttentionFailed, "Job failed"})
		}
		if actionableWarnings(result) > 0 {
			row.Attention = append(row.Attention, Attention{AttentionWarnings, "Result contains warnings"})
		}
	}
	if st.Status == StatusRunning {
		row.Activity = s.activity(st.ID)
	}
	return row
}

const activityTailBytes = 256 * 1024

// activity reads at most 256 KiB from a fixed EOF snapshot. Only newline-committed
// records count: both a clipped first record and an in-flight final record drop.
func (s Store) activity(id string) *Activity {
	a := &Activity{}
	file, err := os.Open(s.LogPath(id))
	if err != nil {
		return a
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return a
	}
	start := max(int64(0), info.Size()-activityTailBytes)
	data, err := io.ReadAll(io.NewSectionReader(file, start, info.Size()-start))
	if err != nil {
		return a
	}
	if start > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			return a
		}
		data = data[i+1:]
	}
	end := bytes.LastIndexByte(data, '\n')
	if end < 0 {
		return a
	}
	data = data[:end]
	renderer := &logRenderer{}
	collector := Collector{}
	latest := -1
	rendered := -1
	index := 0
	var last logEvent
	emit := func(line string) error { a.Summary = singleLogLine(line); rendered = index; return nil }
	hasUsage := false
	for i, line := range bytes.Split(data, []byte{'\n'}) {
		event, ok := parseLogEvent(string(line))
		if !ok {
			continue
		}
		if event.Kind == cockpit.EventUsage {
			collector.Add(event.Event)
			if event.Usage != nil || collector.threadTotals != nil {
				hasUsage = true
			}
		}
		if event.Kind != cockpit.EventTool && event.Kind != cockpit.EventMessage {
			continue
		}
		index = i
		latest, last = i, event
		_ = renderer.add(string(line), emit)
	}
	index = latest
	if last.Kind == cockpit.EventMessage {
		_ = renderer.flushMessage(emit)
	}
	if latest >= 0 && rendered != latest {
		a.Summary = singleLogLine(formatLogEvent(last))
	}
	if hasUsage {
		input, output := collector.Usage.InputTokens, collector.Usage.OutputTokens
		a.InputTokens, a.OutputTokens = &input, &output
		a.UsagePartial = start > 0
	}
	return a
}

// briefLabel matches a bare section label such as "Background:" or "背景：",
// which says nothing about the task and is skipped when picking a title.
var briefLabel = regexp.MustCompile(`^[#\s]*[\p{L}\p{N} ]{1,24}[:：]$`)

// actionableWarnings counts warnings a person should act on. Informational
// notes repeat on every job (experimental harness, version not yet checked by
// doctor, uncommitted changes left out of a worktree) and would otherwise mark
// nearly every row for attention.
func actionableWarnings(r Result) int {
	experimental := ""
	if h, ok := Lookup(r.Harness); ok {
		experimental = h.Experimental
	}
	n := 0
	for _, w := range r.Warnings {
		switch {
		case w == experimental && w != "", w == dirtyWorktreeWarning, strings.HasSuffix(w, "has not been checked; run pactify sub doctor first"):
		default:
			n++
		}
	}
	return n
}
