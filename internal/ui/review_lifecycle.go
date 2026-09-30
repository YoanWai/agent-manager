package ui

import (
	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type reviewReturnKind uint8

const (
	reviewReturnList reviewReturnKind = iota
	reviewReturnFocus
	reviewReturnAttach
)

type reviewReturn struct {
	kind      reviewReturnKind
	sessionID string
}

func (m *Model) diffSession() (store.Session, bool) {
	for _, sess := range m.workspace.sessions {
		if sess.ID == m.review.SessionID() {
			return sess, true
		}
	}
	return store.Session{}, false
}

func (m *Model) closeDiff() tea.Cmd {
	ret := m.reviewReturn
	m.reviewReturn = reviewReturn{}
	gen := m.review.Close()
	m.mode = modeList
	switch ret.kind {
	case reviewReturnAttach:
		return m.reattach(ret.sessionID, gen)
	case reviewReturnFocus:
		_, cmd := m.focusSelected()
		return cmd
	default:
		return nil
	}
}

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
	scope := m.storedReviewScope(sess.ID)
	preferred := ""
	if picked, ok := m.ledger.pickedRepos[sess.ID]; ok {
		preferred = picked
	} else if declared, err := m.services.store.ReviewRepo(sess.ID); err != nil {
		m.errBar.text = err.Error()
	} else {
		preferred = declared
	}
	m.reviewReturn = reviewReturn{kind: reviewReturnList}
	m.mode = modeDiff
	m.errBar.text = ""
	request := m.review.Open(reviewTarget(sess), scope, preferred)
	return tea.Batch(m.reviewLoadCmd(request), m.startStartupTick())
}

func (m *Model) storedReviewScope(sessionID string) git.Scope {
	if m.services.store == nil {
		return git.ScopeUncommitted
	}
	stored, err := m.services.store.ReviewScope(sessionID)
	if err != nil {
		return git.ScopeUncommitted
	}
	switch stored {
	case "branch":
		return git.ScopeBranch
	case "last_commit":
		return git.ScopeLastCommit
	case "staged":
		return git.ScopeStaged
	default:
		return git.ScopeUncommitted
	}
}
