package ui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// formWorktreeOn is the worktree state the form shows and spawns with: the
// toggle, unless the chosen directory cannot host a worktree.
func (m *Model) formWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.formSpawnDir())
	return m.form.Worktree() && known && capable
}

// toggleFormWorktree flips the toggle, or explains why the chosen
// directory rules a worktree out.
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
	m.form.SetWorktree(!m.form.Worktree())
	return nil
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
	return m.requestWorktreeProbe(worktreeProbeForm, m.form.prompt.gen, m.formSpawnDir(), toggleOn, m.form.Worktree())
}

func (m *Model) quickWorktreeProbeCmd(toggleOn bool) tea.Cmd {
	return m.requestWorktreeProbe(worktreeProbeQuick, m.quick.gen, m.quickTargetDir(), toggleOn, m.quick.Worktree())
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
		if m.form.Worktree() != request.from {
			return m, nil
		}
		m.form.SetWorktree(!request.from)
	} else {
		if m.quick.Worktree() != request.from {
			return m, nil
		}
		m.quick.SetWorktree(!request.from)
	}
	return m, nil
}

// worktreeUnavailable is what the worktree toggle reads when the target
// directory cannot host one.
const worktreeUnavailable = "unavailable (not a git repo)"

// worktreeLookupTTL bounds how long a directory's repo answer is reused.
// The quick bar stays open across prompts, so a directory git-initialised
// meanwhile has to be seen without closing it, while a frame that repaints
// on every keystroke must not shell out to git each time.
const worktreeLookupTTL = 2 * time.Second

// forgetWorktreeCapability drops the memo so the next look is a fresh one.
// Opening the form or the quick bar calls it.
func (m *Model) forgetWorktreeCapability() {
	m.ledger.worktreeRepos = nil
}
