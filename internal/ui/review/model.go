package review

import (
	"hash/fnv"
	"maps"
	"slices"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/charmbracelet/bubbles/textarea"
)

const highlightCacheCap = 12

type annotation struct {
	id       string
	file     string
	line     int
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

// Model owns all Review state and acceptance policy. Its fields stay private so
// root composition cannot bypass generation, target, or draft guards.
type Model struct {
	active     bool
	scope      git.Scope
	target     Target
	gen        int
	loading    bool
	errText    string
	set        diff.Set
	fileIdx    int
	scroll     int
	cursorLine int
	sideBySide bool
	codeOnly   bool

	scrollByFile map[string]int
	reviewed     map[string]map[string]uint64
	annotations  map[string][]annotation
	rounds       map[string]Round
	stateLoaded  map[string]bool
	statusGen    int
	sendPending  bool

	annotating     textarea.Model
	annotationOpen bool
	sendConfirm    bool
	notice         string

	fingerprint      uint64
	probeTick        int
	highlights       map[HighlightKey]*Highlight
	highlightOrder   []HighlightKey
	highlightPending HighlightKey
	fileLoading      map[int]bool
	reanchor         map[string]bool

	repoRoots []string
	repoSel   string
	worktrees []Worktree
}

func New(defaultSideBySide bool) Model {
	return Model{
		sideBySide:   defaultSideBySide,
		scrollByFile: make(map[string]int),
		reviewed:     make(map[string]map[string]uint64),
		annotations:  make(map[string][]annotation),
		rounds:       make(map[string]Round),
		stateLoaded:  make(map[string]bool),
		highlights:   make(map[HighlightKey]*Highlight),
		fileLoading:  make(map[int]bool),
	}
}

func (m *Model) ensureInitialized() {
	if m.scrollByFile == nil {
		m.scrollByFile = make(map[string]int)
	}
	if m.reviewed == nil {
		m.reviewed = make(map[string]map[string]uint64)
	}
	if m.annotations == nil {
		m.annotations = make(map[string][]annotation)
	}
	if m.rounds == nil {
		m.rounds = make(map[string]Round)
	}
	if m.stateLoaded == nil {
		m.stateLoaded = make(map[string]bool)
	}
	if m.highlights == nil {
		m.highlights = make(map[HighlightKey]*Highlight)
	}
	if m.fileLoading == nil {
		m.fileLoading = make(map[int]bool)
	}
}

func (m *Model) Open(target Target, scope git.Scope, preferredRepo string) LoadRequest {
	m.ensureInitialized()
	m.active = true
	m.scope = scope
	return m.retarget(target, preferredRepo, false)
}

func (m *Model) retarget(target Target, preferredRepo string, refresh bool) LoadRequest {
	m.target = target
	m.gen++
	m.loading = true
	m.errText = ""
	m.set = diff.Set{}
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.repoRoots = nil
	m.repoSel = preferredRepo
	m.fileLoading = make(map[int]bool)
	m.reanchor = nil
	return LoadRequest{Target: target, Scope: m.scope, Generation: m.gen, RepoWanted: preferredRepo, Refresh: refresh, Resolve: true, Restored: maps.Clone(m.stateLoaded)}
}

func (m *Model) Close() int {
	m.active = false
	m.gen++
	m.loading = false
	m.errText = ""
	m.set = diff.Set{}
	m.target = Target{}
	m.fileIdx = 0
	m.scroll = 0
	m.cursorLine = 0
	m.fingerprint = 0
	m.repoRoots = nil
	m.repoSel = ""
	m.worktrees = nil
	m.fileLoading = nil
	m.reanchor = nil
	m.highlightPending = HighlightKey{}
	m.highlights = nil
	m.highlightOrder = nil
	m.annotationOpen = false
	m.sendConfirm = false
	return m.gen
}

func (m Model) Active() bool      { return m.active }
func (m Model) Generation() int   { return m.gen }
func (m Model) SessionID() string { return m.target.ID }
func (m Model) Scope() git.Scope  { return m.scope }

func (m Model) Snapshot() Snapshot {
	files := make([]FileSummary, len(m.set.Files))
	for i := range m.set.Files {
		fd := &m.set.Files[i]
		files[i] = FileSummary{
			File: fd.File, Stat: fd.Stat, Binary: fd.Binary, Truncated: fd.Truncated,
			Err: fd.Err, HasStat: fd.HasStat, IsLoaded: fd.IsLoaded,
			Hidden: m.fileHidden(fd),
		}
	}
	return Snapshot{
		Active: m.active, Scope: m.scope, SessionID: m.target.ID,
		Generation: m.gen, Loading: m.loading, Error: m.errText,
		Set: SetSummary{
			Repo: m.set.Repo, Scope: m.set.Scope, BaseDesc: m.set.BaseDesc,
			BaseRef: m.set.BaseRef, BaseOverride: m.set.BaseOverride, Files: files,
		},
		FileIndex: m.fileIdx, Scroll: m.scroll,
		CursorLine: m.cursorLine, SideBySide: m.sideBySide,
		CodeOnly: m.codeOnly, Annotating: m.annotationOpen,
		SendConfirm: m.sendConfirm,
		Notice:      m.notice, Fingerprint: m.fingerprint,
		RepoRoots: slices.Clone(m.repoRoots), RepoSelected: m.repoSel,
		Worktrees: slices.Clone(m.worktrees), SendPending: m.sendPending,
	}
}

func (m Model) reviewKey() string          { return m.target.ID + "\x00" + m.repoSel }
func (m Model) markKey(path string) string { return m.scope.String() + "\x00" + path }

func (m Model) SavedState() SavedState { return m.savedState(m.reviewKey()) }

func (m Model) savedState(key string) SavedState {
	marks := maps.Clone(m.reviewed[key])
	if marks == nil {
		marks = make(map[string]uint64)
	}
	comments := make([]Comment, 0, len(m.annotations[key]))
	for _, note := range m.annotations[key] {
		comments = append(comments, Comment{
			ID: note.id, File: note.file, Line: note.line, Deleted: note.deleted,
			Excerpt: note.excerpt, Text: note.text, ContentHash: note.hash,
			Round: note.round, Scope: note.scope, Point: note.point,
			Resolved: note.handled, Outdated: note.outdated,
		})
	}
	return SavedState{Reviewed: marks, Comments: comments, Round: m.rounds[key]}
}

func (m *Model) restore(state SavedState) bool {
	key := m.reviewKey()
	if m.target.ID == "" || m.repoSel == "" || m.stateLoaded[key] {
		return false
	}
	marks := make(map[string]uint64, len(state.Reviewed))
	for markKey, hash := range state.Reviewed {
		if hash != 0 && containsNUL(markKey) {
			marks[markKey] = hash
		}
	}
	notes := make([]annotation, 0, len(state.Comments))
	for _, note := range state.Comments {
		notes = append(notes, annotation{
			id: note.ID, file: note.File, line: note.Line, deleted: note.Deleted,
			excerpt: stripControls(note.Excerpt), text: stripControls(note.Text), hash: note.ContentHash,
			round: note.Round, scope: note.Scope, point: note.Point,
			handled: note.Resolved, outdated: note.Outdated,
		})
	}
	m.reviewed[key] = marks
	m.annotations[key] = notes
	m.rounds[key] = state.Round
	m.stateLoaded[key] = true
	return true
}

func ContentHash(fd *diff.FileDiff) uint64 {
	hash := fnv.New64a()
	for _, line := range fd.Lines {
		_, _ = hash.Write([]byte{byte(line.Kind)})
		_, _ = hash.Write([]byte(line.Text))
		_, _ = hash.Write([]byte{'\n'})
	}
	return hash.Sum64()
}

func (m Model) currentFile() *diff.FileDiff {
	if m.fileIdx < 0 || m.fileIdx >= len(m.set.Files) {
		return nil
	}
	return &m.set.Files[m.fileIdx]
}

func (m Model) CurrentFile() (diff.FileDiff, bool) {
	fd := m.currentFile()
	if fd == nil {
		return diff.FileDiff{}, false
	}
	return fd.Clone(), true
}

// SetCopy returns an owned copy for adapters and tests that need the complete
// data set. Routine rendering should use Snapshot and CurrentFile.
func (m Model) SetCopy() diff.Set { return m.set.Clone() }

func (m Model) CurrentHighlight() *Highlight {
	fd := m.currentFile()
	if fd == nil {
		return nil
	}
	return m.highlights[HighlightKey{TargetID: m.target.ID, Scope: m.scope, Path: fd.File.Path, Hash: ContentHash(fd)}]
}

func (m *Model) putHighlight(key HighlightKey, hl *Highlight) {
	if m.highlights == nil {
		m.highlights = make(map[HighlightKey]*Highlight)
	}
	if _, exists := m.highlights[key]; !exists {
		m.highlightOrder = append(m.highlightOrder, key)
		if len(m.highlightOrder) > highlightCacheCap {
			delete(m.highlights, m.highlightOrder[0])
			m.highlightOrder = m.highlightOrder[1:]
		}
	}
	m.highlights[key] = hl
}

func zeroDiffSet() diff.Set { return diff.Set{} }

func scopeBranch() git.Scope { return git.ScopeBranch }
