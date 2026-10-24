package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
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
	return uiquick.Target{Group: entry.group, IsGroup: entry.isGroup}, true
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
