package ui

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// annotation is one draft or sent review comment anchored to a diff line.
type annotation struct {
	id       string
	file     string
	line     int // NewNum, or OldNum for pure deletions
	deleted  bool
	excerpt  string
	text     string
	hash     uint64
	round    int
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

	scrollByFile map[string]int
	reviewed     map[string]map[string]uint64
	annotations  map[string][]annotation
	rounds       map[string]store.ReviewRound
	stateLoaded  map[string]bool

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
	sessID string
	scope  git.Scope
	gen    int
	set    diff.Set
	fp     uint64
	err    error
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

// repoWant is matched by path so the selection survives ResolveRepos re-ranking between loads.
func (m *Model) diffLoadCmd(sess store.Session, scope git.Scope, gen int, repoWant string, refresh bool) tea.Cmd {
	driver := m.gitDrv
	stor := m.store
	var cachedRoots []string
	var cachedCWD string
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, refresh: refresh}
		var roots []string
		var err error
		if sess.Cwd == cachedCWD && cachedRoots != nil {
			roots = cachedRoots
		} else {
			roots, err = driver.ResolveRepos(sess.Cwd)
			if err != nil {
				msg.err = err
				return msg
			}
			cachedCWD = sess.Cwd
			cachedRoots = roots
		}
		repoIdx, found := 0, false
		for i, root := range roots {
			if root == repoWant {
				repoIdx, found = i, true
				break
			}
		}
		if repoWant != "" && !found {
			if driver.IsRepoRoot(repoWant) {
				roots = append(roots, repoWant)
				repoIdx = len(roots) - 1
			} else {
				msg.missingRepo = repoWant
			}
		}
		msg.repoRoots = roots
		msg.repoRoot = roots[repoIdx]
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(roots[repoIdx]))
		if err != nil {
			msg.err = err
			return msg
		}
		finishDiffMsg(driver, scope, gen, roots[repoIdx], override, roots, &msg)
		return msg
	}
}

func finishDiffMsg(driver *git.Driver, scope git.Scope, gen int, gitRoot, override string, repoRoots []string, msg *diffLoadedMsg) {
	msg.repoRoots = repoRoots
	msg.repoRoot = gitRoot
	msg.set, msg.err = diff.BuildSet(driver, gitRoot, scope, override)
	if msg.err == nil {
		baseRef := msg.set.BaseRef
		if scope == git.ScopeBranch && baseRef == "" {
			baseRef, _, _ = driver.BranchBase(msg.set.Repo.Root, override)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			msg.fp, _ = driver.Fingerprint(msg.set.Repo.Root, scope, baseRef)
		}()
		go func() {
			defer wg.Done()
			msg.worktrees, _ = driver.Worktrees(msg.set.Repo.Root)
		}()
		wg.Wait()
	}
}

func (m *Model) diffReloadCmd(sess store.Session, scope git.Scope, gen int, gitRoot, override string, repoRoots []string) tea.Cmd {
	driver := m.gitDrv
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen}
		finishDiffMsg(driver, scope, gen, gitRoot, override, repoRoots, &msg)
		return msg
	}
}

func (m *Model) diffHLCmd(fd diff.FileDiff, key hlKey) tea.Cmd {
	return func() tea.Msg {
		return diffHLMsg{key: key, hl: highlightFile(&fd)}
	}
}

func (m *Model) diffFileLoadCmd(set diff.Set, sessID string, scope git.Scope, gen, index int, path string) tea.Cmd {
	driver := m.gitDrv
	return func() tea.Msg {
		return diffFileLoadedMsg{
			sessID:   sessID,
			scope:    scope,
			gen:      gen,
			repoRoot: set.Repo.Root,
			index:    index,
			path:     path,
			fd:       diff.LoadFile(driver, set, 0),
		}
	}
}

func diffFilesLoadCmd(cmds []tea.Cmd) tea.Cmd {
	if len(cmds) == 0 {
		return nil
	}
	return func() tea.Msg {
		msgs := make(diffFilesLoadedMsg, 0, len(cmds))
		for _, cmd := range cmds {
			if cmd == nil {
				continue
			}
			if msg, ok := cmd().(diffFileLoadedMsg); ok {
				msgs = append(msgs, msg)
			}
		}
		return msgs
	}
}

func (m *Model) diffProbeCmd(sess store.Session, scope git.Scope) tea.Cmd {
	driver := m.gitDrv
	// The override is keyed by the raw selection while the git operations run
	// against the resolved toplevel - the same split diffLoadCmd uses, so probe
	// and load produce the same fingerprint instead of reloading every tick.
	repoSel := m.diff.repoSel
	gitRoot := m.diff.set.Repo.Root
	stor := m.store
	return func() tea.Msg {
		// Resolve symlinks so the key matches both the CLI writer and the load
		// closure, keeping probe and load fingerprints identical.
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(repoSel))
		if err != nil {
			return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: repoSel, fp: 0}
		}
		baseRef := ""
		if scope == git.ScopeBranch {
			baseRef, _, _ = driver.BranchBase(gitRoot, override)
		}
		fp, err := driver.Fingerprint(gitRoot, scope, baseRef)
		if err != nil {
			return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: repoSel, fp: 0}
		}
		return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: repoSel, fp: fp}
	}
}

// retargetDiff points the open diff at a session, reloading its set. The repo
// selection is seeded from this session's hand-picked repo, then the agent's
// declared one, then the ranking.
func (m *Model) retargetDiff(sess store.Session) tea.Cmd {
	m.diff.sessID = sess.ID
	m.diff.gen++
	m.diff.loading = true
	m.diff.errText = ""
	m.diff.set = diff.Set{}
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	m.diff.repoRoots = nil
	m.diff.repoSel = ""
	m.diff.fileLoading = nil
	m.diff.reanchor = nil
	if picked, ok := m.pickedRepos[sess.ID]; ok {
		m.diff.repoSel = picked
	} else if declared, err := m.store.ReviewRepo(sess.ID); err != nil {
		m.errBar.text = err.Error()
	} else if declared != "" {
		m.diff.repoSel = declared
	}
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

func (m *Model) applyStoredScope(sessionID string) {
	if m.store == nil {
		return
	}
	stored, err := m.store.ReviewScope(sessionID)
	if err != nil || stored == "" {
		return
	}
	switch stored {
	case "branch":
		m.diff.scope = git.ScopeBranch
	case "last_commit":
		m.diff.scope = git.ScopeLastCommit
	case "staged":
		m.diff.scope = git.ScopeStaged
	case "uncommitted":
		m.diff.scope = git.ScopeUncommitted
	}
}

func (m *Model) cycleDiffScope() tea.Cmd {
	if !m.diff.active {
		return nil
	}
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	var override string
	if m.diff.repoSel != "" && len(m.diff.repoRoots) > 0 {
		var err error
		override, err = m.store.ReviewBase(sess.ID, resolveSymlinksOrSelf(m.diff.repoSel))
		if err != nil {
			m.errBar.text = err.Error()
			return nil
		}
	}
	m.diff.scope = m.diff.scope.Next()
	m.diff.gen++
	m.diff.loading = true
	m.diff.errText = ""
	m.diff.set = diff.Set{}
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	m.diff.fileLoading = nil
	m.diff.reanchor = nil
	if m.diff.repoSel != "" && len(m.diff.repoRoots) > 0 {
		return tea.Batch(m.diffReloadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, override, m.diff.repoRoots), m.startStartupTick())
	}
	return tea.Batch(m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false), m.startStartupTick())
}

// reviewKey scopes a session's comments and reviewed marks to the repo under
// review, so switching repos never leaks marks between same-named files.
func (m *Model) reviewKey() string {
	return m.diff.sessID + "\x00" + m.diff.repoSel
}

func (m *Model) loadReviewState() bool {
	key := m.reviewKey()
	if m.store == nil || m.diff.sessID == "" || m.diff.repoSel == "" || m.diff.stateLoaded[key] {
		return false
	}
	if m.diff.reviewed == nil {
		m.diff.reviewed = map[string]map[string]uint64{}
	}
	if m.diff.annotations == nil {
		m.diff.annotations = map[string][]annotation{}
	}
	if m.diff.rounds == nil {
		m.diff.rounds = map[string]store.ReviewRound{}
	}
	if m.diff.stateLoaded == nil {
		m.diff.stateLoaded = map[string]bool{}
	}
	state, err := m.store.ReviewState(m.diff.sessID, m.diff.repoSel)
	if err != nil {
		m.errBar.text = "loading review state: " + err.Error()
		return false
	}
	marks := make(map[string]uint64, len(state.Reviewed))
	for path, hash := range state.Reviewed {
		if hash != 0 {
			marks[path] = hash
		}
	}
	notes := make([]annotation, 0, len(state.Comments))
	points := map[int]int{}
	migrated := false
	for i := range state.Comments {
		note := &state.Comments[i]
		if note.ID == "" {
			note.ID = newReviewCommentID()
			migrated = true
		}
		if note.Round > 0 {
			points[note.Round]++
			if note.Point == 0 {
				note.Point = points[note.Round]
				migrated = true
			} else if note.Point > points[note.Round] {
				points[note.Round] = note.Point
			}
		}
		notes = append(notes, annotation{
			id: note.ID, file: note.File, line: note.Line, deleted: note.Deleted,
			excerpt: note.Excerpt, text: note.Text, hash: note.ContentHash,
			round: note.Round, point: note.Point, handled: note.Resolved, outdated: note.Outdated,
		})
	}
	m.diff.reviewed[key] = marks
	m.diff.annotations[key] = notes
	m.diff.rounds[key] = state.Round
	m.diff.stateLoaded[key] = true
	if migrated {
		if err := m.store.SetReviewState(m.diff.sessID, m.diff.repoSel, state); err != nil {
			m.errBar.text = "migrating review comments: " + err.Error()
		}
	}
	return true
}

func (m *Model) reloadReviewState() {
	if !m.diff.active || m.diff.sessID == "" || m.diff.repoSel == "" {
		return
	}
	delete(m.diff.stateLoaded, m.reviewKey())
	m.loadReviewState()
}

func (m *Model) persistReviewState() error {
	if m.store == nil || m.diff.sessID == "" || m.diff.repoSel == "" {
		return nil
	}
	key := m.reviewKey()
	marks := make(map[string]uint64, len(m.diff.reviewed[key]))
	for path, hash := range m.diff.reviewed[key] {
		if hash != 0 {
			marks[path] = hash
		}
	}
	notes := make([]store.ReviewComment, 0, len(m.diff.annotations[key]))
	for _, note := range m.diff.annotations[key] {
		notes = append(notes, store.ReviewComment{
			ID: note.id, File: note.file, Line: note.line, Deleted: note.deleted,
			Excerpt: note.excerpt, Text: note.text, ContentHash: note.hash,
			Round: note.round, Point: note.point, Resolved: note.handled, Outdated: note.outdated,
		})
	}
	return m.store.MergeReviewState(m.diff.sessID, m.diff.repoSel, store.ReviewState{
		Reviewed: marks,
		Comments: notes,
		Round:    m.diff.rounds[key],
	})
}

func (m *Model) saveReviewState() {
	if err := m.persistReviewState(); err != nil {
		m.errBar.text = "saving review state: " + err.Error()
	}
}

// diffSession resolves the session the diff is pinned to.
func (m *Model) diffSession() (store.Session, bool) {
	for _, sess := range m.sessions {
		if sess.ID == m.diff.sessID {
			return sess, true
		}
	}
	return store.Session{}, false
}

func (m *Model) handleDiffLoaded(msg diffLoadedMsg) tea.Cmd {
	if msg.sessID != m.diff.sessID || msg.scope != m.diff.scope || msg.gen != m.diff.gen {
		return nil
	}
	// Any load landing while a comment is being written or confirmed would
	// shift lines under the open editor and mis-anchor the comment, including
	// an explicit scope-cycle or retarget load still in flight when the box
	// opened. Clear the in-flight flag so probes resume, but leave the
	// fingerprint stale so the next probe reloads once the box closes.
	if m.diff.annotating || m.diff.sendConfirm {
		m.diff.loading = false
		return nil
	}
	m.diff.loading = false
	m.diff.fingerprint = msg.fp
	if msg.err != nil {
		m.diff.errText = msg.err.Error()
		m.diff.set = diff.Set{}
		m.diff.worktrees = nil
		if len(msg.repoRoots) > 0 {
			m.diff.repoRoots = msg.repoRoots
			m.diff.repoSel = msg.repoRoot
		}
		return nil
	}
	m.diff.errText = ""
	m.diff.repoRoots = msg.repoRoots
	m.diff.repoSel = msg.repoRoot
	m.diff.worktrees = msg.worktrees
	restoredState := m.loadReviewState()
	if msg.missingRepo != "" {
		m.errBar.text = fmt.Sprintf("picked or declared repo %s is no longer under the session directory",
			filepath.Base(msg.missingRepo))
		if m.pickedRepos[msg.sessID] == msg.missingRepo {
			delete(m.pickedRepos, msg.sessID)
		}
	}
	previousPath := ""
	if fd := m.currentFileDiff(); fd != nil {
		previousPath = fd.File.Path
	}
	m.diff.set = msg.set
	stateChanged := clearStaleReviewedMarks(m)
	if msg.refresh || restoredState {
		stateChanged = m.markMissingRoundCommentsOutdated() || stateChanged
	}
	// Re-anchor only on a silent same-scope refresh. A scope cycle or session
	// switch loads a different file set, where matching a comment by excerpt
	// would rewrite its line against content it was never made against.
	m.diff.fileLoading = map[int]bool{}
	m.diff.reanchor = nil
	if msg.refresh || restoredState {
		m.diff.reanchor = map[string]bool{}
		for _, note := range m.diff.annotations[m.reviewKey()] {
			m.diff.reanchor[note.file] = true
		}
	}
	// Keep the user's place across silent reloads.
	m.diff.fileIdx = 0
	for i, fd := range m.diff.set.Files {
		if fd.File.Path == previousPath {
			m.diff.fileIdx = i
			break
		}
	}
	m.diff.fileIdx = m.nextShownFile(m.diff.fileIdx, 1)
	m.clampDiffCursor()
	currentLoad := m.loadCurrentDiffFile()
	var statefulLoads []tea.Cmd
	if msg.refresh || restoredState {
		stateful := map[string]bool{}
		for path, hash := range m.diff.reviewed[m.reviewKey()] {
			if hash != 0 {
				stateful[path] = true
			}
		}
		for _, note := range m.diff.annotations[m.reviewKey()] {
			stateful[note.file] = true
		}
		for i := range m.diff.set.Files {
			if stateful[m.diff.set.Files[i].File.Path] {
				if cmd := m.loadDiffFile(i); cmd != nil {
					statefulLoads = append(statefulLoads, cmd)
				}
			}
		}
	}
	if stateChanged {
		m.saveReviewState()
	}
	// The selected file gets its own command so it becomes usable immediately.
	// Less urgent review-state files load serially in one background command,
	// avoiding an unbounded git/process fan-out after a large review refresh.
	return tea.Batch(currentLoad, diffFilesLoadCmd(statefulLoads), m.startStartupTick())
}

func (m *Model) loadCurrentDiffFile() tea.Cmd {
	return m.loadDiffFile(m.diff.fileIdx)
}

func (m *Model) loadDiffFile(index int) tea.Cmd {
	if index < 0 || index >= len(m.diff.set.Files) {
		return nil
	}
	fd := &m.diff.set.Files[index]
	if fd.Loaded() {
		if index == m.diff.fileIdx {
			return m.ensureHighlight()
		}
		return nil
	}
	if m.diff.fileLoading == nil {
		m.diff.fileLoading = map[int]bool{}
	}
	if m.diff.fileLoading[index] {
		return nil
	}
	m.diff.fileLoading[index] = true

	// Copy the requested row into a one-file set before the command starts.
	// The model can switch files or replace its set while the load runs.
	snapshot := m.diff.set
	snapshot.Files = []diff.FileDiff{*fd}
	return m.diffFileLoadCmd(snapshot, m.diff.sessID, m.diff.scope, m.diff.gen,
		index, fd.File.Path)
}

func (m *Model) handleDiffFileLoaded(msg diffFileLoadedMsg) tea.Cmd {
	if !m.diff.active || msg.sessID != m.diff.sessID || msg.scope != m.diff.scope ||
		msg.gen != m.diff.gen || msg.repoRoot != m.diff.set.Repo.Root {
		return nil
	}
	if msg.index < 0 || msg.index >= len(m.diff.set.Files) ||
		m.diff.set.Files[msg.index].File.Path != msg.path {
		return nil
	}
	delete(m.diff.fileLoading, msg.index)
	m.diff.set.Files[msg.index] = msg.fd
	stateChanged := clearStaleReviewedMark(m, msg.path)
	if m.diff.reanchor[msg.path] {
		stateChanged = m.reanchorAnnotationsFor(msg.path) || stateChanged
		delete(m.diff.reanchor, msg.path)
	}
	if stateChanged {
		m.saveReviewState()
	}
	if msg.index != m.diff.fileIdx {
		return nil
	}
	// A NUL sniff is the only thing that outs a blob whose name gives nothing
	// away, so the file under the cursor can turn into one the filter hides.
	if target := m.nextShownFile(m.diff.fileIdx, 1); target != m.diff.fileIdx {
		return m.switchDiffFile(target - m.diff.fileIdx)
	}
	m.clampDiffCursor()
	return m.ensureHighlight()
}

func (m *Model) handleDiffHL(msg diffHLMsg) {
	if m.diff.hl != nil {
		m.diff.hl.put(msg.key, msg.hl)
	}
	if m.diff.hlPending == msg.key {
		m.diff.hlPending = hlKey{}
	}
}

func (m *Model) handleDiffProbe(msg diffProbeMsg) tea.Cmd {
	if !m.diff.active || msg.sessID != m.diff.sessID || msg.scope != m.diff.scope {
		return nil
	}
	// A probe fired against the previously selected repo can land after an r
	// cycle; its fingerprint is for the old repo, so ignore it rather than let
	// the mismatch trigger a spurious refresh-reanchor on the new repo.
	if msg.repoRoot != m.diff.repoSel {
		return nil
	}
	if m.diff.loading || msg.fp == 0 || msg.fp == m.diff.fingerprint {
		return nil
	}
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	m.diff.gen++
	m.diff.loading = true
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, true)
}

// diffRefreshCmd is the poller piggyback: every second tick while the
// diff is open, probe the repo fingerprint and reload on change.
func (m *Model) diffRefreshCmd() tea.Cmd {
	if !m.diff.active || m.diff.loading || m.gitDrv == nil || m.diff.set.Repo.Root == "" {
		return nil
	}
	if m.diff.annotating || m.diff.sendConfirm {
		return nil
	}
	m.diff.probeTick++
	if m.diff.probeTick%2 != 0 {
		return nil
	}
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	return m.diffProbeCmd(sess, m.diff.scope)
}

func (m *Model) currentFileDiff() *diff.FileDiff {
	if m.diff.fileIdx < 0 || m.diff.fileIdx >= len(m.diff.set.Files) {
		return nil
	}
	return &m.diff.set.Files[m.diff.fileIdx]
}

// ensureHighlight kicks off async highlighting for the current file when
// its highlighted lines are not cached yet.
func (m *Model) ensureHighlight() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil || !fd.Loaded() || fd.Binary || fd.Err != nil || len(fd.Lines) == 0 {
		return nil
	}
	key := hlKey{sessID: m.diff.sessID, scope: m.diff.scope, path: fd.File.Path, hash: contentHash(fd)}
	if m.diff.hl.get(key) != nil || m.diff.hlPending == key {
		return nil
	}
	m.diff.hlPending = key
	return m.diffHLCmd(*fd, key)
}

func (m *Model) currentHL() *fileHL {
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	return m.diff.hl.get(hlKey{sessID: m.diff.sessID, scope: m.diff.scope, path: fd.File.Path, hash: contentHash(fd)})
}

// scrollKey scopes a file's saved scroll to the session, repo, and scope it
// was taken in, so positions never leak across sessions, repos, or diff scopes.
func (m *Model) scrollKey(path string) string {
	return m.reviewKey() + "\x00" + m.diff.scope.String() + "\x00" + path
}

// nonCodeExts and nonCodeNames name the files a review takes in whole rather
// than line by line: images, fonts, archives, compiled artifacts, and the lock
// files a package manager rewrites wholesale.
var nonCodeExts = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true, ".webp": true,
	".ico": true, ".svg": true, ".woff": true, ".woff2": true, ".ttf": true,
	".otf": true, ".eot": true, ".wasm": true, ".bin": true, ".exe": true,
	".dll": true, ".so": true, ".dylib": true, ".o": true, ".a": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true, ".xz": true,
	".7z": true, ".mp3": true, ".mp4": true, ".wav": true, ".ogg": true,
	".avi": true, ".mov": true, ".pdf": true, ".lock": true,
	".class": true, ".pyc": true,
}

var nonCodeNames = map[string]bool{
	"package-lock.json": true, "pnpm-lock.yaml": true, "go.sum": true,
}

func nonCodePath(repoPath string) bool {
	name := filepath.Base(repoPath)
	return nonCodeNames[name] || nonCodeExts[strings.ToLower(filepath.Ext(name))]
}

// diffFileHidden reports whether the code-only filter drops a file from the
// review's file list. The name is what settles the verdict before the toggle
// is ever pressed: git's numstat never classifies an untracked path, and the
// loader sniffs for NUL only once the cursor reaches a file. Git's own binary
// verdict and that sniff then catch a blob whose name gives nothing away.
func (m *Model) diffFileHidden(fd *diff.FileDiff) bool {
	return m.diff.codeOnly && (fd.Binary || fd.Stat.Binary || nonCodePath(fd.File.Path))
}

// nextShownFile walks from index in the direction dir to the first file the
// code-only filter leaves in the list, and returns index untouched when the
// filter hides every file.
func (m *Model) nextShownFile(index, dir int) int {
	count := len(m.diff.set.Files)
	if count == 0 {
		return index
	}
	for step := 0; step < count; step++ {
		candidate := ((index+dir*step)%count + count) % count
		if !m.diffFileHidden(&m.diff.set.Files[candidate]) {
			return candidate
		}
	}
	return index
}

// toggleCodeOnly hides or shows the non-code files, carrying the selection to
// a file the list still shows.
func (m *Model) toggleCodeOnly() tea.Cmd {
	m.diff.codeOnly = !m.diff.codeOnly
	target := m.nextShownFile(m.diff.fileIdx, 1)
	if target == m.diff.fileIdx {
		return nil
	}
	return m.switchDiffFile(target - m.diff.fileIdx)
}

func (m *Model) switchDiffFile(delta int) tea.Cmd {
	count := len(m.diff.set.Files)
	if count == 0 {
		return nil
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	target := m.nextShownFile((m.diff.fileIdx+delta+count)%count, dir)
	if m.diffFileHidden(&m.diff.set.Files[target]) {
		return nil
	}
	if fd := m.currentFileDiff(); fd != nil {
		m.diff.scrollByFile[m.scrollKey(fd.File.Path)] = m.diff.scroll
	}
	m.diff.fileIdx = target
	fd := m.currentFileDiff()
	m.diff.scroll = m.diff.scrollByFile[m.scrollKey(fd.File.Path)]
	m.diff.cursorLine = m.diff.scroll
	m.clampDiffCursor()
	return tea.Batch(m.loadCurrentDiffFile(), m.startStartupTick())
}

func (m *Model) clampDiffCursor() {
	fd := m.currentFileDiff()
	total := 0
	if fd != nil {
		total = m.diffRowCount(fd)
	}
	if m.diff.cursorLine >= total {
		m.diff.cursorLine = total - 1
	}
	if m.diff.cursorLine < 0 {
		m.diff.cursorLine = 0
	}
	if m.diff.scroll >= total {
		m.diff.scroll = total - 1
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// diffRowCount is the navigable row count for the active layout.
func (m *Model) diffRowCount(fd *diff.FileDiff) int {
	if m.diff.sideBySide && m.mode == modeDiff {
		return len(fd.SideBySideRows())
	}
	return len(fd.Lines)
}

// moveDiffCursor moves the fullscreen line cursor, dragging the viewport
// along when the cursor leaves it.
func (m *Model) moveDiffCursor(delta int, height int) {
	m.diff.cursorLine += delta
	m.clampDiffCursor()
	if m.diff.cursorLine < m.diff.scroll {
		m.diff.scroll = m.diff.cursorLine
	}
	if m.diff.cursorLine >= m.diff.scroll+height {
		m.diff.scroll = m.diff.cursorLine - height + 1
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// jumpChange moves the cursor to the next or previous change block.
func (m *Model) jumpChange(delta int) {
	fd := m.currentFileDiff()
	if fd == nil || len(fd.Changes) == 0 {
		return
	}
	line := m.cursorDiffLine()
	target := -1
	if delta > 0 {
		for _, start := range fd.Changes {
			if start > line {
				target = start
				break
			}
		}
		if target < 0 {
			target = fd.Changes[0]
		}
	} else {
		for i := len(fd.Changes) - 1; i >= 0; i-- {
			if fd.Changes[i] < line {
				target = fd.Changes[i]
				break
			}
		}
		if target < 0 {
			target = fd.Changes[len(fd.Changes)-1]
		}
	}
	m.setCursorDiffLine(target)
}

// cursorDiffLine maps the cursor to a Lines index in either layout.
func (m *Model) cursorDiffLine() int {
	fd := m.currentFileDiff()
	if fd == nil {
		return 0
	}
	if m.diff.sideBySide && m.mode == modeDiff {
		rows := fd.SideBySideRows()
		if m.diff.cursorLine < len(rows) {
			row := rows[m.diff.cursorLine]
			if row.Right >= 0 {
				return row.Right
			}
			return row.Left
		}
		return 0
	}
	return m.diff.cursorLine
}

func (m *Model) setCursorDiffLine(lineIdx int) {
	fd := m.currentFileDiff()
	if fd == nil {
		return
	}
	if m.diff.sideBySide && m.mode == modeDiff {
		for i, row := range fd.SideBySideRows() {
			if row.Left == lineIdx || row.Right == lineIdx {
				m.diff.cursorLine = i
				break
			}
		}
	} else {
		m.diff.cursorLine = lineIdx
	}
	m.clampDiffCursor()
	height := m.diffCodeHeight()
	if m.diff.cursorLine < m.diff.scroll || m.diff.cursorLine >= m.diff.scroll+height {
		m.diff.scroll = m.diff.cursorLine - height/2
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
}

// diffCodeHeight is the code viewport height in fullscreen review. It
// mirrors viewDiffFull's layout math, including a footer that wraps onto
// extra lines in narrow terminals. Two rows are reserved for the overflow
// indicators (↑ N more / ↓ N more) so the cursor never lands on them.
func (m *Model) diffCodeHeight() int {
	height := m.height - 6 - lipgloss.Height(m.viewDiffFooter())
	if m.diff.annotating {
		height -= m.diffAnnBarRows() + 1
	}
	height -= 2
	if height < 1 {
		height = 1
	}
	return height
}

const annotationInputMaxRows = 5

func (m *Model) diffAnnBarRows() int {
	if !m.diff.annotating {
		return 0
	}
	_, codeWidth := m.diffPaneWidths()
	return 1 + m.annotationInputHeight(codeWidth-2*contentGutter)
}

func (m *Model) annotationInputHeight(width int) int {
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	n := len(wrapTinted(m.diff.annInput.Value(), nil, "", "", inner))
	if n < 1 {
		n = 1
	}
	if n > annotationInputMaxRows {
		n = annotationInputMaxRows
	}
	return n
}

func (m *Model) diffPaneWidths() (fileWidth, codeWidth int) {
	fileWidth = m.width * 24 / 100
	if fileWidth < 28 {
		fileWidth = 28
	}
	codeWidth = m.width - fileWidth - 2
	return fileWidth, codeWidth
}

// handleDiffKey owns the whole keymap in fullscreen review mode.
func (m *Model) handleDiffKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m.diff.notice = ""
	if m.diff.annotating {
		return m.handleAnnotateKey(msg)
	}
	if m.diff.sendConfirm {
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "enter", "y":
			m.diff.sendConfirm = false
			return m.sendAnnotations()
		case "esc", "q":
			m.diff.sendConfirm = false
		}
		return m, nil
	}
	height := m.diffCodeHeight()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		return m, m.closeDiff()
	case "?":
		m.openHelp()
	case "up", "k":
		m.moveDiffCursor(-1, height)
	case "down", "j":
		m.moveDiffCursor(1, height)
	case "ctrl+d":
		m.moveDiffCursor(height/2, height)
	case "ctrl+u":
		m.moveDiffCursor(-height/2, height)
	case "pgup":
		m.moveDiffCursor(-height, height)
	case "pgdown", "pgdn":
		m.moveDiffCursor(height, height)
	case "g":
		m.diff.cursorLine = 0
		m.diff.scroll = 0
	case "G":
		if fd := m.currentFileDiff(); fd != nil {
			m.diff.cursorLine = m.diffRowCount(fd) - 1
			m.moveDiffCursor(0, height)
		}
	case "J", "tab":
		return m, m.switchDiffFile(1)
	case "K", "shift+tab":
		return m, m.switchDiffFile(-1)
	case "n":
		m.jumpChange(1)
	case "N", "shift+n":
		m.jumpChange(-1)
	case "s":
		return m, m.cycleDiffScope()
	case "r":
		m.openRepoPick()
	case "b":
		return m, m.openBranchPick()
	case "B":
		return m, m.openBasePick()
	case "u":
		lineIdx := m.cursorDiffLine()
		m.diff.sideBySide = !m.diff.sideBySide
		m.setCursorDiffLine(lineIdx)
	case "f":
		return m, m.toggleCodeOnly()
	case " ", "space":
		return m, m.toggleReviewed()
	case "c":
		m.openAnnotate()
	case "d":
		m.removeAnnotation()
	case "C":
		if m.draftAnnotationCount() == 0 {
			m.errBar.text = "no comments to send - press c on a line first"
		} else {
			m.diff.sendConfirm = true
		}
	case "o", "f3":
		return m.openDiffFile()
	}
	return m, nil
}

func (m *Model) closeDiff() tea.Cmd {
	reattachID := m.diff.reattachID
	refocus := m.diff.refocus
	m.diff.refocus = false
	m.mode = modeList
	m.diff.active = false
	m.diff.gen++
	m.diff.loading = false
	m.diff.errText = ""
	m.diff.set = diff.Set{}
	m.diff.sessID = ""
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	m.diff.fingerprint = 0
	m.diff.repoRoots = nil
	m.diff.repoSel = ""
	m.diff.worktrees = nil
	m.diff.fileLoading = nil
	m.diff.reanchor = nil
	m.diff.hlPending = hlKey{}
	m.diff.hl = nil
	m.diff.annotating = false
	m.diff.sendConfirm = false
	m.diff.reattachID = ""
	if reattachID != "" {
		return m.reattach(reattachID, m.diff.gen)
	}
	if refocus {
		_, cmd := m.focusSelected()
		return cmd
	}
	return nil
}

func (m *Model) toggleReviewed() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil || !fd.Loaded() || m.diffFileHidden(fd) {
		return nil
	}
	marks := m.diff.reviewed[m.reviewKey()]
	if marks == nil {
		marks = map[string]uint64{}
		m.diff.reviewed[m.reviewKey()] = marks
	}
	if marks[fd.File.Path] != 0 {
		delete(marks, fd.File.Path)
		m.saveReviewState()
		return nil
	}
	marks[fd.File.Path] = contentHash(fd)
	m.saveReviewState()
	// Advance to the next unreviewed file, review-queue style. The switch
	// returns the highlight command for the newly shown file; dropping it
	// would leave that file unhighlighted.
	for step := 1; step < len(m.diff.set.Files); step++ {
		next := (m.diff.fileIdx + step) % len(m.diff.set.Files)
		if marks[m.diff.set.Files[next].File.Path] == 0 && !m.diffFileHidden(&m.diff.set.Files[next]) {
			return m.switchDiffFile(next - m.diff.fileIdx)
		}
	}
	return nil
}

func (m *Model) fileReviewed(path string) bool {
	return m.diff.reviewed[m.reviewKey()][path] != 0
}

func clearStaleReviewedMarks(m *Model) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	if len(marks) == 0 {
		return false
	}
	changed := false
	for path, stored := range marks {
		if stored == 0 {
			continue
		}
		fd := m.fileDiffByPath(path)
		if fd == nil {
			delete(marks, path)
			changed = true
			continue
		}
		if fd.Loaded() && contentHash(fd) != stored {
			delete(marks, path)
			changed = true
		}
	}
	return changed
}

func clearStaleReviewedMark(m *Model, path string) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	stored := marks[path]
	if stored == 0 {
		return false
	}
	fd := m.fileDiffByPath(path)
	if fd == nil || (fd.Loaded() && contentHash(fd) != stored) {
		delete(marks, path)
		return true
	}
	return false
}

func (m *Model) markMissingRoundCommentsOutdated() bool {
	paths := make(map[string]bool, len(m.diff.set.Files))
	for i := range m.diff.set.Files {
		paths[m.diff.set.Files[i].File.Path] = true
	}
	notes := m.diff.annotations[m.reviewKey()]
	changed := false
	for i := range notes {
		if notes[i].round > 0 && !paths[notes[i].file] && !notes[i].outdated {
			notes[i].outdated = true
			changed = true
		}
	}
	return changed
}

func (m *Model) openAnnotate() {
	fd := m.currentFileDiff()
	if fd == nil || m.diffFileHidden(fd) {
		return
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return
	}
	input := textarea.New()
	input.CharLimit = 500
	input.Placeholder = "comment for the agent"
	input.ShowLineNumbers = false
	input.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "¶ "
		}
		return ""
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	input.SetHeight(1)
	if existing := m.annotationAt(fd.File.Path, fd.Lines[lineIdx]); existing != nil {
		input.SetValue(existing.text)
	}
	input.Focus()
	m.diff.annInput = input
	m.diff.annotating = true
}

func annotationLine(line diff.Line) (num int, deleted bool) {
	if line.Kind == diff.Del {
		return line.OldNum, true
	}
	return line.NewNum, false
}

func (m *Model) annotationAt(path string, line diff.Line) *annotation {
	num, deleted := annotationLine(line)
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			return &notes[i]
		}
	}
	return nil
}

func (m *Model) annotationsAt(path string, line diff.Line) []*annotation {
	num, deleted := annotationLine(line)
	notes := m.diff.annotations[m.reviewKey()]
	var matched []*annotation
	for i := range notes {
		if notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			matched = append(matched, &notes[i])
		}
	}
	return matched
}

func (m *Model) draftAnnotationCount() int {
	count := 0
	for _, note := range m.diff.annotations[m.reviewKey()] {
		if note.round == 0 {
			count++
		}
	}
	return count
}

// reanchorAnnotationsFor re-points saved comments at the line that still
// carries their excerpt after a reload shifted line numbers (the agent
// edits while the user reviews), choosing the nearest match. A comment
// whose line vanished entirely keeps its number as the best guess.
func (m *Model) reanchorAnnotationsFor(path string) bool {
	notes := m.diff.annotations[m.reviewKey()]
	changed := false
	for i := range notes {
		note := &notes[i]
		if path != "" && note.file != path {
			continue
		}
		fd := m.fileDiffByPath(note.file)
		if fd == nil {
			continue
		}
		currentHash := contentHash(fd)
		if note.hash != 0 && note.hash == currentHash {
			if note.outdated {
				note.outdated = false
				changed = true
			}
			continue
		}
		// A blank line's excerpt is empty and would match every blank line;
		// only a distinctive excerpt can re-anchor.
		if note.excerpt == "" {
			if note.round > 0 && !note.outdated {
				note.outdated = true
				changed = true
			}
			continue
		}
		matches, target := 0, 0
		for _, line := range fd.Lines {
			num, deleted := annotationLine(line)
			if deleted == note.deleted && excerptOf(line.Text) == note.excerpt {
				matches++
				target = num
			}
		}
		// Move the note only when the excerpt pins exactly one line and no
		// other note already sits there; zero, several, or contested matches
		// are ambiguous, so the stored line stays put rather than snapping to
		// the wrong line or collapsing two comments onto one.
		if matches == 1 && !m.annotationOccupies(note.file, target, note.deleted, i) {
			if note.line != target || note.hash != currentHash || note.outdated {
				note.line = target
				note.hash = currentHash
				note.outdated = false
				changed = true
			}
		} else if note.round > 0 && !note.outdated {
			note.outdated = true
			changed = true
		}
	}
	return changed
}

// annotationOccupies reports whether a note other than self already anchors
// on the given file line, so re-anchoring never stacks two comments there.
func (m *Model) annotationOccupies(file string, line int, deleted bool, self int) bool {
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if i != self && notes[i].file == file && notes[i].line == line && notes[i].deleted == deleted {
			return true
		}
	}
	return false
}

func (m *Model) fileDiffByPath(path string) *diff.FileDiff {
	for i := range m.diff.set.Files {
		if m.diff.set.Files[i].File.Path == path {
			return &m.diff.set.Files[i]
		}
	}
	return nil
}

func (m *Model) handleAnnotateKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.diff.annotating = false
		return m, nil
	case "enter":
		m.saveAnnotation()
		return m, nil
	}
	var cmd tea.Cmd
	m.diff.annInput, cmd = m.diff.annInput.Update(msg)
	return m, cmd
}

func (m *Model) saveAnnotation() {
	m.diff.annotating = false
	fd := m.currentFileDiff()
	if fd == nil {
		return
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return
	}
	text := strings.TrimSpace(m.diff.annInput.Value())
	line := fd.Lines[lineIdx]
	num, deleted := annotationLine(line)
	if existing := m.annotationAt(fd.File.Path, line); existing != nil {
		if text == "" {
			m.removeAnnotation()
			return
		}
		existing.text = text
		existing.hash = contentHash(fd)
		m.saveReviewState()
		return
	}
	if text == "" {
		return
	}
	m.diff.annotations[m.reviewKey()] = append(m.diff.annotations[m.reviewKey()], annotation{
		id:      newReviewCommentID(),
		file:    fd.File.Path,
		line:    num,
		deleted: deleted,
		excerpt: excerptOf(line.Text),
		text:    text,
		hash:    contentHash(fd),
	})
	m.saveReviewState()
}

func (m *Model) removeAnnotation() {
	fd := m.currentFileDiff()
	if fd == nil {
		return
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return
	}
	num, deleted := annotationLine(fd.Lines[lineIdx])
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == fd.File.Path && notes[i].line == num && notes[i].deleted == deleted {
			m.diff.annotations[m.reviewKey()] = append(notes[:i], notes[i+1:]...)
			m.saveReviewState()
			return
		}
	}
	latestOpen := -1
	latestHandled := -1
	for i := range notes {
		if notes[i].round == 0 || notes[i].file != fd.File.Path || notes[i].line != num || notes[i].deleted != deleted {
			continue
		}
		if notes[i].handled {
			if latestHandled < 0 || notes[i].round >= notes[latestHandled].round {
				latestHandled = i
			}
		} else if latestOpen < 0 || notes[i].round >= notes[latestOpen].round {
			latestOpen = i
		}
	}
	target := latestOpen
	if target < 0 {
		target = latestHandled
	}
	if target >= 0 {
		handled := !notes[target].handled
		if m.store != nil {
			found, err := m.store.SetReviewCommentHandled(m.diff.sessID, notes[target].id, handled)
			if err != nil {
				m.errBar.text = "updating review comment: " + err.Error()
				return
			}
			if !found {
				m.errBar.text = "review comment no longer exists"
				return
			}
		}
		notes[target].handled = handled
		m.diff.annotations[m.reviewKey()] = notes
	}
}

// sendAnnotations flattens every comment into one single-line prompt and
// delivers it into the session's pane, mirroring the quick-prompt path.
// The prompt must stay one line: an embedded newline would submit early.
func (m *Model) sendAnnotations() (tea.Model, tea.Cmd) {
	sess, ok := m.diffSession()
	if !ok {
		m.errBar.text = "session is gone"
		return m, nil
	}
	if m.isShell(sess.Tool) {
		m.errBar.text = shellPromptHint(sess.Name)
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	key := m.reviewKey()
	notes := append([]annotation(nil), m.diff.annotations[key]...)
	round := m.diff.rounds[key]
	nextRound := round.Number + 1
	var parts []string
	draftIndexes := make([]int, 0, len(notes))
	for i, note := range notes {
		if note.round != 0 {
			continue
		}
		draftIndexes = append(draftIndexes, i)
		if notes[i].id == "" {
			notes[i].id = newReviewCommentID()
		}
		notes[i].point = len(parts) + 1
		location := fmt.Sprintf("%s:%d", note.file, note.line)
		body := strings.ReplaceAll(note.text, "\n", " / ")
		if note.deleted {
			parts = append(parts, fmt.Sprintf("(%d) [comment %s] %s (deleted line): %s", len(parts)+1, notes[i].id, location, body))
		} else {
			parts = append(parts, fmt.Sprintf("(%d) [comment %s] %s (code: `%s`): %s", len(parts)+1, notes[i].id, location, note.excerpt, body))
		}
	}
	if len(parts) == 0 {
		m.errBar.text = "no comments to send - press c on a line first"
		return m, nil
	}
	prompt := fmt.Sprintf(
		"Code review of %s. Address each numbered point, then mark its comment handled with the review_comment tool (or `agent-manager review-comment <comment-id>`), and summarize what you changed per point: %s",
		scopePhrase(m.diff.scope), strings.Join(parts, "; "))
	if err := m.tmux.SendText(sess.ID, prompt); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	round.Number = nextRound
	round.Scope = m.diff.scope.String()
	round.Fingerprint = m.diff.fingerprint
	m.diff.rounds[key] = round
	for _, i := range draftIndexes {
		notes[i].round = round.Number
		notes[i].handled = false
		notes[i].outdated = false
	}
	m.diff.annotations[key] = notes
	count := len(draftIndexes)
	m.diff.notice = fmt.Sprintf("sent review round %d (%d %s) to %s",
		round.Number, count, commentNoun(count), sess.Name)
	if err := m.persistReviewState(); err != nil {
		m.diff.notice = ""
		m.errBar.text = "comments sent, but saving the review round failed: " + err.Error()
	} else if m.store != nil {
		if err := m.store.SetAcked(sess.ID, false); err != nil {
			m.diff.notice = ""
			m.errBar.text = "comments sent, but clearing the alert ack failed: " + err.Error()
		}
	}
	m.requestRefresh()
	return m, nil
}

// excerptOf caps a code excerpt at 60 runes, never splitting a rune, so
// multibyte lines stay valid UTF-8 in the prompt sent to the agent.
func excerptOf(text string) string {
	excerpt := strings.TrimSpace(text)
	if runes := []rune(excerpt); len(runes) > 60 {
		return string(runes[:60])
	}
	return excerpt
}

func commentNoun(count int) string {
	if count == 1 {
		return "comment"
	}
	return "comments"
}

func newReviewCommentID() string {
	return newID() + newID()
}

func scopePhrase(scope git.Scope) string {
	switch scope {
	case git.ScopeBranch:
		return "your branch changes vs target"
	case git.ScopeLastCommit:
		return "your last commit"
	case git.ScopeStaged:
		return "your staged changes"
	default:
		return "your uncommitted changes"
	}
}

// ---- rendering ----

const diffGutterSign = 2

// openDiff enters the full-screen review for the selected session,
// loading its diff. The whole review takes over the screen so the
// content scrolls freely instead of sharing the narrow sidebar.
func (m *Model) openDiff() tea.Cmd {
	if m.gitDrv == nil {
		m.errBar.text = "git not found in PATH, " + deps.Hint("git")
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.errBar.text = "select a session to diff"
		return nil
	}
	if m.diff.scrollByFile == nil {
		m.diff.scrollByFile = map[string]int{}
		m.diff.sideBySide = m.defaultSplitLayout()
	}
	if m.diff.reviewed == nil {
		m.diff.reviewed = map[string]map[string]uint64{}
	}
	if m.diff.annotations == nil {
		m.diff.annotations = map[string][]annotation{}
	}
	if m.diff.rounds == nil {
		m.diff.rounds = map[string]store.ReviewRound{}
	}
	if m.diff.stateLoaded == nil {
		m.diff.stateLoaded = map[string]bool{}
	}
	if m.diff.hl == nil {
		m.diff.hl = newHLCache()
	}
	m.diff.active = true
	m.mode = modeDiff
	m.errBar.text = ""
	// Default to returning to the list; the in-session Ctrl+R path sets this
	// afterward when review should return to the session instead.
	m.diff.reattachID = ""
	m.applyStoredScope(sess.ID)
	return tea.Batch(m.retargetDiff(sess), m.startStartupTick())
}
