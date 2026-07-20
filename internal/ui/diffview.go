package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// annotation is one line comment destined for the agent.
type annotation struct {
	file    string
	line    int // NewNum, or OldNum for pure deletions
	deleted bool
	excerpt string
	text    string
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

	scrollByFile map[string]int
	reviewed     map[string]map[string]bool
	annotations  map[string][]annotation

	annotating  bool
	annInput    textarea.Model
	sendConfirm bool
	notice      string

	fingerprint uint64
	probeTick   int
	hl          *hlCache
	hlPending   hlKey

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

type diffProbeMsg struct {
	sessID   string
	scope    git.Scope
	repoRoot string
	fp       uint64
}

// repoWant is matched by path so the selection survives ResolveRepos re-ranking between loads.
func (m *Model) diffLoadCmd(sess store.Session, scope git.Scope, gen int, repoWant string, refresh bool) tea.Cmd {
	driver := m.gitDrv
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, refresh: refresh}
		roots, err := driver.ResolveRepos(sess.Cwd)
		if err != nil {
			msg.err = err
			return msg
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
		msg.set, msg.err = diff.BuildSet(driver, roots[repoIdx], scope)
		if msg.err == nil {
			baseRef := ""
			if scope == git.ScopeBranch {
				baseRef, _, _ = driver.BaseRef(msg.set.Repo.Root)
			}
			msg.fp, _ = driver.Fingerprint(msg.set.Repo.Root, scope, baseRef)
			msg.worktrees, _ = driver.Worktrees(msg.set.Repo.Root)
		}
		return msg
	}
}

func (m *Model) diffHLCmd(fd diff.FileDiff, key hlKey) tea.Cmd {
	return func() tea.Msg {
		return diffHLMsg{key: key, hl: highlightFile(&fd)}
	}
}

func (m *Model) diffProbeCmd(sess store.Session, scope git.Scope) tea.Cmd {
	driver := m.gitDrv
	root := m.diff.set.Repo.Root
	return func() tea.Msg {
		baseRef := ""
		if scope == git.ScopeBranch {
			baseRef, _, _ = driver.BaseRef(root)
		}
		fp, err := driver.Fingerprint(root, scope, baseRef)
		if err != nil {
			return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: root, fp: 0}
		}
		return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: root, fp: fp}
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
	if picked, ok := m.pickedRepos[sess.ID]; ok {
		m.diff.repoSel = picked
	} else if declared, err := m.store.ReviewRepo(sess.ID); err != nil {
		m.err = err.Error()
	} else if declared != "" {
		m.diff.repoSel = declared
	}
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

func (m *Model) cycleDiffScope() tea.Cmd {
	if !m.diff.active {
		return nil
	}
	m.diff.scope = m.diff.scope.Next()
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	m.diff.gen++
	m.diff.loading = true
	m.diff.errText = ""
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

// reviewKey scopes a session's comments and reviewed marks to the repo under
// review, so switching repos never leaks marks between same-named files.
func (m *Model) reviewKey() string {
	return m.diff.sessID + "\x00" + m.diff.repoSel
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
		m.diff.repoRoots = nil
		m.diff.worktrees = nil
		return nil
	}
	m.diff.errText = ""
	m.diff.repoRoots = msg.repoRoots
	m.diff.repoSel = msg.repoRoot
	m.diff.worktrees = msg.worktrees
	if msg.missingRepo != "" {
		m.err = fmt.Sprintf("picked or declared repo %s is no longer under the session directory",
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
	// Re-anchor only on a silent same-scope refresh. A scope cycle or session
	// switch loads a different file set, where matching a comment by excerpt
	// would rewrite its line against content it was never made against.
	if msg.refresh {
		m.reanchorAnnotations()
	}
	// Keep the user's place across silent reloads.
	m.diff.fileIdx = 0
	for i, fd := range m.diff.set.Files {
		if fd.File.Path == previousPath {
			m.diff.fileIdx = i
			break
		}
	}
	// The restored file may have been past the eager-load cap in the fresh set.
	diff.EnsureFile(m.gitDrv, &m.diff.set, m.diff.fileIdx)
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
	if msg.fp == 0 || msg.fp == m.diff.fingerprint {
		return nil
	}
	sess, ok := m.diffSession()
	if !ok {
		return nil
	}
	m.diff.gen++
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
	if fd == nil || fd.Binary || fd.Err != nil || len(fd.Lines) == 0 {
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

func (m *Model) switchDiffFile(delta int) tea.Cmd {
	count := len(m.diff.set.Files)
	if count == 0 {
		return nil
	}
	if fd := m.currentFileDiff(); fd != nil {
		m.diff.scrollByFile[m.scrollKey(fd.File.Path)] = m.diff.scroll
	}
	m.diff.fileIdx = (m.diff.fileIdx + delta + count) % count
	diff.EnsureFile(m.gitDrv, &m.diff.set, m.diff.fileIdx)
	fd := m.currentFileDiff()
	m.diff.scroll = m.diff.scrollByFile[m.scrollKey(fd.File.Path)]
	m.diff.cursorLine = m.diff.scroll
	m.clampDiffCursor()
	return m.ensureHighlight()
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
// extra lines in narrow terminals.
func (m *Model) diffCodeHeight() int {
	height := m.height - 6 - lipgloss.Height(m.viewDiffFooter())
	if m.diff.annotating {
		height -= m.diffAnnBarRows() + 1
	}
	if height < 3 {
		height = 3
	}
	return height
}

func (m *Model) diffAnnBarRows() int {
	return 2
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
		default:
			m.diff.sendConfirm = false
		}
		return m, nil
	}
	height := m.diffCodeHeight()
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "q", "esc":
		m.mode = modeList
		m.diff.active = false
		// Review opened from inside a session returns to that session, not
		// the list, so Ctrl+R then esc is a round trip back to where it began.
		if id := m.diff.reattachID; id != "" {
			m.diff.reattachID = ""
			if cmd := m.reattach(id); cmd != nil {
				return m, cmd
			}
		}
	case "up", "k":
		m.moveDiffCursor(-1, height)
	case "down", "j":
		m.moveDiffCursor(1, height)
	case "ctrl+d":
		m.moveDiffCursor(height/2, height)
	case "ctrl+u":
		m.moveDiffCursor(-height/2, height)
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
	case "N":
		m.jumpChange(-1)
	case "s":
		return m, m.cycleDiffScope()
	case "r":
		m.openRepoPick()
	case "b":
		return m, m.openBranchPick()
	case "u":
		lineIdx := m.cursorDiffLine()
		m.diff.sideBySide = !m.diff.sideBySide
		m.setCursorDiffLine(lineIdx)
	case " ", "space":
		return m, m.toggleReviewed()
	case "c":
		m.openAnnotate()
	case "d":
		m.removeAnnotation()
	case "C":
		if len(m.diff.annotations[m.reviewKey()]) == 0 {
			m.err = "no comments to send - press c on a line first"
		} else {
			m.diff.sendConfirm = true
		}
	}
	return m, nil
}

func (m *Model) toggleReviewed() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	marks := m.diff.reviewed[m.reviewKey()]
	if marks == nil {
		marks = map[string]bool{}
		m.diff.reviewed[m.reviewKey()] = marks
	}
	marks[fd.File.Path] = !marks[fd.File.Path]
	if !marks[fd.File.Path] {
		return nil
	}
	// Advance to the next unreviewed file, review-queue style. The switch
	// returns the highlight command for the newly shown file; dropping it
	// would leave that file unhighlighted.
	for step := 1; step < len(m.diff.set.Files); step++ {
		next := (m.diff.fileIdx + step) % len(m.diff.set.Files)
		if !marks[m.diff.set.Files[next].File.Path] {
			return m.switchDiffFile(next - m.diff.fileIdx)
		}
	}
	return nil
}

func (m *Model) fileReviewed(path string) bool {
	return m.diff.reviewed[m.reviewKey()][path]
}

func (m *Model) openAnnotate() {
	fd := m.currentFileDiff()
	if fd == nil {
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

// annotationRows renders the comment attached to line lineIdx, if any, as
// its own indented full-width rows beneath the code line. Shared by the
// unified and side-by-side layouts so both surface saved comments.
func (m *Model) annotationRows(fd *diff.FileDiff, lineIdx, width int) []string {
	note := m.annotationAt(fd.File.Path, fd.Lines[lineIdx])
	if note == nil {
		return nil
	}
	comment := mutedStyle.Italic(true).Render("  ¶ " + note.text)
	return wrapTinted(comment, nil, "", "", width)
}

func (m *Model) annotationAt(path string, line diff.Line) *annotation {
	num, deleted := annotationLine(line)
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted {
			return &notes[i]
		}
	}
	return nil
}

// reanchorAnnotations re-points saved comments at the line that still
// carries their excerpt after a reload shifted line numbers (the agent
// edits while the user reviews), choosing the nearest match. A comment
// whose line vanished entirely keeps its number as the best guess.
func (m *Model) reanchorAnnotations() {
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		note := &notes[i]
		// A blank line's excerpt is empty and would match every blank line;
		// only a distinctive excerpt can re-anchor.
		if note.excerpt == "" {
			continue
		}
		fd := m.fileDiffByPath(note.file)
		if fd == nil {
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
			note.line = target
		}
	}
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
		return
	}
	if text == "" {
		return
	}
	m.diff.annotations[m.reviewKey()] = append(m.diff.annotations[m.reviewKey()], annotation{
		file:    fd.File.Path,
		line:    num,
		deleted: deleted,
		excerpt: excerptOf(line.Text),
		text:    text,
	})
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
		if notes[i].file == fd.File.Path && notes[i].line == num && notes[i].deleted == deleted {
			m.diff.annotations[m.reviewKey()] = append(notes[:i], notes[i+1:]...)
			return
		}
	}
}

// sendAnnotations flattens every comment into one single-line prompt and
// delivers it into the session's pane, mirroring the quick-prompt path.
// The prompt must stay one line: an embedded newline would submit early.
func (m *Model) sendAnnotations() (tea.Model, tea.Cmd) {
	sess, ok := m.diffSession()
	if !ok {
		m.err = "session is gone"
		return m, nil
	}
	if !m.tmux.Exists(sess.ID) {
		m.err = "session is dead - press v to revive"
		return m, nil
	}
	notes := m.diff.annotations[m.reviewKey()]
	var parts []string
	for i, note := range notes {
		location := fmt.Sprintf("%s:%d", note.file, note.line)
		body := strings.ReplaceAll(note.text, "\n", " / ")
		if note.deleted {
			parts = append(parts, fmt.Sprintf("(%d) %s (deleted line) — %s", i+1, location, body))
		} else {
			parts = append(parts, fmt.Sprintf("(%d) %s (code: `%s`) — %s", i+1, location, note.excerpt, body))
		}
	}
	prompt := fmt.Sprintf(
		"Code review of %s — address each numbered point, then summarize what you changed per point: %s",
		scopePhrase(m.diff.scope), strings.Join(parts, "; "))
	if err := m.tmux.SendText(sess.ID, prompt); err != nil {
		m.err = err.Error()
		return m, nil
	}
	count := len(notes)
	delete(m.diff.annotations, m.reviewKey())
	m.diff.notice = fmt.Sprintf("sent %d review %s to %s", count, commentNoun(count), sess.Name)
	if err := m.store.SetAcked(sess.ID, false); err != nil {
		m.diff.notice = ""
		m.err = "comments sent, but clearing the alert ack failed: " + err.Error()
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

func scopePhrase(scope git.Scope) string {
	switch scope {
	case git.ScopeBranch:
		return "your branch changes vs base"
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

func (m *Model) diffEmptyText() string {
	if m.diff.loading && len(m.diff.set.Files) == 0 {
		return mutedStyle.Render("(loading diff…)")
	}
	if m.diff.errText != "" {
		return errStyle.Render("✖ " + m.diff.errText)
	}
	if m.diff.sessID == "" {
		return mutedStyle.Render("(select a session to diff)")
	}
	if len(m.diff.set.Files) == 0 {
		return mutedStyle.Render(fmt.Sprintf("✓ no changes (%s)", m.diff.scope)) + "\n" +
			subtleStyle.Render("s cycles scope")
	}
	return ""
}

func (m *Model) diffBodyNote(fd *diff.FileDiff) string {
	switch {
	case fd.Err != nil:
		return errStyle.Render("✖ " + fd.Err.Error())
	case fd.Binary:
		return mutedStyle.Render("(binary file)")
	case fd.Truncated && len(fd.Lines) == 0:
		return mutedStyle.Render("(file too large to diff)")
	case len(fd.Lines) == 0:
		return mutedStyle.Render("(empty file)")
	}
	return ""
}

// renderDiffRow renders one whole-file diff line into one or more visual
// rows: line numbers, change sign, syntax-highlighted text with the diff
// background tinted through, long lines wrapped with the gutter blanked
// on continuation rows.
func (m *Model) renderDiffRow(fd *diff.FileDiff, hl *fileHL, index, width int, cursor bool) []string {
	line := fd.Lines[index]
	gutterWidth := numWidth(fd)
	gutter := numCell(line.OldNum, gutterWidth) + numCell(line.NewNum, gutterWidth)

	sign, baseBg, spanBg := " ", "", ""
	switch line.Kind {
	case diff.Add:
		sign, baseBg, spanBg = "+", bgAdd, bgAddSpan
	case diff.Del:
		sign, baseBg, spanBg = "−", bgDel, bgDelSpan
	}

	textWidth := width - ansi.StringWidth(gutter) - diffGutterSign
	if textWidth < 4 {
		textWidth = 4
	}
	textRows := wrapTinted(hl.hlLine(line), line.Spans, baseBg, spanBg, textWidth)

	marker := " "
	if m.annotationAt(fd.File.Path, line) != nil {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("¶")
	}
	signCell := sign
	switch line.Kind {
	case diff.Add:
		signCell = lipgloss.NewStyle().Foreground(colorFinished).Render(sign)
	case diff.Del:
		signCell = lipgloss.NewStyle().Foreground(colorErrored).Render(sign)
	}
	blankGutter := strings.Repeat(" ", ansi.StringWidth(gutter))

	out := make([]string, len(textRows))
	for i, text := range textRows {
		prefix := subtleStyle.Render(blankGutter) + "  "
		if i == 0 {
			prefix = subtleStyle.Render(gutter) + marker + signCell
		}
		row := padRight(prefix+text, width)
		if cursor {
			row = selectedRowStyle.Render(row)
		}
		out[i] = row
	}
	return out
}

// unifiedRows fills the code viewport with wrapped whole-file rows,
// starting at the scroll line and reserving rows for the overflow
// indicators. A logical line can span several visual rows; annotation
// comments render on their own indented rows beneath the marked line.
func (m *Model) unifiedRows(fd *diff.FileDiff, hl *fileHL, width, height int) []string {
	total := len(fd.Lines)
	scroll := m.diff.scroll
	if scroll > total-1 {
		scroll = total - 1
	}
	if scroll < 0 {
		scroll = 0
	}

	var rows []string
	if scroll > 0 {
		rows = append(rows, subtleStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < total && len(rows) < height; i++ {
		rows = append(rows, m.renderDiffRow(fd, hl, i, width, i == m.diff.cursorLine)...)
		rows = append(rows, m.annotationRows(fd, i, width)...)
	}
	if i < total {
		if len(rows) >= height {
			rows = rows[:height-1]
		}
		rows = append(rows, subtleStyle.Render(fmt.Sprintf("  ↓ %d more", total-i)))
	} else if len(rows) > height {
		rows = rows[:height]
	}
	return rows
}

func numWidth(fd *diff.FileDiff) int {
	largest := fd.NewTotal
	if fd.OldTotal > largest {
		largest = fd.OldTotal
	}
	width := len(fmt.Sprintf("%d", largest))
	if width < 3 {
		width = 3
	}
	return width + 1
}

func numCell(num, width int) string {
	if num == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*d ", width-1, num)
}

// viewDiffFull is the full-screen review mode: file list on the left,
// whole-file code on the right, annotation bar docked when typing.
func (m *Model) viewDiffFull() string {
	sess, _ := m.diffSession()
	footer := m.viewDiffFooter()
	bodyHeight := m.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	fileWidth := m.width * 24 / 100
	if fileWidth < 28 {
		fileWidth = 28
	}
	codeWidth := m.width - fileWidth

	files := titledPanel("Files", m.viewDiffFileList(fileWidth-4, bodyHeight-2), fileWidth, bodyHeight)
	code := titledPanel(m.diffCodeTitle(), m.viewDiffCode(codeWidth-4, bodyHeight-2), codeWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, files, code)

	header := m.viewDiffHeader(sess.Name)
	status := m.viewDiffStatus()
	return strings.Join([]string{header, "", body, status, footer}, "\n")
}

func (m *Model) viewDiffHeader(sessName string) string {
	layout := "unified"
	if m.diff.sideBySide {
		layout = "split"
	}
	left := badgeStyle.Render("◆ Review · "+sessName) + "  " +
		pill(m.diff.scope.String(), colorAccent2) + "  " + pill(layout, colorAccent)
	if len(m.diff.repoRoots) > 1 {
		name := filepath.Base(m.diff.repoSel)
		left += "  " + pill(fmt.Sprintf("%s · %d repos", name, len(m.diff.repoRoots)), colorAccent)
	}
	if m.diff.set.BaseDesc != "" && m.diff.scope == git.ScopeBranch {
		left += "  " + subtleStyle.Render(m.diff.set.BaseDesc)
	}

	adds, dels := 0, 0
	uncounted := false
	for _, fd := range m.diff.set.Files {
		if !fd.StatKnown() {
			uncounted = true
			continue
		}
		adds += fd.Stat.Adds
		dels += fd.Stat.Dels
	}
	right := mutedStyle.Render(fmt.Sprintf("%d files", len(m.diff.set.Files))) + subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", adds)) + " " +
		lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", dels))
	if uncounted {
		right += " " + mutedStyle.Render("+?")
	}
	if count := len(m.diff.annotations[m.reviewKey()]); count > 0 {
		right += subtleStyle.Render(" · ") + lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("¶%d", count))
	}
	right += " "

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return padRight(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) diffCodeTitle() string {
	fd := m.currentFileDiff()
	if fd == nil {
		return "Diff"
	}
	return fd.File.Path
}

func (m *Model) viewDiffFileList(width, height int) string {
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	files := m.diff.set.Files
	start, end := scrollWindow(len(files), m.diff.fileIdx, height)
	var b strings.Builder
	if start > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	notes := map[string]int{}
	for _, note := range m.diff.annotations[m.reviewKey()] {
		notes[note.file]++
	}
	for i := start; i < end; i++ {
		fd := files[i]
		glyph := subtleStyle.Render("○")
		if m.fileReviewed(fd.File.Path) {
			glyph = lipgloss.NewStyle().Foreground(colorFinished).Render("✔")
		}
		bar := " "
		if i == m.diff.fileIdx {
			bar = lipgloss.NewStyle().Foreground(colorAccent).Render("▎")
		}
		counts := lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", fd.Stat.Adds)) +
			" " + lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", fd.Stat.Dels))
		if !fd.StatKnown() {
			counts = mutedStyle.Render("?")
		}
		if fd.Binary || fd.Stat.Binary {
			counts = mutedStyle.Render("binary")
		}
		if count := notes[fd.File.Path]; count > 0 {
			counts = lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("¶%d ", count)) + counts
		}
		name := truncateTail(fd.File.Path, width-ansi.StringWidth(counts)-6)
		left := bar + glyph + " " + valueStyle.Render(name)
		gap := width - ansi.StringWidth(left) - ansi.StringWidth(counts)
		if gap < 1 {
			gap = 1
		}
		row := left + strings.Repeat(" ", gap) + counts
		if i == m.diff.fileIdx {
			row = selectedRowStyle.Render(padRight(row, width))
		}
		b.WriteString(row + "\n")
	}
	if end < len(files) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(files)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewDiffCode(width, height int) string {
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	fd := m.currentFileDiff()
	if body := m.diffBodyNote(fd); body != "" {
		return body
	}

	var bar string
	if m.diff.annotating {
		fdLine := fd.Lines[m.cursorDiffLine()]
		num, _ := annotationLine(fdLine)
		m.diff.annInput.SetWidth(width)
		m.diff.annInput.SetHeight(1)
		bar = divider(fmt.Sprintf("Comment · %s:%d", fd.File.Path, num), width) + "\n" + m.diff.annInput.View()
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}

	hl := m.currentHL()
	var b strings.Builder
	if m.diff.sideBySide {
		m.renderSideBySide(&b, fd, hl, width, height)
	} else {
		b.WriteString(strings.Join(m.unifiedRows(fd, hl, width, height), "\n"))
	}

	body := strings.TrimRight(b.String(), "\n")
	if bar != "" {
		return padToHeight(body, height) + "\n" + bar
	}
	return body
}

func (m *Model) renderSideBySide(b *strings.Builder, fd *diff.FileDiff, hl *fileHL, width, height int) {
	rows := fd.SideBySideRows()
	half := (width - 1) / 2
	sep := subtleStyle.Render("│")

	scroll := m.diff.scroll
	if scroll > len(rows)-1 {
		scroll = len(rows) - 1
	}
	if scroll < 0 {
		scroll = 0
	}
	var out []string
	if scroll > 0 {
		out = append(out, subtleStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < len(rows) && len(out) < height; i++ {
		row := rows[i]
		left := m.renderSideCell(fd, hl, row.Left, half, true)
		right := m.renderSideCell(fd, hl, row.Right, width-half-1, false)
		// A wrapped cell can be taller than its partner; pad the shorter
		// side with blank cells so the columns stay aligned.
		lines := len(left)
		if len(right) > lines {
			lines = len(right)
		}
		for r := 0; r < lines; r++ {
			leftCell, rightCell := padRight("", half), padRight("", width-half-1)
			if r < len(left) {
				leftCell = left[r]
			}
			if r < len(right) {
				rightCell = right[r]
			}
			line := leftCell + sep + rightCell
			if i == m.diff.cursorLine {
				line = selectedRowStyle.Render(padRight(line, width))
			}
			out = append(out, line)
		}
		if row.Left >= 0 {
			out = append(out, m.annotationRows(fd, row.Left, width)...)
		}
		if row.Right >= 0 && row.Right != row.Left {
			out = append(out, m.annotationRows(fd, row.Right, width)...)
		}
	}
	if i < len(rows) {
		if len(out) >= height {
			out = out[:height-1]
		}
		out = append(out, subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-i)))
	} else if len(out) > height {
		out = out[:height]
	}
	b.WriteString(strings.Join(out, "\n"))
}

// renderSideCell renders one half of a side-by-side row into wrapped
// visual rows; -1 renders a single dim filler for an unpaired line.
func (m *Model) renderSideCell(fd *diff.FileDiff, hl *fileHL, index, width int, leftSide bool) []string {
	if index < 0 {
		return []string{padRight(subtleStyle.Render(" ·"), width)}
	}
	line := fd.Lines[index]
	// The left column carries old-side content: skip adds there.
	if leftSide && line.Kind == diff.Add {
		return []string{padRight("", width)}
	}
	gutterWidth := numWidth(fd)
	num := line.NewNum
	if leftSide {
		num = line.OldNum
	}
	gutter := numCell(num, gutterWidth)

	baseBg, spanBg := "", ""
	switch line.Kind {
	case diff.Add:
		baseBg, spanBg = bgAdd, bgAddSpan
	case diff.Del:
		baseBg, spanBg = bgDel, bgDelSpan
	}

	textWidth := width - gutterWidth
	if textWidth < 4 {
		textWidth = 4
	}
	textRows := wrapTinted(hl.hlLine(line), line.Spans, baseBg, spanBg, textWidth)
	blankGutter := strings.Repeat(" ", gutterWidth)
	out := make([]string, len(textRows))
	for i, text := range textRows {
		g := blankGutter
		if i == 0 {
			g = gutter
		}
		out[i] = padRight(subtleStyle.Render(g)+text, width)
	}
	return out
}

func (m *Model) viewDiffStatus() string {
	if m.err != "" {
		return padRight(errStyle.Render(" ✖ "+m.err), m.width)
	}
	if m.diff.notice != "" {
		return padRight(lipgloss.NewStyle().Foreground(colorFinished).Render(" ✔ "+m.diff.notice), m.width)
	}
	if m.diff.sendConfirm {
		count := len(m.diff.annotations[m.reviewKey()])
		return padRight(errStyle.Render(fmt.Sprintf(" ¶ send %d %s to the agent?", count, commentNoun(count)))+
			subtleStyle.Render("  ↵/y send · esc cancel"), m.width)
	}
	return ""
}

func (m *Model) viewDiffFooter() string {
	if m.diff.annotating {
		return footerLine([][2]string{{"↵", "save"}, {"esc", "cancel"}}, m.width)
	}
	pairs := [][2]string{
		{"↑↓", "scroll"}, {"J/K", "file"}, {"n/N", "change"}, {"space", "reviewed"},
		{"u", "layout"}, {"s", "scope: " + m.diff.scope.String()}, {"c", "comment"},
	}
	if len(m.diff.repoRoots) > 1 {
		pairs = append(pairs, [2]string{"r", "repo: " + filepath.Base(m.diff.repoSel)})
	}
	if len(m.diff.worktrees) > 1 {
		pairs = append(pairs, [2]string{"b", "branch"})
	}
	if count := len(m.diff.annotations[m.reviewKey()]); count > 0 {
		pairs = append(pairs, [2]string{"C", fmt.Sprintf("send %d", count)}, [2]string{"d", "remove"})
	}
	pairs = append(pairs, [2]string{"esc", "close"})
	return footerLine(pairs, m.width)
}
