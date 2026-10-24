package ui

import (
	"time"

	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

// formWorktreeOn is the worktree state the form shows and spawns with: the
// toggle, unless the chosen directory cannot host a worktree.
func (m *Model) formWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.formSpawnDir())
	return m.form().Worktree() && known && capable
}

// toggleFormWorktree flips the toggle, or explains why the chosen directory
// rules a worktree out.
func (m *Model) toggleFormWorktree() tea.Cmd {
	dir := m.formSpawnDir()
	capable, known := m.cachedWorktreeCapability(dir)
	if !known {
		return m.formWorktreeProbeCmd(true)
	}
	if !capable {
		m.reportErr("worktree sessions need a git repository: " + dir + " is not one")
		return nil
	}
	m.clearErr()
	f := m.form()
	f.SetWorktree(!f.Worktree())
	return nil
}

func (m *Model) cachedWorktreeCapability(dir string) (bool, bool) {
	return m.spawner.CachedWorktree(dir, time.Now())
}

func (m *Model) requestWorktreeProbe(target worktreeProbeTarget, generation int, dir string, toggle bool, from bool) tea.Cmd {
	m.gens.worktreeProbe++
	request := worktreeProbeRequest{
		Target: target, Generation: generation, Sequence: m.gens.worktreeProbe,
		Dir: dir, Toggle: toggle, From: from,
	}
	return uilaunch.ProbeCmd(request, preflightReader{reader: systemDirectoryPreflight{git: m.services.gitDrv}})
}

func (m *Model) formWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	f := m.form()
	if f.Remote.On() {
		return nil
	}
	return m.requestWorktreeProbe(worktreeProbeForm, m.formPromptFor().Gen, m.formSpawnDir(), toggleOn, f.Worktree())
}

func (m *Model) quickWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	if _, remote := m.remoteTarget(); remote {
		return nil
	}
	q := m.quick()
	return m.requestWorktreeProbe(worktreeProbeQuick, q.Gen, m.quickTargetDir(), toggleOn, q.Worktree())
}

// worktreeProbeTarget names the surface a probe answers.
type worktreeProbeTarget = uilaunch.ProbeTarget

const (
	worktreeProbeForm  = uilaunch.ProbeForm
	worktreeProbeQuick = uilaunch.ProbeQuick
)

// worktreeProbeRequest names one directory's probe, with the sequence that
// retires an older one.
type worktreeProbeRequest = uilaunch.ProbeRequest

// worktreeProbeMsg carries a directory's git-repo verdict.
type worktreeProbeMsg = uilaunch.ProbeMsg

func (m *Model) handleWorktreeProbe(msg worktreeProbeMsg) (tea.Model, tea.Cmd) {
	request := msg.Request
	if request.Sequence != m.gens.worktreeProbe {
		return m, nil
	}
	switch request.Target {
	case worktreeProbeForm:
		if _, ok := m.formFor(request.Generation); !ok || m.formSpawnDir() != request.Dir {
			return m, nil
		}
	case worktreeProbeQuick:
		if _, ok := m.quickFor(request.Generation); !ok || m.quickTargetDir() != request.Dir {
			return m, nil
		}
	default:
		return m, nil
	}
	m.spawner.RememberWorktree(request.Dir, msg.Capable, time.Now())
	if !request.Toggle {
		return m, nil
	}
	if !msg.Capable {
		m.reportErr("worktree sessions need a git repository: " + request.Dir + " is not one")
		return m, nil
	}
	m.clearErr()
	if request.Target == worktreeProbeForm {
		if m.form().Worktree() != request.From {
			return m, nil
		}
		m.form().SetWorktree(!request.From)
	} else {
		if m.quick().Worktree() != request.From {
			return m, nil
		}
		m.quick().SetWorktree(!request.From)
	}
	return m, nil
}

// worktreeUnavailable is what the worktree toggle reads when the target
// directory cannot host one.
const worktreeUnavailable = uilaunch.WorktreeUnavailable

// forgetWorktreeCapability drops the memo so the next look is a fresh one.
// Opening the form or the quick bar calls it.
func (m *Model) forgetWorktreeCapability() { m.spawner.ForgetWorktree() }
