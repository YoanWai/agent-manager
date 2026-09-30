package ui

import (
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// diffSession resolves the session the diff is pinned to.
func (m *Model) diffSession() (store.Session, bool) {
	for _, sess := range m.workspace.sessions {
		if sess.ID == m.diff.sessID {
			return sess, true
		}
	}
	return store.Session{}, false
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

// openDiff enters the full-screen review for the selected session,
// loading its diff. The whole review takes over the screen so the
// content scrolls freely instead of sharing the narrow sidebar.
func (m *Model) openDiff() tea.Cmd {
	if m.services.gitDrv == nil {
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
