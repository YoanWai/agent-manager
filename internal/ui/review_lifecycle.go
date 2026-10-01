package ui

import (
	"errors"

	"github.com/YoanWai/agent-manager/internal/deps"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/YoanWai/agent-manager/internal/store"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
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
		if !m.focusSession(ret.sessionID) {
			m.errBar.text = "the review origin is no longer listed"
			return nil
		}
		_, cmd := m.focusSelected()
		return cmd
	default:
		return nil
	}
}

type reviewPreferencesReader interface {
	ReviewScope(string) (string, error)
	ReviewRepo(string) (string, error)
}

type reviewPreferencesMsg struct {
	session   store.Session
	request   uireview.LoadRequest
	scope     git.Scope
	preferred string
	err       error
}

func (m *Model) openDiff() tea.Cmd { return m.openDiffWithReader(m.services.store) }

func (m *Model) openDiffWithReader(reader reviewPreferencesReader) tea.Cmd {
	if m.services.gitDrv == nil {
		m.errBar.text = "git not found in PATH, " + deps.Hint("git")
		return nil
	}
	sess, ok := m.selected()
	if !ok {
		m.errBar.text = "select a session to diff"
		return nil
	}
	preferred, picked := m.ledger.pickedRepos[sess.ID]
	m.reviewReturn = reviewReturn{kind: reviewReturnList}
	m.mode = modeDiff
	m.errBar.text = ""
	target := reviewTarget(sess)
	target.Cwd = m.sessionDir(sess)
	request := m.review.Open(target, git.ScopeUncommitted, preferred)
	return func() tea.Msg {
		result := reviewPreferencesMsg{session: sess, request: request, scope: git.ScopeUncommitted, preferred: preferred}
		if reader != nil {
			scope, err := reader.ReviewScope(sess.ID)
			result.err = err
			if err == nil {
				result.scope = reviewScopeValue(scope)
			}
			if !picked {
				repo, err := reader.ReviewRepo(sess.ID)
				result.preferred = repo
				result.err = errors.Join(result.err, err)
			}
		}
		return result
	}
}

func (m *Model) handleReviewPreferences(msg reviewPreferencesMsg) (tea.Model, tea.Cmd) {
	if m.effects.quitting || !m.review.Active() || m.review.Generation() != msg.request.Generation || m.review.SessionID() != msg.request.Target.ID {
		return m, nil
	}
	current, ok := m.diffSession()
	if !ok || !current.CreatedAt.Equal(msg.session.CreatedAt) || !current.LaunchTime().Equal(msg.session.LaunchTime()) || current.TmuxSocket != msg.session.TmuxSocket {
		return m, nil
	}
	request := m.review.Open(msg.request.Target, msg.scope, msg.preferred)
	if msg.err != nil {
		m.errBar.text = "reading review preferences: " + msg.err.Error()
	}
	return m, tea.Batch(m.reviewLoadCmd(request), m.startStartupTick())
}

func reviewScopeValue(stored string) git.Scope {
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
