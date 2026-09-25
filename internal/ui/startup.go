package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

type startupTickMsg struct{}

func (m *Model) hasStartingRow() bool {
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Status == status.Starting {
			return true
		}
	}
	return false
}

func (m *Model) needsLoaderTick() bool {
	return m.booting || m.hasStartingRow() || m.reviewNeedsLoader() || m.hasWorkingLoaderRow()
}

// typedPromptCandidate is a composer draft snapshotted as enter went into
// a focused pane, held until the session shows the send went through.
type typedPromptCandidate struct {
	id   string
	text string
	at   time.Time
}

// typedPromptGrace is how long a candidate waits for its session to turn
// working before it is judged a menu enter and dropped.
const typedPromptGrace = 5 * time.Second

// stashTypedPrompt snapshots the composer draft as enter goes into the
// focused pane, from a fresh capture so the newest keystrokes are in it.
func (m *Model) stashTypedPrompt(sess store.Session) {
	if m.engine == nil || m.tmux == nil {
		return
	}
	pane, err := m.tmux.CapturePane(sess.ID)
	if err != nil {
		return
	}
	if draft, ok := m.engine.InputDraft(sess.Tool, ansi.Strip(pane)); ok {
		m.pendingTyped = &typedPromptCandidate{id: sess.ID, text: draft, at: time.Now()}
	}
}

// commitTypedPrompt records a stashed draft as the session's last prompt
// once the session runs with it: an enter that opened a menu or answered
// a dialog never turns the session working while its draft is fresh, so
// that candidate just expires.
func (m *Model) commitTypedPrompt() {
	cand := m.pendingTyped
	if cand == nil {
		return
	}
	if time.Since(cand.at) > typedPromptGrace {
		m.pendingTyped = nil
		return
	}
	for i := range m.sessions {
		if m.sessions[i].ID != cand.id {
			continue
		}
		if m.sessions[i].Status != status.Working {
			return
		}
		if err := ignoreDeletedSession(m.store.SetLastPrompt(cand.id, cand.text)); err != nil {
			m.errBar.text = err.Error()
		}
		m.sessions[i].LastPrompt = cand.text
		m.pendingTyped = nil
		return
	}
	m.pendingTyped = nil
}

// hasWorkingLoaderRow reports whether a row is animating the working
// loader: a working session with no quotable pane line yet.
func (m *Model) hasWorkingLoaderRow() bool {
	for _, row := range m.rows {
		if !row.isGroup && row.sess.Status == status.Working && m.paneLines[row.sess.ID] == "" {
			return true
		}
	}
	return false
}

func (m *Model) reviewNeedsLoader() bool {
	if m.mode != modeDiff || !m.diff.active {
		return false
	}
	if m.diff.loading && len(m.diff.set.Files) == 0 {
		return true
	}
	fd := m.currentFileDiff()
	return fd != nil && !fd.Loaded() && !m.diffFileHidden(fd)
}

func (m *Model) startStartupTick() tea.Cmd {
	if m.startupAnimating || !m.needsLoaderTick() {
		return nil
	}
	m.startupAnimating = true
	return func() tea.Msg { return startupTickMsg{} }
}

func (m *Model) startupTick() tea.Cmd {
	return tea.Tick(startupInterval, func(time.Time) tea.Msg { return startupTickMsg{} })
}
