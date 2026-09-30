package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"maps"
	"path/filepath"
	"strings"
	"sync"
)

// repoWant is matched by path so the selection survives ResolveRepos re-ranking between loads.
func (m *Model) diffLoadCmd(sess store.Session, scope git.Scope, gen int, repoWant string, refresh bool) tea.Cmd {
	driver := m.services.gitDrv
	stor := m.services.store
	// Restoring happens once per repo, so a reload that would only have its
	// state discarded reads nothing and cannot migrate over a chained write.
	restored := maps.Clone(m.diff.stateLoaded)
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
		if !restored[sess.ID+"\x00"+roots[repoIdx]] {
			msg.reviewState, msg.reviewStateErr = readReviewState(stor, sess.ID, roots[repoIdx])
			msg.reviewStateLoaded = msg.reviewStateErr == nil
		}
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
	driver := m.services.gitDrv
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen}
		finishDiffMsg(driver, scope, gen, gitRoot, override, repoRoots, &msg)
		return msg
	}
}

// diffRebaseCmd reloads the way diffReloadCmd does, but reads the stored
// base itself: the single store connection can wait behind the poller, and
// Update is not the place to wait for it.
func (m *Model) diffRebaseCmd(sess store.Session, scope git.Scope, gen int, gitRoot string, repoRoots []string) tea.Cmd {
	driver, stor := m.services.gitDrv, m.services.store
	return func() tea.Msg {
		msg := diffLoadedMsg{sessID: sess.ID, scope: scope, gen: gen, repoRoots: repoRoots, repoRoot: gitRoot}
		override, err := stor.ReviewBase(sess.ID, resolveSymlinksOrSelf(gitRoot))
		if err != nil {
			msg.err = err
			return msg
		}
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
	driver := m.services.gitDrv
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
	driver := m.services.gitDrv
	// The override is keyed by the raw selection while the git operations run
	// against the resolved toplevel - the same split diffLoadCmd uses, so probe
	// and load produce the same fingerprint instead of reloading every tick.
	repoSel := m.diff.repoSel
	gitRoot := m.diff.set.Repo.Root
	stor := m.services.store
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
	if picked, ok := m.ledger.pickedRepos[sess.ID]; ok {
		m.diff.repoSel = picked
	} else if declared, err := m.services.store.ReviewRepo(sess.ID); err != nil {
		m.errBar.text = err.Error()
	} else if declared != "" {
		m.diff.repoSel = declared
	}
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

func (m *Model) applyStoredScope(sessionID string) {
	if m.services.store == nil {
		return
	}
	stored, err := m.services.store.ReviewScope(sessionID)
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
		return tea.Batch(m.diffRebaseCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, m.diff.repoRoots), m.startStartupTick())
	}
	return tea.Batch(m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false), m.startStartupTick())
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
	restoredState := false
	if msg.reviewStateErr != nil {
		m.errBar.text = "loading review state: " + msg.reviewStateErr.Error()
	} else if msg.reviewStateLoaded {
		restoredState = m.restoreReviewState(msg.reviewState)
	}
	if msg.missingRepo != "" {
		m.errBar.text = fmt.Sprintf("picked or declared repo %s is no longer under the session directory",
			filepath.Base(msg.missingRepo))
		if m.ledger.pickedRepos[msg.sessID] == msg.missingRepo {
			delete(m.ledger.pickedRepos, msg.sessID)
		}
	}
	previousPath := ""
	if fd := m.currentFileDiff(); fd != nil {
		previousPath = fd.File.Path
	}
	m.diff.set = msg.set
	// Every accepted load re-judges the arriving scope's comments: a scope
	// cycle is neither a refresh nor a restore, but the file list it brings
	// in still decides which of its own comments read outdated.
	stateChanged := clearStaleReviewedMarks(m)
	stateChanged = m.markMissingRoundCommentsOutdated() || stateChanged
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
		for markKey, hash := range m.diff.reviewed[m.reviewKey()] {
			scope, path, _ := strings.Cut(markKey, "\x00")
			if hash != 0 && scope == m.diff.scope.String() {
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
	return tea.Batch(currentLoad, diffFilesLoadCmd(statefulLoads), stateSave, m.startStartupTick())
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
	if !m.diff.active || m.diff.loading || m.services.gitDrv == nil || m.diff.set.Repo.Root == "" {
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
