package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	tea "github.com/charmbracelet/bubbletea"
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

// watchSelection points the control client at the current selection. Call
// it where the selection has come to rest, never on every cursor move.
func (m *Model) watchSelection() {
	if m.focus == nil {
		return
	}
	sess, ok := m.selected()
	if !ok || sess.Archived {
		m.focus.setFocus("")
		return
	}
	m.focus.setFocus(sess.ID)
}

// schedulePreview arms a single capture after previewSettle. Call after
// bumping previewGen so earlier timers and in-flight captures go stale.
func (m *Model) schedulePreview() tea.Cmd {
	gen := m.previewGen
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
		if sess.Archived || !m.tmux.Exists(sess.ID) {
			snapshot, err := storedPreview(m.store, m.tmux, sess.ID)
			if err != nil {
				return errMsg{err}
			}
			msg.preview = snapshot
			return msg
		}
		if pane, err := m.tmux.CapturePane(sess.ID); err == nil {
			msg.preview = pane
		}
		if pid, err := m.tmux.PanePID(sess.ID); err == nil {
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
	if sessID != "" && m.focus != nil && m.focus.serving(sessID) {
		return
	}
	// A scrolled-back pane holds still on this path too: without a control
	// client the poll is the only source of frames, and a live bottom
	// landing mid-read is the same yank the pushed frames are held back
	// from.
	if m.scrolledBack() {
		return
	}
	m.preview = preview
}
