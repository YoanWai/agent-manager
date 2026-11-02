package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uiquicksend "github.com/YoanWai/agent-manager/internal/ui/quick/send"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	tea "github.com/charmbracelet/bubbletea"
)

// quickBarHost is the root as the quick bar sees it.
type quickBarHost struct{ spawnHost }

func (h quickBarHost) Prompt() spawn.Prompt {
	return promptBox{c: &h.m.quick().Composer, target: composer.Quick, host: h.m}
}

func (h quickBarHost) Target() (uiquick.Target, bool) {
	if row, ok := h.m.remoteQuickTarget(); ok {
		return uiquick.Target{Group: row.Group, IsGroup: row.Kind != uiconnections.SessionRow}, true
	}
	entry, ok := h.m.selectedRow()
	if !ok {
		return uiquick.Target{}, false
	}
	return uiquick.Target{Group: entry.group, IsGroup: entry.isGroup, Env: h.m.groupEnv(entry.group)}, true
}

func (m *Model) quickWorktreeOn() bool {
	if _, remote := m.remoteTarget(); remote {
		return false
	}
	capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
	if !known || !capable {
		return false
	}
	if q := m.quick(); q.WorktreeTouched() {
		return q.Worktree()
	}
	return m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
}

// quickTargetGroup is the group a quick spawn would land in: the selected
// group, or the group holding the selected session.
func (m *Model) quickTargetGroup() string {
	entry, ok := m.selectedRow()
	if !ok {
		return ""
	}
	if entry.isGroup {
		return entry.group
	}
	return entry.sess.Group
}

// quickTargetDir is the directory a quick spawn would launch in, resolved
// the same way quickSpawn resolves it.
func (m *Model) quickTargetDir() string {
	group := m.quickTargetGroup()
	return m.capturedAbsolutePath(m.workspace.groupPaths[group], m.capturedGroupDefaultDir(group))
}

func (m *Model) quickTool() string { return m.quick().Tool() }

func (m *Model) quickSpawning() bool { return m.quick().Spawning(m.quickBarHost()) }

// closeAfterSend is the bar's stored setting: a confirmed send takes it
// down.
func (m *Model) closeAfterSend() bool {
	return spawn.CloseAfterSend(m.settings.Cache.Value(uisettings.QuickCloseSetting))
}

func (m *Model) quickBarHost() quickBarHost { return quickBarHost{spawnHost{m: m}} }

// The bar's effects: what a key or click asks of the root once the package
// has decided.

func (h quickBarHost) MoveCursor(delta int) tea.Cmd {
	return tea.Batch(h.m.moveCursor(delta), h.m.quickWorktreeProbeCmd(false))
}

func (h quickBarHost) ToggleWorktree() tea.Cmd { return h.m.toggleQuickWorktree() }

func (h quickBarHost) LoadCatalog(toolName string) tea.Cmd { return h.m.ensureCatalog(toolName) }

func (h quickBarHost) Close() { h.m.closeQuick() }

func (h quickBarHost) LoadSaved() tea.Cmd {
	return uipersist.SavedCmd(h.m.services.store, uipersist.SavedForQuick, uint64(h.m.quick().Gen), 0)
}

func (h quickBarHost) Send(text string) tea.Cmd {
	if cmd, taken := h.m.remoteQuickSend(text); taken {
		return cmd
	}
	entry, _ := h.m.selectedRow()
	q := h.m.quick()
	if !h.m.dispatchQuickSend(uiquicksend.SendRequest{
		Session:        entry.sess,
		ComposerGen:    q.Gen,
		Draft:          q.Input.Value(),
		Text:           text,
		CloseAfterSend: q.CloseAfterSend,
		Images:         q.Attachments,
	}) {
		return nil
	}
	h.m.clearErr()
	return h.m.nextEffectCmd()
}

func (h quickBarHost) Spawn(request spawn.Request) tea.Cmd {
	if cmd, taken := h.m.remoteQuickSpawn(request); taken {
		return cmd
	}
	return h.m.dispatchQuickSpawn(h.m.quickSpawnRequest(request), systemDirectoryPreflight{git: h.m.services.gitDrv})
}

var _ uiquick.Effects = quickBarHost{}
