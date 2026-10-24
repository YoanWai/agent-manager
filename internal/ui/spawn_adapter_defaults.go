package ui

import (
	"time"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) cachedDefaultToolSelection() ([]string, int) {
	return m.cachedToolSelection(m.cachedHiddenTools(), "")
}

func (m *Model) cachedSpawnToolSelection() ([]string, int) {
	names, index := m.cachedDefaultToolSelection()
	for i, name := range names {
		if name == m.ledger.spawner.LastSpawnTool {
			return names, i
		}
	}
	return names, index
}

func (m *Model) cachedSpawnWorktreeDefault(group string) bool {
	for g := group; g != ""; g = parentGroup(g) {
		switch m.workspace.groupWorktrees[g] {
		case "on":
			return true
		case "off":
			return false
		}
	}
	for _, name := range m.cachedEnabledToolNames() {
		if name == m.ledger.spawner.LastSpawnTool {
			return m.ledger.spawner.LastSpawnWorktree
		}
	}
	return m.settings.Cache.Value(uisettings.WorktreeSetting) == "on"
}

func (m *Model) cachedEnabledToolNames() []string {
	return m.services.cfg.EnabledAgentTools(m.cachedHiddenTools())
}

// groupBase is the ref a spawn into group branches from: the nearest ancestor
// group's choice, or "" to detect the repo's default branch.
func (m *Model) groupBase(group string) string {
	for g := group; g != ""; g = parentGroup(g) {
		if base := m.workspace.groupBases[g]; base != "" {
			return base
		}
	}
	return ""
}

// stepGroupBase moves a group's base choice through auto and the branches of
// the repo at dir. Branches read within uilaunch.BaseRefsTTL step at once;
// otherwise the read runs as a command and the step lands with it.
func (m *Model) stepGroupBase(target uilaunch.BaseTarget, gen uint64, dir, current string, delta int) (string, tea.Cmd) {
	next, cmd, err := m.ledger.spawner.StepGroupBase(m.services.gitDrv, target, gen, dir, current, delta, time.Now())
	if err != nil {
		m.reportErr(err.Error())
		return current, nil
	}
	if cmd == nil {
		m.clearErr()
	}
	return next, cmd
}

func (m *Model) handleGroupBaseStep(msg uilaunch.BaseStepMsg) {
	var base *string
	switch msg.Target {
	case uilaunch.BaseForm:
		s, ok := surfaceOf[*uidialogs.GroupFormSurface](m)
		if !ok || uint64(s.Gen()) != msg.Gen || m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(s.Selected())) != msg.Dir {
			return
		}
		base = s.BaseRef()
	case uilaunch.BaseRename:
		s, ok := surfaceOf[*uidialogs.RenameSurface](m)
		if !ok || m.gens.dialog != msg.Gen || m.capturedAbsolutePath(s.Dir(), m.capturedGroupDefaultDir(parentGroup(s.Path()))) != msg.Dir {
			return
		}
		base = &s.Base
	default:
		return
	}
	if msg.Err != nil {
		m.reportErr("group base: " + msg.Err.Error())
		return
	}
	m.ledger.spawner.KeepBaseRefs(msg.Dir, msg.Refs, time.Now())
	if *base != msg.From {
		return
	}
	m.clearErr()
	*base = uilaunch.StepBaseChoice(msg.Refs, msg.From, msg.Delta)
}

// refreshSpawnBase resolves the base of the worktree spawn the form or the
// quick bar is set to make, for the form to show, then fetches it unless
// Settings turned that off. A spawn that beats the fetch branches from the
// last one.
func (m *Model) refreshSpawnBase() tea.Cmd {
	dir, group, ok := m.pendingWorktreeSpawn()
	if !ok {
		return nil
	}
	key := uilaunch.BaseFetchKey{Dir: dir, Override: m.groupBase(group)}
	if !m.ledger.spawner.ReserveBaseFetch(key, time.Now()) {
		return nil
	}
	return uilaunch.ResolveBaseCmd(m.services.gitDrv, key)
}

// recordBaseFetch keeps what a step of a base refresh found, and starts the
// fetch once the resolving step is in.
func (m *Model) recordBaseFetch(msg uilaunch.BaseFetchedMsg) tea.Cmd {
	return m.ledger.spawner.RecordBaseFetch(msg, m.services.gitDrv, m.prefs.baseFetchOff)
}

// pendingWorktreeSpawn is the directory and group of the worktree spawn the
// New Session form or the quick bar is set to make.
func (m *Model) pendingWorktreeSpawn() (dir, group string, ok bool) {
	switch {
	case m.form() != nil && m.formWorktreeOn():
		return m.formSpawnDir(), m.selectedGroupPath(), true
	case m.quick() != nil && m.quickSpawning() && m.quickWorktreeOn():
		return m.quickTargetDir(), m.quickTargetGroup(), true
	}
	return "", "", false
}

// spawnBaseLabel names the ref a worktree spawn into dir branches from, where
// that choice came from, and how fetching it went.
func (m *Model) spawnBaseLabel(dir, group string) string {
	override := m.groupBase(group)
	fetch := m.ledger.spawner.BaseFetch(dir, override)
	label := valueStyle().Render(override) + subtleStyle().Render(" (group)")
	if override == "" {
		switch {
		case !fetch.Resolved:
			label = subtleStyle().Render("…")
		case fetch.Detected == "":
			label = valueStyle().Render("HEAD") + subtleStyle().Render(" (auto)")
		default:
			label = valueStyle().Render(fetch.Detected) + subtleStyle().Render(" (auto)")
		}
	}
	switch {
	case m.prefs.baseFetchOff:
	case !fetch.Fetched:
		label += subtleStyle().Render(" · fetching")
	case fetch.Err != nil:
		label += subtleStyle().Render(" · fetch failed")
	}
	return label
}

func (m *Model) cachedHiddenTools() map[string]bool {
	return m.settings.KnownHidden(m.services.cfg)
}

func (m *Model) cachedToolSelection(hidden map[string]bool, preferred string) ([]string, int) {
	return m.settings.ToolSelection(m.services.cfg, hidden, preferred)
}
