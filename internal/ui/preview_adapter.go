package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/sysstat"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uicapture "github.com/YoanWai/agent-manager/internal/ui/preview/capture"
	tea "github.com/charmbracelet/bubbletea"
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

// previewCaptures is the root as the preview's capture runtime reaches it.
type previewCaptures struct{ *Model }

func (r previewCaptures) Selected() (store.Session, bool) { return r.selected() }

func (r previewCaptures) Generation() uint64 { return r.focus.pane.PreviewGeneration() }

func (r previewCaptures) Accepts(gen uint64) bool { return r.focus.pane.AcceptPreview(gen) }

// CaptureWanted reports whether a capture of sess would be painted. A
// session with a control client already pushes every frame, and review and
// the modal screens have no preview to feed.
func (r previewCaptures) CaptureWanted(sess store.Session) bool {
	if !r.previewFeeds() {
		return false
	}
	return !r.focus.runtime.Serving(sess.ID)
}

func (r previewCaptures) WatchSelection() { r.watchSelection() }

func (r previewCaptures) ArmTick(withRemote bool) tea.Cmd {
	if !withRemote {
		return r.workspace.capture.Tick(r)
	}
	return tea.Batch(r.tickRemotePreview(), r.workspace.capture.Tick(r))
}

func (r previewCaptures) ApplyCapture(sessID, preview string, proc sysstat.ProcStat) {
	r.setPreview(sessID, preview)
	r.workspace.proc, r.workspace.procFor = proc, sessID
}

func (r previewCaptures) ReportErr(err error) { r.reportErr(err.Error()) }

// previewInterval is the cadence the current selection earns.
func (m *Model) previewInterval() time.Duration {
	return m.workspace.capture.Interval(previewCaptures{m})
}

func (m *Model) schedulePreview() tea.Cmd {
	return m.workspace.capture.Schedule(m.focus.pane.PreviewGeneration())
}

func (m *Model) previewCmd(sess store.Session, gen uint64) tea.Cmd {
	return m.workspace.capture.Capture(sess, gen)
}

func (m *Model) holdTypedPrompt(cand uicapture.TypedPrompt) { m.workspace.capture.Hold(cand) }

// commitTypedPrompt records each stashed draft as its session's last prompt
// once that session runs with it.
func (m *Model) commitTypedPrompt() {
	m.workspace.capture.Commit(m.workspace.sessions, time.Now(), func(sess store.Session, text string) {
		m.dispatchInput(focusruntime.Request{Kind: focusruntime.Prompt, Session: sess, Text: text})
	})
}

// setPreview stores a polled or ticked pane capture, unless the session's
// control client is already pushing frames. Those two paths sample at
// different times, and a capture taken a second ago repainting over a
// pushed one is what makes typed characters blink in and out.
func (m *Model) setPreview(sessID, preview string) {
	if sessID != "" && m.focus.runtime.Serving(sessID) {
		return
	}
	// A scrolled-back pane holds still on this path too: without a control
	// client the poll is the only source of frames, and a live bottom
	// landing mid-read is the same yank the pushed frames are held back
	// from.
	if m.focus.pane.ScrolledBack() {
		return
	}
	m.workspace.preview = preview
}

func (m *Model) reviewNeedsLoader() bool {
	state := m.review.Snapshot()
	if m.base != baseReview || !state.Active {
		return false
	}
	if state.Loading && len(state.Set.Files) == 0 {
		return true
	}
	fd := m.currentFileDiff()
	return fd != nil && !fd.Loaded() && !m.diffFileHidden(fd)
}

func (m *Model) needsLoaderTick() bool {
	return m.startup.Booting || m.rail.HasStatus(status.Starting) || m.reviewNeedsLoader() || m.rail.HasWorkingLoader()
}

func (m *Model) routePreviewMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if _, ok := msg.(cursorBlinkMsg); ok {
		if !m.focused() {
			return routed(m, nil)
		}
		m.focus.pane.Blink()
		return routed(m, m.cursorBlink())
	}
	if cmd, ok := m.workspace.capture.Route(previewCaptures{m}, msg); ok {
		return routed(m, cmd)
	}
	return nil, nil, false
}
