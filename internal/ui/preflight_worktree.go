package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type worktreeProbeTarget uint8

const (
	worktreeProbeForm worktreeProbeTarget = iota
	worktreeProbeQuick
)

type worktreeProbeRequest struct {
	target     worktreeProbeTarget
	generation int
	sequence   uint64
	dir        string
	toggle     bool
	from       bool
}

type worktreeProbeMsg struct {
	request worktreeProbeRequest
	capable bool
}

func worktreeProbeCmd(request worktreeProbeRequest, reader directoryPreflight) tea.Cmd {
	return func() tea.Msg {
		return worktreeProbeMsg{request: request, capable: reader.repoCapable(request.dir)}
	}
}

func (m *Model) cachedWorktreeCapability(dir string) (bool, bool) {
	answer, ok := m.ledger.worktreeRepos[dir]
	if !ok || answer.at.IsZero() || time.Since(answer.at) >= worktreeLookupTTL {
		return false, false
	}
	return answer.capable, true
}

func (m *Model) requestWorktreeProbe(target worktreeProbeTarget, generation int, dir string, toggle bool, from bool) tea.Cmd {
	m.gens.worktreeProbe++
	request := worktreeProbeRequest{
		target: target, generation: generation, sequence: m.gens.worktreeProbe,
		dir: dir, toggle: toggle, from: from,
	}
	return worktreeProbeCmd(request, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) formWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	return m.requestWorktreeProbe(worktreeProbeForm, m.form.prompt.gen, m.formSpawnDir(), toggleOn, m.form.worktree)
}

func (m *Model) quickWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	return m.requestWorktreeProbe(worktreeProbeQuick, m.quick.gen, m.quickTargetDir(), toggleOn, m.quick.worktree)
}

func (m *Model) handleWorktreeProbe(msg worktreeProbeMsg) (tea.Model, tea.Cmd) {
	request := msg.request
	if request.sequence != m.gens.worktreeProbe {
		return m, nil
	}
	switch request.target {
	case worktreeProbeForm:
		if m.mode != modeForm || m.form.prompt.gen != request.generation || m.formSpawnDir() != request.dir {
			return m, nil
		}
	case worktreeProbeQuick:
		if !m.quick.active || m.quick.gen != request.generation || m.quickTargetDir() != request.dir {
			return m, nil
		}
	default:
		return m, nil
	}
	if m.ledger.worktreeRepos == nil {
		m.ledger.worktreeRepos = make(map[string]repoAnswer)
	}
	m.ledger.worktreeRepos[request.dir] = repoAnswer{capable: msg.capable, at: time.Now()}
	if !request.toggle {
		return m, nil
	}
	if !msg.capable {
		m.reportErr("worktree sessions need a git repository: " + request.dir + " is not one")
		return m, nil
	}
	m.clearErr()
	if request.target == worktreeProbeForm {
		if m.form.worktree != request.from {
			return m, nil
		}
		m.form.worktree = !request.from
		m.form.worktreeAuto = false
		m.form.defaultsTouched = true
	} else {
		if m.quick.worktree != request.from {
			return m, nil
		}
		m.quick.worktree = !request.from
		m.quick.worktreeTouched = true
		m.quick.defaultsTouched = true
	}
	return m, nil
}
