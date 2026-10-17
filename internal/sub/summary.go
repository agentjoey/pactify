package sub

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentjoey/pactify/internal/cockpit"
)

// JobSummary is the shared contract for status, /jobs and the local panel.
// Empty metadata stays empty for historical jobs; it is never guessed from ids.
type JobSummary struct {
	Actions        []string         `json:"actions"`
	FallbackFrom   string           `json:"fallbackFrom,omitempty"`
	FallbackTo     string           `json:"fallbackTo,omitempty"`
	RetryAttempt   int              `json:"retryAttempt,omitempty"`
	RetryLimit     int              `json:"retryLimit,omitempty"`
	RetryCount     int              `json:"retryCount,omitempty"`
	FailureClass   string           `json:"failureClass,omitempty"`
	QuotaResetAt   *time.Time       `json:"quotaResetAt,omitempty"`
	ID             string           `json:"id"`
	Title          string           `json:"title"`
	Harness        string           `json:"harness"`
	Effort         string           `json:"effort,omitempty"`
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
	CostUSD        *float64         `json:"costUSD,omitempty"`
	InputTokens    int              `json:"inputTokens"`
	OutputTokens   int              `json:"outputTokens"`
	UsagePartial   bool             `json:"usagePartial,omitempty"`
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

func briefSummary(brief string) string {
	for line := range strings.SplitSeq(brief, "\n") {
		clean := strings.TrimSpace(line)
		if clean == "" {
			continue
		}
		if strings.HasPrefix(clean, "#") {
			clean = strings.TrimSpace(strings.TrimLeft(clean, "#"))
			if end := strings.TrimRight(clean, "#"); len(end) < len(clean) && (end == "" || strings.HasSuffix(end, " ") || strings.HasSuffix(end, "\t")) {
				clean = strings.TrimSpace(end)
			}
		}
		// Preserve underscores inside identifiers such as KIMI_CODE_HOME.
		clean = strings.Trim(strings.NewReplacer("**", "", "__", "", "`", "").Replace(clean), "*_ ")
		return truncateSummary(strings.Join(strings.Fields(clean), " "), 80)
	}
	return ""
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
	if peers == nil {
		index, err := s.actionIndex(st)
		if err == nil {
			return index.Summarize(st, now)
		}
	}
	rows := append([]State{}, peers...)
	found := false
	for i := range rows {
		if rows[i].ID == st.ID {
			rows[i] = st
			found = true
			break
		}
	}
	if !found {
		rows = append(rows, st)
	}
	return s.NewSummaryIndex(rows).Summarize(st, now)
}

func (index *SummaryIndex) Summarize(st State, now time.Time) JobSummary {
	s := index.store
	title := briefSummary(st.Request.Brief)
	row := JobSummary{Actions: index.Actions(st), FallbackFrom: st.FallbackFrom, FallbackTo: st.FallbackTo, ID: st.ID, Title: title, Harness: st.Request.Harness, ModelRequested: st.Request.Model,
		Effort: st.Request.Effort, Access: st.Request.Access, ConfigMode: st.ConfigMode, Repo: JobRepo(st), Cwd: st.Request.Cwd, Status: st.Status, CreatedAt: st.CreatedAt, ResumeOf: st.ResumeOf, Attention: []Attention{}, ResumedBy: []string{}}
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
	row.ResumedBy = append([]string{}, index.resumedBy[st.ID]...)
	result := index.results[st.ID]
	if st.Status == StatusFailed || st.Status == StatusCanceled {
		row.FailureClass, row.QuotaResetAt = result.FailureClass, result.QuotaResetAt
	}
	row.RetryCount = len(result.Retries)
	if st.Status == StatusRunning {
		row.RetryAttempt, row.RetryLimit = st.RetryAttempt, st.RetryLimit
		row.RetryCount = len(st.Retries)
	}
	row.ModelActual = result.ModelActual
	row.InputTokens, row.OutputTokens = result.InputTokens, result.OutputTokens
	if result.CostUSD > 0 {
		cost := result.CostUSD
		row.CostUSD = &cost
	}
	row.UsagePartial = result.UsagePartial
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
		if event.Kind == cockpit.EventSystem {
			collector.Add(event.Event)
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

// StatusText keeps lifecycle status stable while displaying retry progress.
func (r JobSummary) StatusText(now time.Time) string {
	label := FailureStatus(r.Status, r.FailureClass, r.QuotaResetAt, now)
	if r.Status == StatusRunning && r.RetryAttempt > 0 {
		label = fmt.Sprintf("retrying (%d/%d)", r.RetryAttempt, r.RetryLimit)
	}
	if r.FallbackFrom != "" {
		label += " · fallback from " + r.FallbackFrom
	}
	if r.FallbackTo != "" {
		label += " · → fallback to " + r.FallbackTo
	}
	if Terminal(r.Status) && r.RetryCount > 0 {
		label += fmt.Sprintf(" (retries: %d)", r.RetryCount)
	}
	return label
}
