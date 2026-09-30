package ui

import (
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"time"
)

type previewMsg struct {
	sessID  string
	preview string
	proc    sysstat.ProcStat
	// gen is the previewGen the capture was scheduled for; mismatched
	// gens are discarded so a hold-j burst cannot paint a stale session.
	gen uint64
}

// previewSettleMsg fires after the cursor has stopped moving so one
// capture runs instead of one per key-repeat tick.
type previewSettleMsg struct {
	gen uint64
}

// previewSettle is how long we wait after the last cursor move before
// talking to tmux. Short enough to feel instant, long enough to collapse
// a held j/k burst into a single capture.
const previewSettle = 50 * time.Millisecond

// The selected session's pane is re-captured on its own timer. The full
// poll is deliberately slow (it lists panes, samples every process tree and
// writes the store), which left the preview refreshing on the poll cadence
// and reading as a still image of a live agent. One capture of one pane is
// cheap, but it is still a tmux exec, so the rate follows the session: an
// agent that is producing output earns a fast cadence, one that is waiting
// on a human does not.
const (
	startupInterval     = 80 * time.Millisecond
	previewIntervalLive = 300 * time.Millisecond
	previewIntervalCalm = 1200 * time.Millisecond
	updateTickInterval  = 10 * time.Minute
)

// cursorBlinkMsg toggles the focused pane's caret.
type cursorBlinkMsg struct{}

// cursorBlinkInterval is the caret's half period, matching the rate most
// terminals blink their own.
const cursorBlinkInterval = 530 * time.Millisecond

// cursorBlink re-arms the caret timer. It runs only while a session is
// focused; every other mode lets the timer die.
func (m *Model) cursorBlink() tea.Cmd {
	return tea.Tick(cursorBlinkInterval, func(time.Time) tea.Msg { return cursorBlinkMsg{} })
}

// previewTickMsg drives that timer.
type previewTickMsg struct{}

func (m *Model) hasStartingRow() bool {
	return m.rail.HasStatus(status.Starting)
}

func (m *Model) previewInterval() time.Duration {
	if sess, ok := m.selected(); ok {
		switch sess.Status {
		case status.Working, status.Starting:
			return previewIntervalLive
		}
	}
	return previewIntervalCalm
}

func (m *Model) previewTick() tea.Cmd {
	return tea.Tick(m.previewInterval(), func(time.Time) tea.Msg { return previewTickMsg{} })
}

func (m *Model) needsLoaderTick() bool {
	return m.startup.booting || m.hasStartingRow() || m.reviewNeedsLoader() || m.hasWorkingLoaderRow()
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
	if m.services.engine == nil || m.services.tmux == nil {
		return
	}
	pane, err := m.services.tmux.CapturePane(sess.ID)
	if err != nil {
		return
	}
	if draft, ok := m.services.engine.InputDraft(sess.Tool, ansi.Strip(pane)); ok {
		m.startup.pendingTyped = &typedPromptCandidate{id: sess.ID, text: draft, at: time.Now()}
	}
}

// commitTypedPrompt records a stashed draft as the session's last prompt
// once the session runs with it: an enter that opened a menu or answered
// a dialog never turns the session working while its draft is fresh, so
// that candidate just expires.
func (m *Model) commitTypedPrompt() {
	cand := m.startup.pendingTyped
	if cand == nil {
		return
	}
	if time.Since(cand.at) > typedPromptGrace {
		m.startup.pendingTyped = nil
		return
	}
	for i := range m.workspace.sessions {
		if m.workspace.sessions[i].ID != cand.id {
			continue
		}
		if m.workspace.sessions[i].Status != status.Working {
			return
		}
		if err := ignoreDeletedSession(m.services.store.SetLastPrompt(cand.id, cand.text)); err != nil {
			m.errBar.text = err.Error()
		}
		m.workspace.sessions[i].LastPrompt = cand.text
		m.startup.pendingTyped = nil
		return
	}
	m.startup.pendingTyped = nil
}

// hasWorkingLoaderRow reports whether a row is animating the working
// loader: a working session with no quotable pane line yet.
func (m *Model) hasWorkingLoaderRow() bool {
	return m.rail.HasWorkingLoader()
}

func (m *Model) reviewNeedsLoader() bool {
	state := m.review.Snapshot()
	if m.mode != modeDiff || !state.Active {
		return false
	}
	if state.Loading && len(state.Set.Files) == 0 {
		return true
	}
	fd := m.currentFileDiff()
	return fd != nil && !fd.Loaded() && !m.diffFileHidden(fd)
}

// schedulePreview arms a single capture after previewSettle. Call after
// bumping previewGen so earlier timers and in-flight captures go stale.
func (m *Model) schedulePreview() tea.Cmd {
	gen := m.focusPane.PreviewGeneration()
	return tea.Tick(previewSettle, func(time.Time) tea.Msg {
		return previewSettleMsg{gen: gen}
	})
}

// previewCmd captures one session's pane and process stats off the
// render loop. gen tags the result so a newer cursor move can discard it.
// Size pins stay on resizeSessions / create / attach — not on every look —
// so a settle capture is one capture-pane, not resize+capture+pid storms.
func (m *Model) previewCmd(sess store.Session, gen uint64) tea.Cmd {
	return func() tea.Msg {
		msg := previewMsg{sessID: sess.ID, gen: gen}
		if sess.Archived || !m.services.tmux.Exists(sess.ID) {
			snapshot, err := storedPreview(m.services.store, m.services.tmux, sess.ID)
			if err != nil {
				return errMsg{err}
			}
			msg.preview = snapshot
			return msg
		}
		if pane, err := m.services.tmux.CapturePane(sess.ID); err == nil {
			msg.preview = pane
		}
		if pid, err := m.services.tmux.PanePID(sess.ID); err == nil {
			memTotal, _ := sysstat.MemTotalBytes()
			msg.proc = sysstat.Trees([]int{pid})[pid].ScaleToHost(sysstat.LogicalCPUs(), memTotal)
		}
		return msg
	}
}

// setPreview stores a polled or ticked pane capture, unless the session's
// control client is already pushing frames. Those two paths sample at
// different times, and a capture taken a second ago repainting over a
// pushed one is what makes typed characters blink in and out.
func (m *Model) setPreview(sessID, preview string) {
	if sessID != "" && m.focusRuntime.watch != nil && m.focusRuntime.watch.serving(sessID) {
		return
	}
	// A scrolled-back pane holds still on this path too: without a control
	// client the poll is the only source of frames, and a live bottom
	// landing mid-read is the same yank the pushed frames are held back
	// from.
	if m.focusPane.ScrolledBack() {
		return
	}
	m.workspace.preview = preview
}

// sessionGone reports whether id is absent from a refresh's session list.
func sessionGone(sessions []store.Session, id string) bool {
	for _, sess := range sessions {
		if sess.ID == id {
			return false
		}
	}
	return true
}
