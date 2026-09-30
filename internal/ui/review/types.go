package review

import (
	"time"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	tea "github.com/charmbracelet/bubbletea"
)

// Target is the immutable part of a session Review needs. Concrete store
// records and services stay in the root adapter.
type Target struct {
	ID   string
	Name string
	Tool string
	Cwd  string
}

type Round struct {
	Number      int
	Scope       string
	Fingerprint uint64
}

type Comment struct {
	ID          string
	File        string
	Line        int
	Deleted     bool
	Excerpt     string
	Text        string
	ContentHash uint64
	Round       int
	Scope       string
	Point       int
	Resolved    bool
	Outdated    bool
}

type SavedState struct {
	Reviewed map[string]uint64
	Comments []Comment
	Round    Round
}

// FileSummary is the immutable, line-free file metadata used by list and
// header renderers. Full file content is available through CurrentFile.
type FileSummary struct {
	File      git.ChangedFile
	Stat      git.FileStat
	Binary    bool
	Truncated bool
	Err       error
	HasStat   bool
	IsLoaded  bool
	Hidden    bool
}

func (f FileSummary) StatKnown() bool { return f.HasStat }
func (f FileSummary) Loaded() bool    { return f.IsLoaded }

type SetSummary struct {
	Repo         git.Repo
	Scope        git.Scope
	BaseDesc     string
	BaseRef      string
	BaseOverride string
	Files        []FileSummary
}

type Worktree struct {
	Root   string
	Branch string
}

type Snapshot struct {
	Active       bool
	Scope        git.Scope
	SessionID    string
	Generation   int
	Loading      bool
	Error        string
	Set          SetSummary
	FileIndex    int
	Scroll       int
	CursorLine   int
	SideBySide   bool
	CodeOnly     bool
	Annotating   bool
	SendConfirm  bool
	Notice       string
	Fingerprint  uint64
	RepoRoots    []string
	RepoSelected string
	Worktrees    []Worktree
	SendPending  bool
}

type LoadRequest struct {
	Target       Target
	Scope        git.Scope
	Generation   int
	RepoWanted   string
	RepoRoot     string
	RepoRoots    []string
	Refresh      bool
	Resolve      bool
	Restored     map[string]bool
	BaseOverride *string
}

type LoadResult struct {
	TargetID    string
	Scope       git.Scope
	Generation  int
	Set         diff.Set
	Fingerprint uint64
	Err         error
	Saved       SavedState
	SavedLoaded bool
	SavedErr    error
	RepoRoots   []string
	RepoRoot    string
	Worktrees   []Worktree
	MissingRepo string
	Refresh     bool
}

type FileRequest struct {
	TargetID   string
	Scope      git.Scope
	Generation int
	RepoRoot   string
	Index      int
	Path       string
	Set        diff.Set
}

type FileResult struct {
	TargetID   string
	Scope      git.Scope
	Generation int
	RepoRoot   string
	Index      int
	Path       string
	File       diff.FileDiff
}

type ProbeRequest struct {
	Target       Target
	Scope        git.Scope
	RepoSelected string
	GitRoot      string
}

type ProbeResult struct {
	TargetID     string
	Scope        git.Scope
	RepoSelected string
	Fingerprint  uint64
}

type HighlightKey struct {
	TargetID string
	Scope    git.Scope
	Path     string
	Hash     uint64
}

// Highlight is renderer-owned prepared syntax text stored behind a feature
// identity key. Its fields are intentionally private; callers can only read a
// line through Line.
type Highlight struct {
	lines []string
}

func NewHighlight(lines []string) *Highlight {
	return &Highlight{lines: append([]string(nil), lines...)}
}

func (h *Highlight) Line(index int, fallback string) string {
	if h == nil || index < 0 || index >= len(h.lines) || h.lines[index] == "" {
		return fallback
	}
	return h.lines[index]
}

type HighlightRequest struct {
	Key  HighlightKey
	File diff.FileDiff
}

type HighlightResult struct {
	Key       HighlightKey
	Highlight *Highlight
}

type SaveRequest struct {
	TargetID string
	RepoRoot string
	State    SavedState
}

type SaveResult struct {
	TargetID string
	RepoRoot string
	Err      error
}

type StatusRequest struct {
	TargetID   string
	RepoRoot   string
	Generation int
}

type StatusResult struct {
	TargetID   string
	RepoRoot   string
	Generation int
	Handled    map[string]bool
	Err        error
}

type HandleCommentRequest struct {
	TargetID  string
	RepoRoot  string
	CommentID string
	Handled   bool
	Previous  bool
}

type HandleCommentResult struct {
	TargetID  string
	RepoRoot  string
	CommentID string
	Handled   bool
	Previous  bool
	Found     bool
	Err       error
}

type SendRequest struct {
	Target        Target
	RepoRoot      string
	Prompt        string
	State         SavedState
	PreviousState SavedState
	CommentIDs    []string
	PreviousRound Round
	Round         int
	Count         int
}

type SendResult struct {
	TargetID      string
	RepoRoot      string
	CommentIDs    []string
	PreviousRound Round
	Round         int
	Count         int
	TargetName    string
	Delivered     bool
	Err           error
	AckErr        error
}

type FileCheckRequest struct {
	TargetID   string
	RepoRoot   string
	Generation int
	Path       string
}

type FileCheckResult struct {
	Request FileCheckRequest
	Err     error
}

type Requests struct {
	Load        *LoadRequest
	Files       []FileRequest
	Highlight   *HighlightRequest
	Save        *SaveRequest
	Status      *StatusRequest
	Handle      *HandleCommentRequest
	Send        *SendRequest
	FileCheck   *FileCheckRequest
	WidgetCmd   tea.Cmd
	StartupTick bool
}

type ApplyResult struct {
	Accepted            bool
	Requests            Requests
	Error               string
	Notice              string
	Refresh             bool
	ForgetPreferredRepo string
}

type Navigation int

const (
	NavigationNone Navigation = iota
	NavigationExit
	NavigationHelp
	NavigationRepoPicker
	NavigationBranchPicker
	NavigationBasePicker
	NavigationOpenEditor
	NavigationQuit
)

type KeyContext struct {
	CodeHeight           int
	Now                  time.Time
	Target               Target
	IsShell              bool
	PersistenceAvailable bool
	SendError            string
}

type KeyResult struct {
	Navigation Navigation
	Requests   Requests
	Error      string
	Consumed   bool
}
