package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	focusruntime "github.com/YoanWai/agent-manager/internal/ui/focus/runtime"
	uicapture "github.com/YoanWai/agent-manager/internal/ui/preview/capture"
	uireviewsource "github.com/YoanWai/agent-manager/internal/ui/review/source"
	uilifecycle "github.com/YoanWai/agent-manager/internal/ui/session/lifecycle"
	tea "github.com/charmbracelet/bubbletea"
)

// focusHost is the root as a focused session reads it: the selected row, the
// pane's height, and the manager's own keys.
type focusHost struct{ m *Model }

func (h focusHost) Session() (string, bool) {
	sess, ok := h.m.selected()
	if !ok {
		return "", false
	}
	return sess.ID, true
}

func (h focusHost) PaneRows() int { return h.m.focusPaneRows() }

func (h focusHost) Bindings(pressed string) uifocus.Bindings {
	keys := h.m.services.keys
	bindings := uifocus.Bindings{
		Detach:    keys.Binding(keybind.Detach).Has(pressed),
		Editor:    keys.Binding(keybind.Editor).Has(pressed),
		Review:    keys.Binding(keybind.Review).Has(pressed),
		Saved:     keys.Binding(keybind.SavedPrompt).Has(pressed),
		ArrowStep: h.m.prefs.arrowStep,
	}
	if sess, ok := h.m.selected(); ok {
		bindings.AtInputHead = h.m.caretAtInputStart(sess.ID, sess.Tool)
	}
	return bindings
}

func (h focusHost) RuntimeReady() bool { return h.m.focus.runtime.Ready() }

func (h focusHost) Queued(id string, write uifocus.Write) tea.Cmd {
	sess, ok := h.m.sessionByID(id)
	if !ok {
		return nil
	}
	h.m.dispatchInput(focusruntime.Request{
		Kind:    focusruntime.Kind(write.Kind),
		Session: sess,
		Command: write.Command,
		Args:    write.Args,
		Text:    write.Text,
		Submit:  write.Submit,
		Echo:    write.Echo,
	})
	return h.m.nextEffectCmd()
}

func (h focusHost) Reported(report string, echo bool) { h.m.sendFocusReport(report, echo) }

func (h focusHost) Copied(request uifocus.CopyRequest) tea.Cmd {
	return copyTextCmd(request.Text, func(chars int) tea.Msg {
		return uifocus.CopiedMessage{Generation: request.Generation, Chars: chars}
	})
}

func (h focusHost) Opened(url string) (tea.Model, tea.Cmd) { return h.m, openLinkCmd(url) }

func (h focusHost) Region(request *uifocus.RegionRequest) tea.Cmd {
	if request == nil {
		return nil
	}
	return h.m.focusRegionRequestCmd(*request)
}

func (h focusHost) Editor() (tea.Model, tea.Cmd) { return h.m.openEditor() }

func (h focusHost) Review(id string) (tea.Model, tea.Cmd) {
	cmd := h.m.openDiff()
	if h.m.base == baseReview {
		h.m.reviewNav.Set(uireviewsource.Return{Kind: uireviewsource.ReturnFocus, SessionID: id})
	}
	return h.m, cmd
}

func (h focusHost) Saved() tea.Cmd { return h.m.openFocusSaved() }

func (h focusHost) Leave() tea.Cmd { return h.m.leaveFocus() }

func (h focusHost) LeaveMode() tea.Cmd { return h.m.leaveFocusMode() }

// focusFrameHost is the root as a watcher frame and a scrollback viewport read
// it.
type focusFrameHost struct{ m *Model }

func (h focusFrameHost) Session() (string, bool) {
	sess, ok := h.m.selected()
	if !ok {
		return "", false
	}
	return sess.ID, true
}

func (h focusFrameHost) PaneRows() int { return h.m.focusPaneRows() }

func (h focusFrameHost) Region(request *uifocus.RegionRequest) tea.Cmd {
	if request == nil {
		return nil
	}
	return h.m.focusRegionRequestCmd(*request)
}

func (h focusFrameHost) UsePreview(preview string) { h.m.workspace.preview = preview }

// focusEffects is the root as the focus runtime's completions reach it.
type focusEffects struct{ m *Model }

// Enter opens the focused surface over the session a probe confirmed.
func (h focusEffects) Enter(sess store.Session) tea.Cmd { return h.m.enterFocus(sess) }

// Focused moves the cursor to the session that asked, false when it has left
// the list.
func (h focusEffects) Focused(id string) bool { return h.m.focusSession(id) }

// TakenSinceDetach reports whether, since the detach, the user opened a view, a
// rail prompt or a resize, or asked to enter a session.
func (h focusEffects) TakenSinceDetach() bool {
	m := h.m
	if m.base != baseList || len(m.surfaces) > 0 || m.rail.Busy() {
		return true
	}
	for _, job := range m.effects.Main.Pending {
		switch job.Request.(type) {
		case focusruntime.ProbeRequest, uilifecycle.AttachRequest:
			return true
		}
	}
	return false
}

// Quitting reports a quit under way, which drops what a detach asked for.
func (h focusEffects) Quitting() bool { return h.m.effects.Quitting }

func (h focusEffects) Review(id string) tea.Cmd {
	cmd := h.m.openDiff()
	if h.m.base == baseReview {
		h.m.reviewNav.Set(uireviewsource.Return{Kind: uireviewsource.ReturnAttach, SessionID: id})
	}
	return cmd
}

func (h focusEffects) Editor(id string) tea.Cmd {
	_, cmd := h.m.openEditorWithReaderForReturn(newSystemTerminalDirectoryReader(h.m), id)
	return cmd
}

// Hold records a captured composer draft for the startup tick to judge.
func (h focusEffects) Hold(candidate focusruntime.Candidate) {
	h.m.holdTypedPrompt(uicapture.TypedPrompt{Session: candidate.Session, ID: candidate.ID, Text: candidate.Text, At: candidate.At})
}

// Recorded writes a prompt receipt onto its row.
func (h focusEffects) Recorded(id, prompt string) {
	for i := range h.m.workspace.sessions {
		if h.m.workspace.sessions[i].ID == id {
			h.m.workspace.sessions[i].LastPrompt = prompt
		}
	}
}

// InputLanded points the watcher at what the write reached, so its echo shows
// without waiting out the frame budget.
func (h focusEffects) InputLanded(id string) {
	m := h.m
	if m.effects.Quitting || !m.focused() || !m.focus.runtime.Ready() {
		return
	}
	if selected, ok := m.selected(); ok && selected.ID == id {
		m.focus.runtime.Retry()
		m.watchSelection()
	}
}

// focusProbeHost is the root as the entry probe's completion reaches it.
type focusProbeHost struct{ m *Model }

func (h focusProbeHost) ReportErr(text string) { h.m.reportErr(text) }

func (h focusProbeHost) Grounded(generation uint64) bool {
	return !h.m.effects.Quitting && generation == h.m.gens.foreground
}

func (h focusProbeHost) ListClear() bool { return h.m.listClear() }

func (h focusProbeHost) Enter(sessionID string) tea.Cmd {
	sess, ok := h.m.selected()
	if !ok || sess.ID != sessionID {
		return nil
	}
	return h.m.enterFocus(sess)
}

// focusDetachHost is the root as a detached session's request reaches it.
type focusDetachHost struct{ m *Model }

func (h focusDetachHost) ReportErr(text string) { h.m.reportErr(text) }

func (h focusDetachHost) Warn(text string) { h.m.reportWarn(text) }

func (h focusDetachHost) Refresh() { h.m.requestRefresh() }

func (h focusDetachHost) Quitting() bool { return h.m.effects.Quitting }

func (h focusDetachHost) TakenSinceDetach() bool { return focusEffects{h.m}.TakenSinceDetach() }

func (h focusDetachHost) Name(sessionID string) string {
	if sess, ok := h.m.sessionByID(sessionID); ok {
		return sess.Name
	}
	return sessionID
}

func (h focusDetachHost) Focused(sessionID string) bool { return h.m.focusSession(sessionID) }

func (h focusDetachHost) Review(sessionID string) tea.Cmd { return focusEffects{h.m}.Review(sessionID) }

func (h focusDetachHost) Editor(sessionID string) tea.Cmd { return focusEffects{h.m}.Editor(sessionID) }

// focusInputHost is the root as a finished write to a pane reaches it.
type focusInputHost struct{ m *Model }

func (h focusInputHost) ReportErr(text string) { h.m.reportErr(text) }

func (h focusInputHost) Landed(sessionID string) { focusEffects{h.m}.InputLanded(sessionID) }

func (h focusInputHost) Hold(candidate focusruntime.Candidate) { focusEffects{h.m}.Hold(candidate) }

func (h focusInputHost) Recorded(sessionID, prompt string) {
	focusEffects{h.m}.Recorded(sessionID, prompt)
}

var (
	_ uifocus.KeyHost         = focusHost{}
	_ uifocus.MouseHost       = focusHost{}
	_ uifocus.ScrollHost      = focusFrameHost{}
	_ uifocus.FrameHost       = focusFrameHost{}
	_ focusruntime.ProbeHost  = focusProbeHost{}
	_ focusruntime.DetachHost = focusDetachHost{}
	_ focusruntime.InputHost  = focusInputHost{}
)
