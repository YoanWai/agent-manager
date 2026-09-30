package ui

import (
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
)

const (
	diffFileRailWidth = 28
	diffCodeMinWidth  = 20
	// The seam column and the fill's bleed edge between the two surfaces.
	diffPaneSeam = 2
)

type annotation struct {
	id       string
	file     string
	line     int // NewNum, or OldNum for pure deletions
	deleted  bool
	excerpt  string
	text     string
	hash     uint64
	round    int
	scope    string
	point    int
	handled  bool
	outdated bool
}

// diffState drives the diff panel and the full-screen review mode. The
// panel follows the tree cursor like the pane preview; fullscreen pins
// the session. Annotations and reviewed marks are keyed by session and repo
// so cursor moves never lose them.
type diffState struct {
	active     bool
	scope      git.Scope
	sessID     string
	gen        int
	loading    bool
	errText    string
	set        diff.Set
	fileIdx    int
	scroll     int
	cursorLine int
	sideBySide bool
	codeOnly   bool

	scrollByFile      map[string]int
	reviewed          map[string]map[string]uint64
	annotations       map[string][]annotation
	rounds            map[string]store.ReviewRound
	stateLoaded       map[string]bool
	reviewStatusGen   int
	reviewWriteDone   <-chan struct{}
	reviewSendPending bool

	annotating  bool
	annInput    textarea.Model
	sendConfirm bool
	notice      string

	fingerprint uint64
	probeTick   int
	hl          *hlCache
	hlPending   hlKey
	fileLoading map[int]bool
	reanchor    map[string]bool

	// repoRoots holds the git repos found under the session cwd; the r key picks
	// between them. repoSel is the selected repo's root path, the stable identity
	// carried across reloads since ResolveRepos re-ranks the list each call;
	// review state (comments, reviewed marks) is keyed by it so a same-named file
	// in a sibling repo never inherits the wrong marks.
	repoRoots []string
	repoSel   string

	// worktrees of the selected repo, cached each load so the footer can offer
	// the b branch picker only when there is more than one to switch between.
	worktrees []git.Worktree

	// Set when review opened from inside a session; leaving re-attaches it.
	reattachID string

	// Set when review opened from focus mode; leaving focuses again.
	refocus bool
}

type diffLoadedMsg struct {
	sessID            string
	scope             git.Scope
	gen               int
	set               diff.Set
	fp                uint64
	err               error
	reviewState       store.ReviewState
	reviewStateLoaded bool
	reviewStateErr    error
	// repoRoots and repoRoot report which repo the load resolved to, so the UI
	// can show the repo name and offer the picker when the cwd holds several.
	repoRoots []string
	repoRoot  string
	worktrees []git.Worktree
	// Set when the wanted repo is not under the session cwd, so the fallback to
	// the top-ranked repo is announced rather than silent.
	missingRepo string
	// refresh marks a silent reload of the same session and scope (the agent
	// edited the file under review), the only case where saved comments
	// re-anchor. Scope cycles and session switches load a different file set.
	refresh bool
}

type diffHLMsg struct {
	key hlKey
	hl  *fileHL
}

type diffFileLoadedMsg struct {
	sessID   string
	scope    git.Scope
	gen      int
	repoRoot string
	index    int
	path     string
	fd       diff.FileDiff
}

type diffFilesLoadedMsg []diffFileLoadedMsg

type diffProbeMsg struct {
	sessID   string
	scope    git.Scope
	repoRoot string
	fp       uint64
}

type reviewStatusesLoadedMsg struct {
	sessID   string
	repoRoot string
	gen      int
	handled  map[string]bool
	err      error
}

type reviewStateSavedMsg struct {
	sessID   string
	repoRoot string
	err      error
}

type reviewCommentHandledMsg struct {
	sessID    string
	repoRoot  string
	commentID string
	handled   bool
	previous  bool
	found     bool
	err       error
}

type reviewSendFinishedMsg struct {
	sessID        string
	repoRoot      string
	commentIDs    []string
	previousRound store.ReviewRound
	round         int
	count         int
	sessName      string
	delivered     bool
	err           error
	ackErr        error
}

type reviewSendRequest struct {
	sess          store.Session
	prompt        string
	state         store.ReviewState
	previousState store.ReviewState
	commentIDs    []string
	previousRound store.ReviewRound
	round         int
	count         int
}
