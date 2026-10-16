package ui

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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

// commitPin is the commit the commit scope shows; an empty sha follows HEAD.
// index is where the pinned commit stood in the list, the place to fall back
// to once a rewrite leaves nothing to match it by. unreviewed asks the next
// load for the oldest commit not fully reviewed, falling back to HEAD.
type commitPin struct {
	sha        string
	index      int
	unreviewed bool
}

// commitLoad is what a load takes to pick the commit scope's commit: the
// pin, the commits review state is keyed by so a rewrite can carry it, the
// HEAD that state was last carried at, and the reviewed marks and filter
// that decide which commits are done.
type commitLoad struct {
	pin      commitPin
	keyed    []string
	head     string
	marks    map[string]uint64
	codeOnly bool
}

// commitScopePrefix keys review state taken in the commit scope by the
// commit it was taken on, since the same path renders differently in each.
const commitScopePrefix = "commit@"

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

	// commits lists what the commit scope steps through, newest first, since
	// the target commitTarget names. rewriteHead is the HEAD review state was
	// last carried across rewrites at.
	commitPin    commitPin
	commits      []git.Commit
	commitTarget string
	rewriteHead  string

	// markAll is set while A waits on file loads to mark every shown file
	// reviewed. markOnLoad asks the next load for the same, for a click on
	// another commit's glyph; markFileOnLoad names a file whose glyph was
	// clicked before it had loaded.
	markAll        bool
	markOnLoad     bool
	markFileOnLoad string

	// Where the last frame painted what a click acts on: the header pills,
	// and the rail rows naming a commit or a file, keyed by screen row.
	pills      []pillHit
	railWidth  int
	commitRows map[int]int
	fileRows   map[int]int
	// codeRows names the cursor row each painted code row belongs to, from
	// the first row under the code title; lineClickAt and lineClickRow
	// pair two presses on one line into a double click.
	codeRows     []int
	lineClickAt  time.Time
	lineClickRow int
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
	// The commit scope's list and the commit it resolved, and what a rewrite
	// since the last load moved: rewrites maps old SHAs to their replacements.
	commits      []git.Commit
	commitTarget string
	head         string
	rewrites     map[string]string
	rewriteErr   error
	commitNotice string
	// pinned reports the resolved commit pins the scope rather than
	// following HEAD.
	pinned bool
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

// repoWant is matched by path so the selection survives ResolveRepos re-ranking between loads.
func (m *Model) diffLoadCmd(sess store.Session, scope git.Scope, gen int, repoWant string, refresh bool) tea.Cmd {
	driver := m.gitDrv
	stor := m.store
	groupBase := m.groupBase(sess.Group)
	// Restoring happens once per repo, so a reload that would only have its
	// state discarded reads nothing and cannot migrate over a chained write.
	restored := maps.Clone(m.diff.stateLoaded)
	cwd := m.sessionDir(sess)
	load := m.commitLoad()
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, refresh: refresh}
		roots, err := driver.ResolveRepos(cwd)
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
		if !restored[sess.ID+"\x00"+roots[repoIdx]] {
			msg.reviewState, msg.reviewStateErr = readReviewState(stor, sess.ID, roots[repoIdx])
			msg.reviewStateLoaded = msg.reviewStateErr == nil
		}
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(roots[repoIdx]))
		if err != nil {
			msg.err = err
			return msg
		}
		finishDiffMsg(driver, scope, gen, roots[repoIdx], cmp.Or(override, groupBase), roots, load, &msg)
		return msg
	}
}

func finishDiffMsg(driver *git.Driver, scope git.Scope, gen int, gitRoot, override string, repoRoots []string, load commitLoad, msg *diffLoadedMsg) {
	msg.repoRoots = repoRoots
	msg.repoRoot = gitRoot
	commit := ""
	if scope == git.ScopeCommit {
		if commit, msg.err = chooseCommit(driver, gitRoot, override, load, msg); msg.err != nil {
			return
		}
	}
	msg.set, msg.err = diff.BuildSet(driver, gitRoot, scope, override, commit)
	if msg.err == nil {
		ref := msg.set.BaseRef
		switch {
		case scope == git.ScopeCommit:
			ref = msg.set.Commit
		case scope == git.ScopeBranch && ref == "":
			ref, _, _ = driver.BranchBase(msg.set.Repo.Root, override)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			msg.fp, _ = driver.Fingerprint(msg.set.Repo.Root, scope, ref)
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
	load := m.commitLoad()
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen}
		finishDiffMsg(driver, scope, gen, gitRoot, override, repoRoots, load, &msg)
		return msg
	}
}

// diffRebaseCmd reloads the way diffReloadCmd does, but reads the stored
// base itself: the single store connection can wait behind the poller, and
// Update is not the place to wait for it.
func (m *Model) diffRebaseCmd(sess store.Session, scope git.Scope, gen int, gitRoot string, repoRoots []string) tea.Cmd {
	driver, stor := m.gitDrv, m.store
	groupBase := m.groupBase(sess.Group)
	load := m.commitLoad()
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, repoRoots: repoRoots, repoRoot: gitRoot}
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(gitRoot))
		if err != nil {
			msg.err = err
			return msg
		}
		finishDiffMsg(driver, scope, gen, gitRoot, cmp.Or(override, groupBase), repoRoots, load, &msg)
		return msg
	}
}

// chooseCommit lists the commit scope's commits into msg and returns the one
// to show: the pinned commit, its replacement after a rewrite, or the commit
// now at its place in the list. "" follows HEAD. Review state keyed by a
// commit HEAD no longer reaches is mapped to its replacement in msg.rewrites.
func chooseCommit(driver *git.Driver, root, override string, load commitLoad, msg *diffLoadedMsg) (string, error) {
	commits, target, err := driver.Commits(root, override)
	if err != nil {
		return "", err
	}
	msg.commits, msg.commitTarget = commits, target
	if len(commits) == 0 {
		return "", nil
	}
	msg.head = commits[0].SHA
	if msg.head != load.head {
		keyed := load.keyed
		if msg.reviewStateLoaded {
			keyed = append(keyed, keyedCommits(msg.reviewState)...)
		}
		if load.pin.sha != "" {
			keyed = append(keyed, load.pin.sha)
		}
		slices.Sort(keyed)
		msg.rewrites, msg.rewriteErr = driver.Rewrites(root, slices.Compact(keyed), commits)
	}
	if load.pin.unreviewed {
		marks := map[string]uint64{}
		maps.Copy(marks, load.marks)
		if msg.reviewStateLoaded {
			maps.Copy(marks, msg.reviewState.Reviewed)
		}
		marks = carriedMarks(marks, msg.rewrites)
		for i := len(commits) - 1; i > 0; i-- {
			if !commitReviewed(commits[i], marks, load.codeOnly) {
				msg.pinned = true
				return commits[i].SHA, nil
			}
		}
		return "", nil
	}
	sha := load.pin.sha
	if sha == "" {
		return "", nil
	}
	msg.pinned = true
	if commitIndex(commits, sha) >= 0 {
		return sha, nil
	}
	if moved, ok := msg.rewrites[sha]; ok {
		msg.commitNotice = fmt.Sprintf("commit %s was rewritten as %s", shortSHA(sha), shortSHA(moved))
		return moved, nil
	}
	at := commits[min(load.pin.index, len(commits)-1)]
	msg.commitNotice = fmt.Sprintf("commit %s is no longer on the branch, showing %s", shortSHA(sha), at.Short)
	return at.SHA, nil
}

// commitReviewed reports whether every path c changes carries a reviewed
// mark taken on c. The code-only filter leaves out the paths it hides, and a
// commit changing nothing has nothing left to review.
func commitReviewed(c git.Commit, marks map[string]uint64, codeOnly bool) bool {
	for _, path := range c.Files {
		if codeOnly && nonCodePath(path) {
			continue
		}
		if marks[commitScopePrefix+c.SHA+"\x00"+path] == 0 {
			return false
		}
	}
	return true
}

func (m *Model) commitIsReviewed(c git.Commit) bool {
	return commitReviewed(c, m.diff.reviewed[m.reviewKey()], m.diff.codeOnly)
}

// carriedScope is scope with the commit it names replaced by the one a
// rewrite moved it to.
func carriedScope(scope string, moved map[string]string) (string, bool) {
	sha, ok := strings.CutPrefix(scope, commitScopePrefix)
	if to, found := moved[sha]; ok && found {
		return commitScopePrefix + to, true
	}
	return scope, false
}

// carriedMarks rekeys marks onto the commits a rewrite moved them to. A mark
// already on the replacement wins over one carried onto it.
func carriedMarks(marks map[string]uint64, moved map[string]string) map[string]uint64 {
	carried := make(map[string]uint64, len(marks))
	for markKey, hash := range marks {
		scope, path, _ := strings.Cut(markKey, "\x00")
		if to, ok := carriedScope(scope, moved); ok {
			if _, taken := marks[to+"\x00"+path]; !taken {
				carried[to+"\x00"+path] = hash
			}
			continue
		}
		carried[markKey] = hash
	}
	return carried
}

func (m *Model) commitLoad() commitLoad {
	state := m.reviewStateSnapshot(m.reviewKey())
	return commitLoad{
		pin:      m.diff.commitPin,
		keyed:    keyedCommits(state),
		head:     m.diff.rewriteHead,
		marks:    state.Reviewed,
		codeOnly: m.diff.codeOnly,
	}
}

// keyedCommits lists the commits review state was taken on.
func keyedCommits(state store.ReviewState) []string {
	var shas []string
	add := func(scope string) {
		if sha, ok := strings.CutPrefix(scope, commitScopePrefix); ok && sha != "" {
			shas = append(shas, sha)
		}
	}
	for markKey := range state.Reviewed {
		scope, _, _ := strings.Cut(markKey, "\x00")
		add(scope)
	}
	for _, note := range state.Comments {
		add(note.Scope)
	}
	add(state.Round.Scope)
	return shas
}

func commitIndex(commits []git.Commit, sha string) int {
	return slices.IndexFunc(commits, func(c git.Commit) bool { return c.SHA == sha })
}

// shortSHA abbreviates a commit for text an agent reads, long enough to stay
// unambiguous where git's own short form is not at hand.
func shortSHA(sha string) string {
	return sha[:min(len(sha), 12)]
}

// carryRewrites moves review state taken on commits a rebase or amend
// replaced onto their replacements. The reload's content hashes then judge
// whether each mark and comment still holds there.
func (m *Model) carryRewrites(moved map[string]string) bool {
	if len(moved) == 0 {
		return false
	}
	key := m.reviewKey()
	changed := false
	if marks := m.diff.reviewed[key]; marks != nil {
		carried := carriedMarks(marks, moved)
		changed = !maps.Equal(carried, marks)
		m.diff.reviewed[key] = carried
	}
	notes := m.diff.annotations[key]
	for i := range notes {
		if to, ok := carriedScope(notes[i].scope, moved); ok {
			notes[i].scope = to
			changed = true
		}
	}
	if round, ok := m.diff.rounds[key]; ok {
		if to, carried := carriedScope(round.Scope, moved); carried {
			round.Scope = to
			m.diff.rounds[key] = round
			changed = true
		}
	}
	return changed
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
	groupBase := m.groupBase(sess.Group)
	commit := m.diff.set.Commit
	return func() tea.Msg {
		// Resolve symlinks so the key matches both the CLI writer and the load
		// closure, keeping probe and load fingerprints identical.
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(repoSel))
		if err != nil {
			return diffProbeMsg{sessID: sess.ID, scope: scope, repoRoot: repoSel, fp: 0}
		}
		ref := ""
		switch scope {
		case git.ScopeBranch:
			ref, _, _ = driver.BranchBase(gitRoot, cmp.Or(override, groupBase))
		case git.ScopeCommit:
			ref = commit
		}
		fp, err := driver.Fingerprint(gitRoot, scope, ref)
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
	case "last_commit", "commit":
		// An agent asking for the commit scope means the one it just made.
		m.diff.scope = git.ScopeCommit
		m.diff.commitPin = commitPin{}
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
	m.diff.scope = m.diff.scope.Next()
	if m.diff.scope == git.ScopeCommit {
		m.diff.commitPin = commitPin{unreviewed: true}
	}
	return m.reloadDiffScope(sess)
}

// reloadDiffScope loads the diff afresh after the scope or the commit in
// view changed, dropping the place kept in the old one.
func (m *Model) reloadDiffScope(sess store.Session) tea.Cmd {
	m.diff.gen++
	m.diff.loading = true
	m.diff.markOnLoad = false
	m.diff.errText = ""
	// The repo stays: the reload reads the same one, and the header's repo,
	// target and commit pills hold still instead of blinking out with the
	// files.
	m.diff.set = diff.Set{Repo: m.diff.set.Repo, BaseOverride: m.diff.set.BaseOverride}
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	m.diff.fileLoading = nil
	m.diff.reanchor = nil
	if m.diff.repoSel != "" && len(m.diff.repoRoots) > 0 {
		return tea.Batch(m.diffRebaseCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, m.diff.repoRoots), m.startStartupTick())
	}
	return tea.Batch(m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false), m.startStartupTick())
}

// handleDiffClick acts on a left press in review: a header pill presses its
// key, and a rail row shows its commit or file.
func (m *Model) handleDiffClick(x, y int) (tea.Model, tea.Cmd) {
	if m.diff.annotating || m.diff.sendConfirm {
		return m, nil
	}
	if y == 0 {
		for _, pill := range m.diff.pills {
			if x >= pill.x0 && x < pill.x1 {
				return m.handleDiffKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(pill.key)})
			}
		}
		return m, nil
	}
	if x >= m.diff.railWidth+diffPaneSeam {
		return m.clickDiffLine(y - diffBodyTop - 2)
	}
	if x >= m.diff.railWidth {
		return m, nil
	}
	glyph := x == railGlyphX || x == railGlyphX+1
	if i, ok := m.diff.commitRows[y]; ok {
		current := i == commitIndex(m.diff.commits, m.viewedCommit())
		switch {
		case glyph && current:
			return m, m.markAllReviewed()
		case current:
			return m, nil
		}
		cmd := m.showCommit(i)
		m.diff.markOnLoad = glyph
		return m, cmd
	}
	if i, ok := m.diff.fileRows[y]; ok {
		if glyph {
			return m, m.clickFileGlyph(i)
		}
		if i != m.diff.fileIdx {
			return m, m.switchDiffFile(i - m.diff.fileIdx)
		}
	}
	return m, nil
}

// railGlyphX is the screen column of a rail row's reviewed glyph: the rail's
// edge cell, its inset, then the cursor bar.
const railGlyphX = 1 + railInset + 1

// clickDiffLine moves the cursor to the code row a press landed on, and a
// second press on the same line inside the double-click window opens a
// comment there.
func (m *Model) clickDiffLine(row int) (tea.Model, tea.Cmd) {
	if row < 0 || row >= len(m.diff.codeRows) {
		return m, nil
	}
	target := m.diff.codeRows[row]
	double := !m.diff.lineClickAt.IsZero() && m.diff.lineClickRow == target && time.Since(m.diff.lineClickAt) < multiClickWindow
	m.diff.lineClickAt, m.diff.lineClickRow = time.Now(), target
	m.diff.cursorLine = target
	m.clampDiffCursor()
	if double {
		m.diff.lineClickAt = time.Time{}
		m.openAnnotate()
	}
	return m, nil
}

// resetCommitScope forgets the commit scope's commit and list, for a review
// moving to another session or repo, which starts on its oldest commit not
// fully reviewed.
func (m *Model) resetCommitScope() {
	m.diff.commitPin = commitPin{unreviewed: true}
	m.diff.commits = nil
	m.diff.commitTarget = ""
	m.diff.rewriteHead = ""
}

// stepCommit moves the commit scope delta commits older, or newer when
// negative. Reaching the newest follows HEAD again.
func (m *Model) stepCommit(delta int) tea.Cmd {
	if !m.diff.active || m.diff.scope != git.ScopeCommit || len(m.diff.commits) == 0 {
		return nil
	}
	at := max(commitIndex(m.diff.commits, m.viewedCommit()), 0)
	next := min(max(at+delta, 0), len(m.diff.commits)-1)
	if next == at {
		m.diff.notice = m.commitEndNotice(delta < 0)
		return nil
	}
	return m.showCommit(next)
}

// commitEndNotice answers a step past either end of the commit list, saying
// what is left to review there.
func (m *Model) commitEndNotice(newest bool) string {
	left := 0
	for _, c := range m.diff.commits {
		if !m.commitIsReviewed(c) {
			left++
		}
	}
	total := len(m.diff.commits)
	switch {
	case left == 0:
		return fmt.Sprintf("all %d %s reviewed", total, commitNoun(total))
	case newest:
		return fmt.Sprintf("HEAD is the newest commit, %d of %d not fully reviewed", left, total)
	default:
		return fmt.Sprintf("oldest commit, %d of %d not fully reviewed", left, total)
	}
}

func commitNoun(count int) string {
	if count == 1 {
		return "commit"
	}
	return "commits"
}

// showCommit pins the commit scope to the commit at index, or follows HEAD
// for the newest.
func (m *Model) showCommit(index int) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok || index < 0 || index >= len(m.diff.commits) {
		return nil
	}
	m.diff.commitPin = commitPin{}
	if index > 0 {
		m.diff.commitPin = commitPin{sha: m.diff.commits[index].SHA, index: index}
	}
	return m.reloadDiffScope(sess)
}

// viewedCommit is the commit in view, or while a step or pick loads, the one
// it is bringing in, so the counter, rail and pill move straight to it.
func (m *Model) viewedCommit() string {
	switch {
	case m.diff.set.Commit != "":
		return m.diff.set.Commit
	case m.diff.commitPin.sha != "":
		return m.diff.commitPin.sha
	case !m.diff.commitPin.unreviewed && len(m.diff.commits) > 0:
		return m.diff.commits[0].SHA
	}
	return ""
}

// commitPosition numbers the commit in view from the oldest, 1, to HEAD,
// total; at is 0 when the list does not hold it.
func (m *Model) commitPosition() (at, total int) {
	total = len(m.diff.commits)
	if i := commitIndex(m.diff.commits, m.viewedCommit()); i >= 0 {
		at = total - i
	}
	return at, total
}

// shownCommit is the commit list's entry for the commit in view.
func (m *Model) shownCommit() (git.Commit, bool) {
	at := commitIndex(m.diff.commits, m.viewedCommit())
	if at < 0 {
		return git.Commit{}, false
	}
	return m.diff.commits[at], true
}

// scopeKey names the rendering review state is taken against: a scope, or
// in the commit scope the commit in view.
func (m *Model) scopeKey() string {
	if m.diff.scope == git.ScopeCommit {
		return commitScopePrefix + m.diff.set.Commit
	}
	return m.diff.scope.String()
}

// inScope reports whether a comment's scope is the rendering in view. One
// with no scope predates scoped comments and matches any scope but a commit,
// which no comment reached before commits had a scope of their own.
func (m *Model) inScope(scope string) bool {
	if scope == "" {
		return m.diff.scope != git.ScopeCommit
	}
	return scope == m.scopeKey()
}

// noteShown reports whether a comment belongs on the rendering in view. One
// taken on a commit shows only there, where its line numbers mean something.
func (m *Model) noteShown(note *annotation) bool {
	return !strings.HasPrefix(note.scope, commitScopePrefix) || note.scope == m.scopeKey()
}

// reviewKey scopes a session's comments and reviewed marks to the repo under
// review, so switching repos never leaks marks between same-named files.
func (m *Model) reviewKey() string {
	return m.diff.sessID + "\x00" + m.diff.repoSel
}

// reviewedMarkKey scopes a reviewed mark to the diff scope it was taken in:
// the same file renders different lines - and so a different content hash -
// under each scope, so only its own scope can read or judge the mark.
func (m *Model) reviewedMarkKey(path string) string {
	return m.scopeKey() + "\x00" + path
}

func readReviewState(stor *store.Store, sessID, repoRoot string) (store.ReviewState, error) {
	state, err := stor.ReviewState(sessID, repoRoot)
	if err != nil {
		return store.ReviewState{}, err
	}
	// A round's numbered points are read before any are handed out, so a
	// point a comment already carries cannot be handed to another one.
	highest := map[int]int{}
	for _, note := range state.Comments {
		if note.Round > 0 && note.Point > highest[note.Round] {
			highest[note.Round] = note.Point
		}
	}
	migrated := false
	for i := range state.Comments {
		note := &state.Comments[i]
		if note.ID == "" {
			note.ID = newReviewCommentID()
			migrated = true
		}
		if note.Round > 0 && note.Point == 0 {
			highest[note.Round]++
			note.Point = highest[note.Round]
			migrated = true
		}
	}
	// Merge rather than set: this write lands outside the review write
	// chain, and merging keeps a status an agent set while the load ran.
	if migrated {
		if err := stor.MergeReviewState(sessID, repoRoot, state); err != nil {
			return store.ReviewState{}, err
		}
	}
	return state, nil
}

func (m *Model) restoreReviewState(state store.ReviewState) bool {
	key := m.reviewKey()
	if m.diff.sessID == "" || m.diff.repoSel == "" || m.diff.stateLoaded[key] {
		return false
	}
	marks := make(map[string]uint64, len(state.Reviewed))
	for markKey, hash := range state.Reviewed {
		// A mark saved before marks were scope-keyed carries a bare path; it
		// can never match a scoped lookup, so dropping it here lets the next
		// save clear it from the row.
		if hash != 0 && strings.Contains(markKey, "\x00") {
			marks[markKey] = hash
		}
	}
	notes := make([]annotation, 0, len(state.Comments))
	for i := range state.Comments {
		note := &state.Comments[i]
		notes = append(notes, annotation{
			id: note.ID, file: note.File, line: note.Line, deleted: note.Deleted,
			excerpt: withoutControlBytes(note.Excerpt), text: withoutControlBytes(note.Text), hash: note.ContentHash,
			round: note.Round, scope: note.Scope, point: note.Point, handled: note.Resolved, outdated: note.Outdated,
		})
	}
	m.diff.reviewed[key] = marks
	m.diff.annotations[key] = notes
	m.diff.rounds[key] = state.Round
	m.diff.stateLoaded[key] = true
	return true
}

func (m *Model) reviewStatusesCmd() tea.Cmd {
	if m.store == nil || !m.diff.active || m.diff.sessID == "" || m.diff.repoSel == "" {
		return nil
	}
	m.diff.reviewStatusGen++
	gen := m.diff.reviewStatusGen
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.store
	writesDone := m.diff.reviewWriteDone
	return func() tea.Msg {
		if writesDone != nil {
			<-writesDone
		}
		state, err := stor.ReviewState(sessID, repoRoot)
		if err != nil {
			return reviewStatusesLoadedMsg{sessID: sessID, repoRoot: repoRoot, gen: gen, err: err}
		}
		handled := make(map[string]bool, len(state.Comments))
		for _, comment := range state.Comments {
			if comment.ID != "" && comment.Round > 0 {
				handled[comment.ID] = comment.Resolved
			}
		}
		return reviewStatusesLoadedMsg{sessID: sessID, repoRoot: repoRoot, gen: gen, handled: handled}
	}
}

func (m *Model) handleReviewStatusesLoaded(msg reviewStatusesLoadedMsg) {
	if !m.diff.active || msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel || msg.gen != m.diff.reviewStatusGen {
		return
	}
	if msg.err != nil {
		m.errBar.text = "loading review statuses: " + msg.err.Error()
		return
	}
	key := m.reviewKey()
	notes := m.diff.annotations[key]
	for i := range notes {
		if handled, ok := msg.handled[notes[i].id]; ok {
			notes[i].handled = handled
		}
	}
	m.diff.annotations[key] = notes
}

func (m *Model) reviewStateSnapshot(key string) store.ReviewState {
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
			Round: note.round, Scope: note.scope, Point: note.point, Resolved: note.handled, Outdated: note.outdated,
		})
	}
	return store.ReviewState{
		Reviewed: marks,
		Comments: notes,
		Round:    m.diff.rounds[key],
	}
}

func (m *Model) chainReviewWrite(run func() tea.Msg) tea.Cmd {
	previous := m.diff.reviewWriteDone
	done := make(chan struct{})
	m.diff.reviewWriteDone = done
	return func() tea.Msg {
		if previous != nil {
			<-previous
		}
		defer close(done)
		return run()
	}
}

func (m *Model) saveReviewStateCmd() tea.Cmd {
	if m.store == nil || m.diff.sessID == "" || m.diff.repoSel == "" {
		return nil
	}
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.store
	state := m.reviewStateSnapshot(m.reviewKey())
	return m.chainReviewWrite(func() tea.Msg {
		return reviewStateSavedMsg{sessID: sessID, repoRoot: repoRoot, err: stor.MergeReviewState(sessID, repoRoot, state)}
	})
}

func (m *Model) reviewCommentHandledCmd(commentID string, handled, previous bool) tea.Cmd {
	sessID, repoRoot, stor := m.diff.sessID, m.diff.repoSel, m.store
	return m.chainReviewWrite(func() tea.Msg {
		found, err := stor.SetReviewCommentHandled(sessID, commentID, handled)
		return reviewCommentHandledMsg{
			sessID: sessID, repoRoot: repoRoot, commentID: commentID,
			handled: handled, previous: previous, found: found, err: err,
		}
	})
}

func (m *Model) handleReviewStateSaved(msg reviewStateSavedMsg) {
	if msg.err != nil && msg.sessID == m.diff.sessID && msg.repoRoot == m.diff.repoSel {
		m.errBar.text = "saving review state: " + msg.err.Error()
	}
}

func (m *Model) handleReviewCommentHandled(msg reviewCommentHandledMsg) {
	if msg.err == nil && msg.found {
		return
	}
	key := msg.sessID + "\x00" + msg.repoRoot
	notes := m.diff.annotations[key]
	for i := range notes {
		if notes[i].id == msg.commentID && notes[i].handled == msg.handled {
			notes[i].handled = msg.previous
		}
	}
	m.diff.annotations[key] = notes
	if msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel {
		return
	}
	if msg.err != nil {
		m.errBar.text = "updating review comment: " + msg.err.Error()
	} else {
		m.errBar.text = "review comment no longer exists"
	}
}

func (m *Model) reviewSendCmd(req reviewSendRequest) tea.Cmd {
	sessID, repoRoot, stor, tmuxDriver := m.diff.sessID, m.diff.repoSel, m.store, m.tmux
	return m.chainReviewWrite(func() tea.Msg {
		msg := reviewSendFinishedMsg{
			sessID: sessID, repoRoot: repoRoot, commentIDs: req.commentIDs,
			previousRound: req.previousRound, round: req.round, count: req.count, sessName: req.sess.Name,
		}
		if !tmuxDriver.Exists(req.sess.ID) {
			msg.err = errors.New(deadSessionHint)
			return msg
		}
		if err := stor.MergeReviewState(sessID, repoRoot, req.state); err != nil {
			msg.err = fmt.Errorf("saving review round: %w", err)
			return msg
		}
		if err := tmuxDriver.SendText(req.sess.ID, req.prompt); err != nil {
			msg.err = err
			if rollbackErr := stor.MergeReviewState(sessID, repoRoot, req.previousState); rollbackErr != nil {
				msg.err = fmt.Errorf("%w; restoring review drafts: %w", err, rollbackErr)
			}
			return msg
		}
		msg.delivered = true
		msg.ackErr = stor.SetAcked(req.sess.ID, false)
		return msg
	})
}

func (m *Model) handleReviewSendFinished(msg reviewSendFinishedMsg) {
	m.diff.reviewSendPending = false
	key := msg.sessID + "\x00" + msg.repoRoot
	if !msg.delivered {
		ids := make(map[string]bool, len(msg.commentIDs))
		for _, id := range msg.commentIDs {
			ids[id] = true
		}
		notes := m.diff.annotations[key]
		for i := range notes {
			if ids[notes[i].id] && notes[i].round == msg.round {
				notes[i].round = 0
				notes[i].point = 0
			}
		}
		m.diff.annotations[key] = notes
		if m.diff.rounds[key].Number == msg.round {
			m.diff.rounds[key] = msg.previousRound
		}
	}
	if msg.sessID != m.diff.sessID || msg.repoRoot != m.diff.repoSel {
		return
	}
	if msg.err != nil {
		m.diff.notice = ""
		m.errBar.text = msg.err.Error()
		return
	}
	m.diff.notice = fmt.Sprintf("sent review round %d (%d %s) to %s",
		msg.round, msg.count, commentNoun(msg.count), msg.sessName)
	if msg.ackErr != nil {
		m.errBar.text = "comments sent, but clearing the alert ack failed: " + msg.ackErr.Error()
	}
	m.requestRefresh()
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
	if msg.scope == git.ScopeCommit {
		m.diff.commits, m.diff.commitTarget = msg.commits, msg.commitTarget
	}
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
	restoredState := false
	if msg.reviewStateErr != nil {
		m.errBar.text = "loading review state: " + msg.reviewStateErr.Error()
	} else if msg.reviewStateLoaded {
		restoredState = m.restoreReviewState(msg.reviewState)
	}
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
	// A pending A belongs to the set it was pressed on: a silent refresh of
	// that set carries it, any other load drops it.
	resumeMarkAll := (m.diff.markAll && msg.refresh) || m.diff.markOnLoad
	if m.diff.markAll {
		m.diff.markAll = false
		m.diff.notice = ""
	}
	m.diff.markOnLoad = false
	m.diff.markFileOnLoad = ""
	carried := false
	if msg.scope == git.ScopeCommit {
		carried = m.settleCommit(msg)
	}
	// Every accepted load re-judges the arriving scope's comments: a scope
	// cycle is neither a refresh nor a restore, but the file list it brings
	// in still decides which of its own comments read outdated.
	stateChanged := clearStaleReviewedMarks(m) || carried
	stateChanged = m.markMissingRoundCommentsOutdated() || stateChanged
	// Re-anchor only on a silent same-scope refresh. A scope cycle or session
	// switch loads a different file set, where matching a comment by excerpt
	// would rewrite its line against content it was never made against. The
	// commit scope keys comments by commit, so each re-anchors only against
	// its own commit, or the one a rewrite carried it to.
	m.diff.fileLoading = map[int]bool{}
	m.diff.reanchor = nil
	reanchor := msg.refresh || restoredState || msg.scope == git.ScopeCommit
	if reanchor {
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
	if reanchor {
		stateful := map[string]bool{}
		for markKey, hash := range m.diff.reviewed[m.reviewKey()] {
			scope, path, _ := strings.Cut(markKey, "\x00")
			if hash != 0 && scope == m.scopeKey() {
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
	var stateSave tea.Cmd
	if stateChanged {
		stateSave = m.saveReviewStateCmd()
	}
	// The selected file gets its own command so it becomes usable immediately.
	// Less urgent review-state files load serially in one background command,
	// avoiding an unbounded git/process fan-out after a large review refresh.
	var markAll tea.Cmd
	if resumeMarkAll {
		markAll = m.markAllReviewed()
	}
	return tea.Batch(currentLoad, diffFilesLoadCmd(statefulLoads), stateSave, markAll, m.startStartupTick())
}

// settleCommit records the commit a commit scope load resolved and carries
// review state across the rewrites it found, reporting whether state moved.
func (m *Model) settleCommit(msg diffLoadedMsg) bool {
	m.diff.rewriteHead = msg.head
	m.diff.commitPin = commitPin{}
	if msg.pinned {
		m.diff.commitPin = commitPin{sha: msg.set.Commit, index: max(commitIndex(msg.commits, msg.set.Commit), 0)}
	}
	if msg.commitNotice != "" {
		m.diff.notice = msg.commitNotice
	}
	if msg.rewriteErr != nil {
		m.errBar.text = "following rewritten commits: " + msg.rewriteErr.Error()
	}
	return m.carryRewrites(msg.rewrites)
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
	var stateSave tea.Cmd
	if stateChanged {
		stateSave = m.saveReviewStateCmd()
	}
	if marked := m.finishMarkAll(); marked != nil {
		stateSave = tea.Batch(stateSave, marked)
	}
	if m.diff.markFileOnLoad == msg.path {
		m.diff.markFileOnLoad = ""
		stateSave = tea.Batch(stateSave, m.toggleFileMark(&m.diff.set.Files[msg.index]))
	}
	if msg.index != m.diff.fileIdx {
		return stateSave
	}
	// A NUL sniff is the only thing that outs a blob whose name gives nothing
	// away, so the file under the cursor can turn into one the filter hides.
	if target := m.nextShownFile(m.diff.fileIdx, 1); target != m.diff.fileIdx {
		return tea.Batch(stateSave, m.switchDiffFile(target-m.diff.fileIdx))
	}
	m.clampDiffCursor()
	return tea.Batch(stateSave, m.ensureHighlight())
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
	return m.reviewKey() + "\x00" + m.scopeKey() + "\x00" + path
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
	fileWidth = max(m.width*24/100, diffFileRailWidth)
	// The file rail keeps its share only while the code pane still has one:
	// a terminal too narrow for both gives the rail what is left over.
	if m.width-fileWidth-diffPaneSeam < diffCodeMinWidth {
		fileWidth = max(m.width-diffPaneSeam-diffCodeMinWidth, 0)
	}
	return fileWidth, max(m.width-fileWidth-diffPaneSeam, 0)
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
	case "L":
		m.openCommitPick()
	case "[":
		return m, m.stepCommit(1)
	case "]":
		return m, m.stepCommit(-1)
	case "{":
		return m, m.stepCommit(len(m.diff.commits))
	case "}":
		return m, m.stepCommit(-len(m.diff.commits))
	case "u":
		lineIdx := m.cursorDiffLine()
		m.diff.sideBySide = !m.diff.sideBySide
		m.setCursorDiffLine(lineIdx)
	case "f":
		return m, m.toggleCodeOnly()
	case " ", "space":
		return m, m.toggleReviewed()
	case "A":
		return m, m.markAllReviewed()
	case "c":
		m.openAnnotate()
	case "d":
		return m, m.discardOrToggleAnnotation()
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
	if m.fileReviewed(fd.File.Path) {
		return m.toggleFileMark(fd)
	}
	stateSave := m.toggleFileMark(fd)
	if !m.fileReviewed(fd.File.Path) {
		return stateSave
	}
	marks := m.diff.reviewed[m.reviewKey()]
	// Advance to the next unreviewed file, review-queue style. The switch
	// returns the highlight command for the newly shown file; dropping it
	// would leave that file unhighlighted.
	for step := 1; step < len(m.diff.set.Files); step++ {
		next := (m.diff.fileIdx + step) % len(m.diff.set.Files)
		if marks[m.reviewedMarkKey(m.diff.set.Files[next].File.Path)] == 0 && !m.diffFileHidden(&m.diff.set.Files[next]) {
			return tea.Batch(stateSave, m.switchDiffFile(next-m.diff.fileIdx))
		}
	}
	return stateSave
}

// markAllReviewed marks every file the list shows reviewed, or clears them
// all when each already is. A mark hashes the file's rendering, so files not
// loaded yet load first and the marks land once the last one arrives.
func (m *Model) markAllReviewed() tea.Cmd {
	if m.store == nil {
		m.errBar.text = "review state is unavailable"
		return nil
	}
	shown, marked := 0, 0
	for i := range m.diff.set.Files {
		fd := &m.diff.set.Files[i]
		if m.diffFileHidden(fd) {
			continue
		}
		shown++
		if m.fileReviewed(fd.File.Path) {
			marked++
		}
	}
	if shown == 0 {
		return nil
	}
	if marked == shown {
		marks := m.diff.reviewed[m.reviewKey()]
		for i := range m.diff.set.Files {
			if fd := &m.diff.set.Files[i]; !m.diffFileHidden(fd) {
				delete(marks, m.reviewedMarkKey(fd.File.Path))
			}
		}
		m.diff.notice = fmt.Sprintf("cleared %d reviewed %s", shown, fileNoun(shown))
		return m.saveReviewStateCmd()
	}
	m.diff.markAll = true
	if done := m.finishMarkAll(); done != nil {
		return done
	}
	var unloaded []tea.Cmd
	for i := range m.diff.set.Files {
		if fd := &m.diff.set.Files[i]; !fd.Loaded() && !m.diffFileHidden(fd) {
			if cmd := m.loadDiffFile(i); cmd != nil {
				unloaded = append(unloaded, cmd)
			}
		}
	}
	m.diff.notice = fmt.Sprintf("loading %d %s to mark reviewed", len(unloaded), fileNoun(len(unloaded)))
	return diffFilesLoadCmd(unloaded)
}

// finishMarkAll lands a pending A once every shown file has loaded.
func (m *Model) finishMarkAll() tea.Cmd {
	if !m.diff.markAll {
		return nil
	}
	for i := range m.diff.set.Files {
		if fd := &m.diff.set.Files[i]; !fd.Loaded() && !m.diffFileHidden(fd) {
			return nil
		}
	}
	m.diff.markAll = false
	marks := m.diff.reviewed[m.reviewKey()]
	if marks == nil {
		marks = map[string]uint64{}
		m.diff.reviewed[m.reviewKey()] = marks
	}
	count := 0
	for i := range m.diff.set.Files {
		if fd := &m.diff.set.Files[i]; !m.diffFileHidden(fd) {
			marks[m.reviewedMarkKey(fd.File.Path)] = contentHash(fd)
			count++
		}
	}
	m.diff.notice = fmt.Sprintf("marked %d %s reviewed", count, fileNoun(count))
	return m.saveReviewStateCmd()
}

func fileNoun(count int) string {
	if count == 1 {
		return "file"
	}
	return "files"
}

// toggleFileMark marks a loaded file reviewed, or clears its mark.
func (m *Model) toggleFileMark(fd *diff.FileDiff) tea.Cmd {
	if m.store == nil {
		m.errBar.text = "review state is unavailable"
		return nil
	}
	marks := m.diff.reviewed[m.reviewKey()]
	if marks == nil {
		marks = map[string]uint64{}
		m.diff.reviewed[m.reviewKey()] = marks
	}
	markKey := m.reviewedMarkKey(fd.File.Path)
	if marks[markKey] != 0 {
		delete(marks, markKey)
	} else {
		marks[markKey] = contentHash(fd)
	}
	return m.saveReviewStateCmd()
}

// clickFileGlyph toggles the reviewed mark of the file whose rail glyph was
// clicked, leaving the file in view where it is. A file not loaded yet loads
// first, since its mark hashes the rendering.
func (m *Model) clickFileGlyph(index int) tea.Cmd {
	fd := &m.diff.set.Files[index]
	if m.diffFileHidden(fd) {
		return nil
	}
	if fd.Loaded() {
		return m.toggleFileMark(fd)
	}
	m.diff.markFileOnLoad = fd.File.Path
	return m.loadDiffFile(index)
}

func (m *Model) fileReviewed(path string) bool {
	return m.diff.reviewed[m.reviewKey()][m.reviewedMarkKey(path)] != 0
}

func clearStaleReviewedMarks(m *Model) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	if len(marks) == 0 {
		return false
	}
	changed := false
	for markKey, stored := range marks {
		if stored == 0 {
			continue
		}
		// A mark hashes the rendering of the scope it was taken in, so only
		// that scope can judge it stale. A scope that does not list the file
		// says nothing about it either, and the mark outlives the scope.
		scope, path, _ := strings.Cut(markKey, "\x00")
		if scope != m.scopeKey() {
			continue
		}
		fd := m.fileDiffByPath(path)
		if fd != nil && fd.Loaded() && contentHash(fd) != stored {
			delete(marks, markKey)
			changed = true
		}
	}
	return changed
}

func clearStaleReviewedMark(m *Model, path string) bool {
	marks := m.diff.reviewed[m.reviewKey()]
	markKey := m.reviewedMarkKey(path)
	stored := marks[markKey]
	if stored == 0 {
		return false
	}
	fd := m.fileDiffByPath(path)
	if fd != nil && fd.Loaded() && contentHash(fd) != stored {
		delete(marks, markKey)
		return true
	}
	return false
}

// A round sent under another scope listed other files, so this scope's file
// list says nothing about whether those comments still sit on their code.
// Each comment is judged against the scope its own round was sent in.
func (m *Model) markMissingRoundCommentsOutdated() bool {
	// Comments saved before they carried a scope fall back to the latest
	// round's scope, the only one recorded then.
	fallback := m.diff.rounds[m.reviewKey()].Scope
	paths := make(map[string]bool, len(m.diff.set.Files))
	for i := range m.diff.set.Files {
		paths[m.diff.set.Files[i].File.Path] = true
	}
	notes := m.diff.annotations[m.reviewKey()]
	changed := false
	for i := range notes {
		if notes[i].round == 0 || notes[i].outdated || paths[notes[i].file] {
			continue
		}
		scope := notes[i].scope
		if scope == "" {
			scope = fallback
		}
		if !m.inScope(scope) {
			continue
		}
		notes[i].outdated = true
		changed = true
	}
	return changed
}

func (m *Model) openAnnotate() {
	fd := m.currentFileDiff()
	if fd == nil || m.diffFileHidden(fd) {
		return
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) || fd.Lines[lineIdx].Kind == diff.Gap {
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
		if notes[i].round == 0 && notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted && m.noteShown(&notes[i]) {
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
		if notes[i].file == path && notes[i].line == num && notes[i].deleted == deleted && m.noteShown(&notes[i]) {
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
		// A comment's line and hash track the rendering of its own scope;
		// another scope's rendering would move it onto lines it was never
		// made against. Comments saved before they carried a scope fall
		// back to the latest round's scope, the only one recorded then.
		scope := note.scope
		if scope == "" && note.round > 0 {
			scope = m.diff.rounds[m.reviewKey()].Scope
		}
		if !m.inScope(scope) {
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
			if line.Kind == diff.Gap {
				continue
			}
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
		return m, m.saveAnnotation()
	}
	var cmd tea.Cmd
	m.diff.annInput, cmd = m.diff.annInput.Update(msg)
	return m, cmd
}

func (m *Model) saveAnnotation() tea.Cmd {
	m.diff.annotating = false
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return nil
	}
	text := strings.TrimSpace(withoutControlBytes(m.diff.annInput.Value()))
	line := fd.Lines[lineIdx]
	num, deleted := annotationLine(line)
	if existing := m.annotationAt(fd.File.Path, line); existing != nil {
		if text == "" {
			return m.discardOrToggleAnnotation()
		}
		existing.text = text
		existing.hash = contentHash(fd)
		existing.scope = m.scopeKey()
		return m.saveReviewStateCmd()
	}
	if text == "" {
		return nil
	}
	m.diff.annotations[m.reviewKey()] = append(m.diff.annotations[m.reviewKey()], annotation{
		id:      newReviewCommentID(),
		file:    fd.File.Path,
		line:    num,
		deleted: deleted,
		excerpt: excerptOf(line.Text),
		text:    text,
		hash:    contentHash(fd),
		scope:   m.scopeKey(),
	})
	return m.saveReviewStateCmd()
}

func (m *Model) discardOrToggleAnnotation() tea.Cmd {
	fd := m.currentFileDiff()
	if fd == nil {
		return nil
	}
	lineIdx := m.cursorDiffLine()
	if lineIdx < 0 || lineIdx >= len(fd.Lines) {
		return nil
	}
	num, deleted := annotationLine(fd.Lines[lineIdx])
	notes := m.diff.annotations[m.reviewKey()]
	for i := range notes {
		if notes[i].round == 0 && notes[i].file == fd.File.Path && notes[i].line == num && notes[i].deleted == deleted && m.noteShown(&notes[i]) {
			m.diff.annotations[m.reviewKey()] = append(notes[:i], notes[i+1:]...)
			return m.saveReviewStateCmd()
		}
	}
	latestOpen := -1
	latestHandled := -1
	for i := range notes {
		if notes[i].round == 0 || notes[i].file != fd.File.Path || notes[i].line != num || notes[i].deleted != deleted || !m.noteShown(&notes[i]) {
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
		if m.store == nil {
			m.errBar.text = "review state is unavailable"
			return nil
		}
		previous := notes[target].handled
		handled := !notes[target].handled
		m.diff.reviewStatusGen++
		notes[target].handled = handled
		m.diff.annotations[m.reviewKey()] = notes
		return m.reviewCommentHandledCmd(notes[target].id, handled, previous)
	}
	return nil
}

// Newlines would submit the prompt before every comment reaches the pane.
func (m *Model) sendAnnotations() (tea.Model, tea.Cmd) {
	if m.diff.reviewSendPending {
		m.errBar.text = "review round is already being sent"
		return m, nil
	}
	if m.store == nil {
		m.errBar.text = "review state is unavailable"
		return m, nil
	}
	sess, ok := m.diffSession()
	if !ok {
		m.errBar.text = "session is gone"
		return m, nil
	}
	if m.isShell(sess.Tool) {
		m.errBar.text = shellPromptHint(sess.Name)
		return m, nil
	}
	key := m.reviewKey()
	previousState := m.reviewStateSnapshot(key)
	notes := append([]annotation(nil), m.diff.annotations[key]...)
	round := m.diff.rounds[key]
	previousRound := round
	nextRound := round.Number + 1
	var parts []string
	draftIndexes := make([]int, 0, len(notes))
	for i, note := range notes {
		if note.round != 0 {
			continue
		}
		draftIndexes = append(draftIndexes, i)
		notes[i].point = len(parts) + 1
		location := fmt.Sprintf("%s:%d", note.file, note.line)
		if sha, ok := strings.CutPrefix(note.scope, commitScopePrefix); ok && note.scope != m.scopeKey() {
			location += " in commit " + shortSHA(sha)
		}
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
		reviewSubject(m.diff.scope, m.diff.set, m.diff.repoSel), strings.Join(parts, "; "))
	round.Number = nextRound
	round.Scope = m.scopeKey()
	round.Fingerprint = m.diff.fingerprint
	m.diff.rounds[key] = round
	for _, i := range draftIndexes {
		notes[i].round = round.Number
		// A comment on another commit stays keyed by the commit it was made on.
		if !strings.HasPrefix(notes[i].scope, commitScopePrefix) {
			notes[i].scope = round.Scope
		}
		notes[i].handled = false
		notes[i].outdated = false
	}
	m.diff.annotations[key] = notes
	count := len(draftIndexes)
	commentIDs := make([]string, 0, len(draftIndexes))
	for _, i := range draftIndexes {
		commentIDs = append(commentIDs, notes[i].id)
	}
	m.diff.reviewSendPending = true
	m.diff.notice = fmt.Sprintf("sending review round %d to %s", round.Number, sess.Name)
	return m, m.reviewSendCmd(reviewSendRequest{
		sess: sess, prompt: prompt,
		state: m.reviewStateSnapshot(key), previousState: previousState,
		commentIDs: commentIDs, previousRound: previousRound,
		round: round.Number, count: count,
	})
}

// excerptOf caps a code excerpt at 60 runes, never splitting a rune, so
// multibyte lines stay valid UTF-8 in the prompt sent to the agent.
// A comment and its excerpt are painted into the pane and pasted into the
// agent's prompt, so a control byte from the file under review or from a
// paste would drive the terminal instead of reading as text.
func withoutControlBytes(text string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.IsControl(r):
			return -1
		}
		return r
	}, text)
}

func excerptOf(text string) string {
	excerpt := strings.TrimSpace(withoutControlBytes(text))
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

// reviewSubject names the checkout a round was written against, because the
// session receiving it may sit on another branch or worktree, or none at all.
func reviewSubject(scope git.Scope, set diff.Set, repoDir string) string {
	subject := scopePhrase(scope)
	if scope == git.ScopeCommit && set.Commit != "" {
		subject = fmt.Sprintf("commit `%s`", shortSHA(set.Commit))
	}
	if set.Repo.Detached {
		subject += fmt.Sprintf(" on detached HEAD `%s`", set.Repo.Head)
	} else if set.Repo.Branch != "" {
		subject += fmt.Sprintf(" on `%s`", set.Repo.Branch)
		if set.Repo.Head != "" {
			subject += fmt.Sprintf(" (`%s`)", set.Repo.Head)
		}
	}
	if scope == git.ScopeBranch && set.BaseDesc != "" {
		target := stripBaseHash(set.BaseDesc)
		subject += fmt.Sprintf(" vs `%s` (merge-base `%s`)", target, strings.TrimPrefix(set.BaseDesc, target+"@"))
	}
	return subject + fmt.Sprintf(" in `%s`", escapeControlsInline(repoDir))
}

func scopePhrase(scope git.Scope) string {
	switch scope {
	case git.ScopeBranch:
		return "the branch changes"
	case git.ScopeCommit:
		return "the commit"
	case git.ScopeStaged:
		return "the staged changes"
	default:
		return "the uncommitted changes"
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
	m.resetCommitScope()
	m.applyStoredScope(sess.ID)
	return tea.Batch(m.retargetDiff(sess), m.startStartupTick())
}
