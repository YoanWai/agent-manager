package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	tea "github.com/charmbracelet/bubbletea"
)

// confirmInstall asks before this build's release goes to a host whose
// agent-manager is missing or too old. A dev build has no release to send,
// so it says so instead of asking.
func (m *Model) confirmInstall(host string) {
	if m.ssh.installing[host] {
		m.reportWarn("already installing agent-manager on " + host)
		return
	}
	tag, err := remote.InstallTag(m.update.version)
	if err != nil {
		m.reportErr(host + ": " + err.Error())
		return
	}
	m.confirm.confirmTarget = confirmTarget{
		action: actionInstall,
		label: "install agent-manager " + tag + " on " + host + "? This machine downloads the release, checks its checksum, " +
			"and copies it over SSH to ~/.local/bin there. Nothing runs with sudo.",
		remote: &remoteRequest{op: remoteInstall, host: host, name: tag, version: m.update.version, gen: m.ssh.gen},
	}
	m.mode = modeConfirmDelete
}

// queueInstall is the confirmed install. It goes ahead on a host the last
// refresh marked offline, since that refresh is what asked for it.
func (m *Model) queueInstall(request remoteRequest) tea.Cmd {
	m.ssh.installing[request.host] = true
	delete(m.ssh.installFailed, request.host)
	m.rebuildRows()
	m.reportWarn("installing agent-manager " + request.name + " on " + request.host + "…")
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// applyInstall takes the refresh the install ran once it finished, so the
// row comes back online without waiting out the failed host's retry wait.
func (m *Model) applyInstall(request remoteRequest, result remoteEffectResult, err error) tea.Cmd {
	delete(m.ssh.installing, request.host)
	if err != nil {
		m.ssh.installFailed[request.host] = err.Error()
		m.rebuildRows()
		m.reportErr("install failed: " + err.Error())
		return nil
	}
	if request.gen == m.ssh.gen {
		m.ssh.hosts[request.host] = result.state
	}
	m.rebuildRows()
	done := "installed agent-manager " + request.name + " on " + request.host
	switch advice := result.installed.PathAdvice(); {
	case advice != "":
		m.reportWarn(done + "; " + advice)
	case !result.state.OK && result.state.Err != nil:
		m.reportWarn(done + ", but " + result.state.Err.Error())
	default:
		m.reportDone(done)
	}
	return m.syncRemotePreview()
}

// installFact is the connection detail's line about installing there: the
// key that offers it, the one running, or why a dev build cannot.
func (m *Model) installFact(host string) (string, bool) {
	switch {
	case m.ssh.installing[host]:
		return "installing agent-manager there…", true
	case !m.ssh.installable(host):
		return "", false
	}
	tag, err := remote.InstallTag(m.update.version)
	if err != nil {
		return "this build is not a release; install agent-manager there by hand", true
	}
	offer := "press " + m.listGlyph(keybind.Restore) + " to install agent-manager " + tag + " there"
	if failed := m.ssh.installFailed[host]; failed != "" {
		return "the last install failed (" + failed + "); " + offer, true
	}
	return offer, true
}
