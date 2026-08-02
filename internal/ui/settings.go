package ui

import (
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultTool is the CLI quick spawn launches: the settings choice when it
// still exists in the config, else the first tool alphabetically. A store
// error still yields the fallback but is surfaced, never swallowed.
func (m *Model) defaultTool() string {
	names := sortedToolNames(m.cfg)
	if len(names) == 0 {
		return ""
	}
	chosen, err := m.store.Setting("default_tool")
	if err != nil {
		m.errBar.text = "reading default tool setting: " + err.Error()
		return names[0]
	}
	if chosen != "" {
		if _, ok := m.cfg.Tools[chosen]; ok {
			return chosen
		}
	}
	return names[0]
}

func (m *Model) defaultWorktree() bool {
	chosen, err := m.store.Setting(worktreeSetting)
	if err != nil {
		m.errBar.text = "reading worktree setting: " + err.Error()
		return false
	}
	return chosen == "on"
}

// groupWorktree resolves a group's spawn-in-worktree default: the nearest
// ancestor with an explicit choice wins, else the global setting.
func (m *Model) groupWorktree(group string) bool {
	for g := group; g != ""; g = parentGroup(g) {
		switch m.groupWorktrees[g] {
		case "on":
			return true
		case "off":
			return false
		}
	}
	return m.defaultWorktree()
}

// worktreeUnavailable is what the worktree toggle reads when the target
// directory cannot host one.
const worktreeUnavailable = "unavailable (not a git repo)"

// worktreeLookupTTL bounds how long a directory's repo answer is reused.
// The quick bar stays open across prompts, so a directory git-initialised
// meanwhile has to be seen without closing it, while a frame that repaints
// on every keystroke must not shell out to git each time.
const worktreeLookupTTL = 2 * time.Second

// worktreeCapable reports whether dir can host a worktree session: git
// installed, and the directory inside a repository. An umbrella directory
// that merely contains repos cannot, so the toggle is gated up front
// instead of failing once the prompt is already typed.
func (m *Model) worktreeCapable(dir string) bool {
	if m.gitDrv == nil || dir == "" {
		return false
	}
	if answer, seen := m.worktreeRepos[dir]; seen && time.Since(answer.at) < worktreeLookupTTL {
		return answer.capable
	}
	_, err := m.gitDrv.RepoRoot(dir)
	if m.worktreeRepos == nil {
		m.worktreeRepos = make(map[string]repoAnswer)
	}
	m.worktreeRepos[dir] = repoAnswer{capable: err == nil, at: time.Now()}
	return err == nil
}

// forgetWorktreeCapability drops the memo so the next look is a fresh one.
// Opening the form or the quick bar calls it.
func (m *Model) forgetWorktreeCapability() {
	m.worktreeRepos = nil
}

// defaultSplitLayout reports whether review mode should open in split
// (side-by-side) layout. Split is the default; a stored "unified" choice
// opts out. A store error is surfaced but still yields the split default.
func (m *Model) defaultSplitLayout() bool {
	chosen, err := m.store.Setting(diffLayoutSetting)
	if err != nil {
		m.errBar.text = "reading diff layout setting: " + err.Error()
		return true
	}
	return chosen != "unified"
}

// storedComfortableRows reads the persisted list density. Compact is the
// default; a stored "comfortable" choice gives every entry a second line.
func storedComfortableRows(st *store.Store) bool {
	chosen, err := st.Setting(listDensitySetting)
	if err != nil {
		return false
	}
	return chosen == "comfortable"
}

// enterFocuses reports which key opens a session where. Enter focuses the
// preview and A attaches full screen by default; a stored "attach" choice
// swaps the pair. Cached on the model because the footer reads it every
// frame.
func (m *Model) enterFocuses() bool {
	return m.focusOnEnter
}

// storedFocusOnEnter reads the persisted key choice. A read failure yields
// the default pairing.
func storedFocusOnEnter(st *store.Store) bool {
	chosen, err := st.Setting(focusKeySetting)
	if err != nil {
		return true
	}
	return chosen != "attach"
}

func (m *Model) openSettings() {
	if len(m.cfg.Tools) == 0 {
		m.errBar.text = "no tools configured"
		return
	}
	m.errBar.text = ""
	names, index := m.defaultToolSelection()
	m.settings = settingsState{
		toolNames:      names,
		toolIndex:      index,
		themeIndex:     themeIndex(current.Name),
		layoutSplit:    m.defaultSplitLayout(),
		quickCloseSend: m.quickCloseAfterSend(),
		enterFocuses:   m.enterFocuses(),

		comfortableRows: m.comfortableRows,
		worktreeDefault: m.defaultWorktree(),
	}
	m.mode = modeSettings
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.settings.field = (m.settings.field + settingsFieldCount - 1) % settingsFieldCount
	case "down", "j":
		m.settings.field = (m.settings.field + 1) % settingsFieldCount
	case "left", "h":
		m.cycleSetting(-1)
	case "right", "l":
		m.cycleSetting(1)
	case "enter", "esc":
		// Enter on the bug-report row is the action itself, not save-and-close.
		if msg.String() == "enter" && m.settings.field == settingsFieldBugReport {
			if err := openBrowser(bugReportURL(m.update.version)); err != nil {
				m.errBar.text = err.Error()
			}
			return m, nil
		}
		if err := m.store.SetSetting("default_tool", m.settings.toolNames[m.settings.toolIndex]); err != nil {
			m.errBar.text = err.Error()
		}
		if err := m.store.SetSetting(themeSetting, themes[m.settings.themeIndex].Name); err != nil {
			m.errBar.text = err.Error()
		}
		layout := "split"
		if !m.settings.layoutSplit {
			layout = "unified"
		}
		if err := m.store.SetSetting(diffLayoutSetting, layout); err != nil {
			m.errBar.text = err.Error()
		}
		quickClose := "stay"
		if m.settings.quickCloseSend {
			quickClose = "close"
		}
		if err := m.store.SetSetting(quickCloseSetting, quickClose); err != nil {
			m.errBar.text = err.Error()
		}
		focusKey := "focus"
		if !m.settings.enterFocuses {
			focusKey = "attach"
		}
		if err := m.store.SetSetting(focusKeySetting, focusKey); err != nil {
			m.errBar.text = err.Error()
		}
		density := "compact"
		if m.settings.comfortableRows {
			density = "comfortable"
		}
		if err := m.store.SetSetting(listDensitySetting, density); err != nil {
			m.errBar.text = err.Error()
		}
		worktreeChoice := "off"
		if m.settings.worktreeDefault {
			worktreeChoice = "on"
		}
		if err := m.store.SetSetting(worktreeSetting, worktreeChoice); err != nil {
			m.errBar.text = err.Error()
		}
		m.focusOnEnter = m.settings.enterFocuses
		m.comfortableRows = m.settings.comfortableRows
		m.mode = modeList
	}
	return m, nil
}

// cycleSetting steps the focused setting by one. The theme applies as it
// is stepped so the picker doubles as a live preview of the palette.
func (m *Model) cycleSetting(step int) {
	switch m.settings.field {
	case settingsFieldTool:
		count := len(m.settings.toolNames)
		m.settings.toolIndex = (m.settings.toolIndex + step + count) % count
	case settingsFieldTheme:
		m.settings.themeIndex = (m.settings.themeIndex + step + len(themes)) % len(themes)
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalBackground()
	case settingsFieldDensity:
		m.settings.comfortableRows = !m.settings.comfortableRows
	case settingsFieldLayout:
		m.settings.layoutSplit = !m.settings.layoutSplit
	case settingsFieldQuickClose:
		m.settings.quickCloseSend = !m.settings.quickCloseSend
	case settingsFieldFocusKey:
		m.settings.enterFocuses = !m.settings.enterFocuses
	case settingsFieldWorktree:
		m.settings.worktreeDefault = !m.settings.worktreeDefault
	}
}
