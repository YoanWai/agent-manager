package ui

import (
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

func (m *Model) cachedDefaultToolSelection() ([]string, int) {
	return m.cachedToolSelection(m.cachedHiddenTools(), "")
}

func (m *Model) cachedSpawnToolSelection() ([]string, int) {
	names, index := m.cachedDefaultToolSelection()
	for i, name := range names {
		if name == m.ledger.lastSpawnTool {
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
		if name == m.ledger.lastSpawnTool {
			return m.ledger.lastSpawnWorktree
		}
	}
	return m.settings.cache.value(uisettings.WorktreeSetting) == "on"
}

func (m *Model) cachedEnabledToolNames() []string {
	return m.services.cfg.EnabledAgentTools(m.cachedHiddenTools())
}

// groupBase is the ref a spawn into group branches from: the nearest
// ancestor group's choice, or "" to detect the repo's default branch.
func (m *Model) groupBase(group string) string {
	for g := group; g != ""; g = parentGroup(g) {
		if base := m.workspace.groupBases[g]; base != "" {
			return base
		}
	}
	return ""
}

// groupBaseTarget names the base picker a branch read answers.
type groupBaseTarget uint8

const (
	groupBaseForm groupBaseTarget = iota
	groupBaseRename
)

// baseRefsTTL bounds how long a repo's branch list steps a base picker
// without a fresh read, so a held arrow steps at key speed.
const baseRefsTTL = 10 * time.Second

type baseRefsAnswer struct {
	refs []string
	at   time.Time
}

// groupBaseStepMsg is a base picker's step, answered off the update path
// with the branches of dir. It lands only on the picker that asked, while
// it still shows from.
type groupBaseStepMsg struct {
	target groupBaseTarget
	gen    uint64
	dir    string
	from   string
	delta  int
	refs   []string
	err    error
}

// stepGroupBase moves a group's base choice through auto and the branches
// of the repo at dir. Branches read within baseRefsTTL step at once;
// otherwise the read runs as a command and the step lands with it.
func (m *Model) stepGroupBase(target groupBaseTarget, gen uint64, dir, current string, delta int) (string, tea.Cmd) {
	if answer, ok := m.ledger.baseRefs[dir]; ok && time.Since(answer.at) < baseRefsTTL {
		m.clearErr()
		return stepBaseChoice(answer.refs, current, delta), nil
	}
	driver := m.services.gitDrv
	if driver == nil {
		m.reportErr("a group base needs git installed")
		return current, nil
	}
	return current, func() tea.Msg {
		refs, err := driver.BranchRefs(dir)
		return groupBaseStepMsg{target: target, gen: gen, dir: dir, from: current, delta: delta, refs: refs, err: err}
	}
}

func stepBaseChoice(refs []string, current string, delta int) string {
	choices := append([]string{""}, refs...)
	at := max(slices.Index(choices, current), 0)
	return choices[(at+delta+len(choices))%len(choices)]
}

func (m *Model) handleGroupBaseStep(msg groupBaseStepMsg) {
	var base *string
	switch msg.target {
	case groupBaseForm:
		if m.mode != modeGroupForm || uint64(m.groupForm.Gen()) != msg.gen || m.groupFormDir() != msg.dir {
			return
		}
		base = &m.groupForm.base
	case groupBaseRename:
		if m.mode != modeRename || m.gens.dialog != msg.gen || m.renameGroupDir() != msg.dir {
			return
		}
		base = &m.rename.base
	default:
		return
	}
	if msg.err != nil {
		m.reportErr("group base: " + msg.err.Error())
		return
	}
	if m.ledger.baseRefs == nil {
		m.ledger.baseRefs = map[string]baseRefsAnswer{}
	}
	m.ledger.baseRefs[msg.dir] = baseRefsAnswer{refs: msg.refs, at: time.Now()}
	if *base != msg.from {
		return
	}
	m.clearErr()
	*base = stepBaseChoice(msg.refs, msg.from, msg.delta)
}

// baseFetchInterval keeps a burst of spawns into one repo to one fetch.
const baseFetchInterval = time.Minute

type baseFetchKey struct{ dir, override string }

// baseFetch is one refresh of a spawn's base: when it started, the default
// branch the repo resolved to, and how the fetch ended.
type baseFetch struct {
	at       time.Time
	resolved bool
	detected string
	fetched  bool
	err      error
}

type baseFetchedMsg struct {
	key      baseFetchKey
	detected string
	fetched  bool
	err      error
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
	key := baseFetchKey{dir: dir, override: m.groupBase(group)}
	if last, seen := m.ledger.baseFetches[key]; seen && time.Since(last.at) < baseFetchInterval {
		return nil
	}
	if m.ledger.baseFetches == nil {
		m.ledger.baseFetches = map[baseFetchKey]baseFetch{}
	}
	m.ledger.baseFetches[key] = baseFetch{at: time.Now()}
	driver := m.services.gitDrv
	return func() tea.Msg {
		return baseFetchedMsg{key: key, detected: driver.DefaultBase(dir)}
	}
}

// recordBaseFetch keeps what a step of a base refresh found, and starts the
// fetch once the resolving step is in.
func (m *Model) recordBaseFetch(msg baseFetchedMsg) tea.Cmd {
	fetch, ok := m.ledger.baseFetches[msg.key]
	if !ok {
		return nil
	}
	fetch.resolved, fetch.detected = true, msg.detected
	if msg.fetched {
		fetch.fetched, fetch.err = true, msg.err
	}
	m.ledger.baseFetches[msg.key] = fetch
	if msg.fetched || m.prefs.baseFetchOff {
		return nil
	}
	driver, key := m.services.gitDrv, msg.key
	return func() tea.Msg {
		err := driver.FetchBase(key.dir, key.override)
		return baseFetchedMsg{key: key, detected: driver.DefaultBase(key.dir), fetched: true, err: err}
	}
}

// pendingWorktreeSpawn is the directory and group of the worktree spawn
// the New Session form or the quick bar is set to make.
func (m *Model) pendingWorktreeSpawn() (dir, group string, ok bool) {
	switch {
	case m.mode == modeForm && m.formWorktreeOn():
		return m.formSpawnDir(), m.selectedGroupPath(), true
	case m.mode == modeList && m.quick.active && m.quickSpawning() && m.quickWorktreeOn():
		return m.quickTargetDir(), m.quickTargetGroup(), true
	}
	return "", "", false
}

// spawnBaseLabel names the ref a worktree spawn into dir branches from,
// where that choice came from, and how fetching it went.
func (m *Model) spawnBaseLabel(dir, group string) string {
	override := m.groupBase(group)
	fetch := m.ledger.baseFetches[baseFetchKey{dir: dir, override: override}]
	label := valueStyle().Render(override) + subtleStyle().Render(" (group)")
	if override == "" {
		switch {
		case !fetch.resolved:
			label = subtleStyle().Render("…")
		case fetch.detected == "":
			label = valueStyle().Render("HEAD") + subtleStyle().Render(" (auto)")
		default:
			label = valueStyle().Render(fetch.detected) + subtleStyle().Render(" (auto)")
		}
	}
	switch {
	case m.prefs.baseFetchOff:
	case !fetch.fetched:
		label += subtleStyle().Render(" · fetching")
	case fetch.err != nil:
		label += subtleStyle().Render(" · fetch failed")
	}
	return label
}

func (m *Model) cachedHiddenTools() map[string]bool {
	return m.settings.cachedHiddenTools(m.services.cfg)
}

func (s *settingsFeature) cachedHiddenTools(cfg config.Config) map[string]bool {
	return uisettings.Tools{Known: knownTools(cfg)}.KnownHidden(s.cache.hidden)
}

func (m *Model) cachedToolSelection(hidden map[string]bool, preferred string) ([]string, int) {
	return m.settings.cachedToolSelection(m.services.cfg, hidden, preferred)
}

func (s *settingsFeature) cachedToolSelection(cfg config.Config, hidden map[string]bool, preferred string) ([]string, int) {
	names := cfg.EnabledAgentTools(hidden)
	if preferred == "" {
		preferred = s.cache.value(uisettings.DefaultToolSetting)
	}
	return names, uisettings.ToolIndex(names, preferred)
}
