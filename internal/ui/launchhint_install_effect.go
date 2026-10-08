package ui

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	tea "github.com/charmbracelet/bubbletea"
)

var (
	installHomeDir        = os.UserHomeDir
	installCheckInstalled = config.CheckInstalled
	installSendText       = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
		return driver.SendTextResult(id, text)
	}
	installSessionExists = func(driver *tmux.Driver, id string) (bool, error) {
		return driver.SessionExists(id)
	}
)

// installStartRequest freezes the setup dialog, shell selection, placement,
// pane geometry, retry, and image ownership before any filesystem, store, or
// tmux work begins.
type installStartRequest struct {
	gen         uint64
	id          string
	command     string
	binary      string
	toolName    string
	group       string
	pane        sessioncmd.PaneSize
	effectRetry effectRequest
	images      []imageAttachment
}

func (installStartRequest) effectRequest() {}

// installStartResult carries a committed shell row even when a later label
// or send step fails. install is set only after the command was typed.
type installStartResult struct {
	session store.Session
	install *pendingInstall
}

func (installStartResult) effectResult() {}

type installSettleRequest struct {
	install pendingInstall
}

func (installSettleRequest) effectRequest() {}

type installSettleState uint8

const (
	installStillRunning installSettleState = iota
	installShellGone
	installExited
	installBinaryMissing
	installReady
)

type installSettleResult struct {
	sessionID string
	state     installSettleState
	code      string
}

func (installSettleResult) effectResult() {}

// installImageCleanupCmd owns a frozen copy of paths that no dialog or
// retry still references. The command performs the file deletes without
// retaining or reading the Model.
func installImageCleanupCmd(images []imageAttachment) tea.Cmd {
	owned := append([]imageAttachment(nil), images...)
	return func() tea.Msg {
		dropImages(owned)
		return nil
	}
}

// startInstall admits the setup dialog's install onto the effect lane,
// stamped with the dialog generation, a new shell row id, the shell tool,
// the placement group and the pane geometry. It refuses while another
// install is still starting.
func (m *Model) startInstall(request installStartRequest) bool {
	for _, job := range append([]*effectJob{m.effects.active}, m.effects.pending...) {
		if job == nil {
			continue
		}
		if starting, ok := job.request.(installStartRequest); ok {
			m.reportErr("an install is already starting for " + starting.binary)
			return false
		}
	}
	toolName, _ := m.shellTool()
	w, h := m.paneTargetSize()
	request.gen = m.gens.dialog
	request.id = newID()
	request.toolName = toolName
	request.group = m.contextGroup()
	request.pane = sessioncmd.PaneSize{Width: w, Height: h}
	m.enqueueEffect(request, 0, false)
	return true
}

func (s effectServices) runInstallStart(request installStartRequest) (effectResult, error) {
	result := installStartResult{}
	if s.lifecycle == nil || s.hooks == nil || s.driver == nil {
		return result, errors.New("install requires lifecycle, hook, and tmux services")
	}
	home, err := s.installHomeDir()
	if err != nil {
		return result, err
	}
	tool, ok := s.cfg.Tools[request.toolName]
	if !ok {
		return result, fmt.Errorf("shell tool %s is no longer configured", request.toolName)
	}
	sess := store.Session{
		ID:     request.id,
		Name:   "install-" + request.binary,
		Tool:   request.toolName,
		Cwd:    home,
		Group:  request.group,
		Status: status.Starting,
	}
	statusFile := s.hooks.InstallStatusFile(sess.ID)
	script, err := s.hooks.WriteInstallScript(sess.ID, installScript(request.command, statusFile))
	if err != nil {
		return result, err
	}
	launched, err := s.lifecycle.Launch(sessioncmd.LaunchRequest{
		Session:     sess,
		Tool:        tool,
		BaseCommand: tool.Command,
		Pane:        request.pane,
	})
	if err != nil {
		removeInstallFiles(statusFile, script)
		return result, err
	}
	result.session = launched.Session
	if launched.LabelError != nil {
		removeInstallFiles(statusFile, script)
		return result, launched.LabelError
	}
	install := &pendingInstall{
		sessionID:   sess.ID,
		name:        sess.Name,
		binary:      request.binary,
		statusFile:  statusFile,
		script:      script,
		effectRetry: request.effectRetry,
		images:      request.images,
	}
	sendResult, sendErr := s.installSendText(s.driver, sess.ID, "sh "+tmux.ShellQuote(script))
	if sendErr == nil && sendResult.Phase != tmux.SendPhaseSubmitted {
		sendErr = errors.New("installer command was not confirmed submitted")
	}
	if sendErr != nil && !sendResult.PasteMayHaveStarted() {
		// The durable shell remains open like any other terminal. Only the
		// command files are retired because this install never began.
		removeInstallFiles(statusFile, script)
		return result, sendErr
	}
	result.install = install
	if sendErr != nil {
		return result, fmt.Errorf(
			"%s install may be running in %s; inspect that shell before retrying or quitting; Agent Manager will not send it again: %w",
			request.binary, sess.Name, sendErr,
		)
	}
	return result, nil
}

func (s effectServices) runInstallSettle(request installSettleRequest) (effectResult, error) {
	install := request.install
	result := installSettleResult{sessionID: install.sessionID, state: installStillRunning}
	data, err := os.ReadFile(install.statusFile)
	if errors.Is(err, fs.ErrNotExist) {
		exists, existsErr := s.installSessionExists(s.driver, install.sessionID)
		if existsErr != nil {
			return result, fmt.Errorf("check installer shell %s liveness: %w", install.name, existsErr)
		}
		if exists {
			return result, nil
		}
		removeInstallFiles(install.statusFile, install.script)
		dropImages(install.images)
		result.state = installShellGone
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("read installer status for %s: %w", install.name, err)
	}
	// The shell creates the file before printf fills it. An empty file is
	// observed again on a later poll rather than read as a successful exit.
	code := strings.TrimSpace(string(data))
	if code == "" {
		return result, nil
	}
	if code != "0" {
		removeInstallFiles(install.statusFile, install.script)
		dropImages(install.images)
		result.state = installExited
		result.code = code
		return result, nil
	}
	if err := s.installCheckInstalled(install.binary); err != nil {
		var missing config.MissingToolError
		if errors.As(err, &missing) {
			removeInstallFiles(install.statusFile, install.script)
			dropImages(install.images)
			result.state = installBinaryMissing
			return result, nil
		}
		return result, fmt.Errorf("%s installer finished, and looking for it failed: %w", install.binary, err)
	}
	removeInstallFiles(install.statusFile, install.script)
	result.state = installReady
	if install.effectRetry == nil {
		dropImages(install.images)
	}
	return result, nil
}

func (m *Model) applyInstallStart(request installStartRequest, result installStartResult, err error) tea.Cmd {
	if result.session.ID != "" {
		m.markFreshPane(result.session.ID)
		if m.ledger.launched == nil {
			m.ledger.launched = map[string]time.Time{}
		}
		m.ledger.launched[result.session.ID] = time.Now()
		known := false
		for _, sess := range m.workspace.sessions {
			if sess.ID == result.session.ID {
				known = true
				break
			}
		}
		if !known {
			m.workspace.sessions = append(m.workspace.sessions, result.session)
		}
		m.rebuildRows()
	}
	stillOpen := m.mode == modeLaunchHint && m.gens.dialog == request.gen
	if err != nil {
		if result.install != nil {
			m.launchHint.install = result.install
			if stillOpen {
				m.launchHint.fix = launchFix{}
				m.mode = modeList
				m.rail.ClearStatusFilter()
				m.focusSession(result.session.ID)
			}
			m.reportErr(err.Error())
			return m.refreshCmd()
		}
		var cleanup tea.Cmd
		if stillOpen {
			m.launchHint.fix.images = append(request.images, m.launchHint.fix.images...)
		} else {
			cleanup = installImageCleanupCmd(request.images)
		}
		m.reportErr(err.Error())
		return cleanup
	}
	m.launchHint.install = result.install
	if stillOpen {
		m.launchHint.fix = launchFix{}
		m.mode = modeList
		m.rail.ClearStatusFilter()
		m.focusSession(result.session.ID)
	}
	m.reportDone("installing " + request.binary)
	return m.refreshCmd()
}

func (m *Model) applyInstallSettle(request installSettleRequest, result installSettleResult, err error) tea.Cmd {
	if m.launchHint.install == nil || m.launchHint.install.sessionID != result.sessionID {
		return nil
	}
	if result.state == installStillRunning {
		if err != nil {
			m.reportErr(err.Error())
		}
		return nil
	}
	m.launchHint.install = nil
	install := request.install
	if err != nil {
		m.reportErr(err.Error())
		return nil
	}
	switch result.state {
	case installShellGone:
		return nil
	case installExited:
		m.reportErr(fmt.Sprintf("%s install exited with status %s; its output is in %s", install.binary, result.code, install.name))
	case installBinaryMissing:
		m.reportErr(fmt.Sprintf("%s installer finished, but %s is still not on PATH; add its directory to PATH, the installer's output names it", install.binary, install.binary))
	case installReady:
		if install.effectRetry != nil {
			m.enqueueEffect(install.effectRetry, 0, false)
			m.reportDone(install.binary + " installed; retry queued")
			return nil
		}
		m.reportDone(install.binary + " installed")
	}
	return nil
}
