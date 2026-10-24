package ui

import (
	"slices"

	"github.com/YoanWai/agent-manager/internal/store"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	tea "github.com/charmbracelet/bubbletea"
)

// quickSendRoot is the root as a finished send reaches it.
type quickSendRoot struct{ m *Model }

func (r quickSendRoot) ClearAccepted(composerGen int, draft string, closeAfterSend bool) {
	r.m.clearAccepted(composerGen, draft, closeAfterSend)
}

func (r quickSendRoot) RestoreImages(request uiquick.SendRequest) tea.Cmd {
	return r.m.returnQuickSendImages(request)
}

func (r quickSendRoot) ReportErr(text string) { r.m.reportErr(text) }

func (r quickSendRoot) ClearErr() { r.m.clearErr() }

func (r quickSendRoot) MarkSent(session store.Session, text string) {
	for i := range r.m.workspace.sessions {
		if uiquick.SameTarget(r.m.workspace.sessions[i], session) {
			r.m.workspace.sessions[i].Acked = false
			r.m.workspace.sessions[i].LastPrompt = text
		}
	}
}

var _ uiquick.Root = quickSendRoot{}

func (m *Model) dispatchQuickSend(request uiquick.SendRequest) bool {
	jobs := append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		pending, ok := job.Request.(uiquick.SendRequest)
		if ok && pending.ComposerGen == request.ComposerGen {
			m.reportErr("this prompt is already being sent")
			return false
		}
	}
	request.Images = slices.Clone(request.Images)
	m.quick().Attachments = nil
	m.enqueueEffect(request, 0, false)
	return true
}

// clearAccepted empties the bar a confirmed send came from, unless its
// prompt has changed since, and closes it when the setting asks.
func (m *Model) clearAccepted(composerGen int, draft string, closeAfterSend bool) {
	if !m.quickHolds(composerGen, draft) {
		return
	}
	q := m.quick()
	q.Input.SetValue("")
	q.Saved = nil
	if closeAfterSend {
		m.closeQuick()
	}
}

func (m *Model) returnQuickSendImages(request uiquick.SendRequest) tea.Cmd {
	if q, ok := m.quickFor(request.ComposerGen); ok {
		q.Attachments = append(request.Images, q.Attachments...)
		return nil
	}
	if len(request.Images) == 0 {
		return nil
	}
	images := slices.Clone(request.Images)
	return func() tea.Msg {
		uiinstall.DropImages(images)
		return nil
	}
}

// quickServices is the effect services as a quick send reaches them.
func (s effectServices) quickServices() uiquick.Services {
	exists, sendText := s.quickSessionExists, s.quickSendText
	if exists == nil {
		exists = func(driver *tmux.Driver, id string) (bool, error) { return driver.SessionExists(id) }
	}
	if sendText == nil {
		sendText = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
			return driver.SendTextResult(id, text)
		}
	}
	return uiquick.Services{Store: s.store, Config: s.cfg, Driver: s.driver, SessionExists: exists, SendText: sendText}
}
