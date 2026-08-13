package ui

import (
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultTool is the CLI quick spawn launches: the settings choice when it
// is still enabled, else the first enabled tool. A store error still yields
// the fallback but is surfaced, never swallowed.
func (m *Model) defaultTool() string {
	names := m.enabledToolNames()
	if len(names) == 0 {
		return ""
	}
	chosen, err := m.store.Setting("default_tool")
	if err != nil {
		m.errBar.text = "reading default tool setting: " + err.Error()
		return names[0]
	}
	if chosen != "" {
		for _, name := range names {
			if name == chosen {
				return chosen
			}
		}
	}
	return names[0]
}

// hiddenTools returns the set of CLI names the user turned off for new sessions.
func (m *Model) hiddenTools() map[string]bool {
	raw, err := m.store.Setting(hiddenToolsSetting)
	if err != nil {
		m.errBar.text = "reading hidden tools setting: " + err.Error()
		return nil
	}
	return parseHiddenTools(raw)
}

func parseHiddenTools(raw string) map[string]bool {
	if raw == "" {
		return nil
	}
	hidden := make(map[string]bool)
	for _, part := range strings.Split(raw, ",") {
		name := strings.TrimSpace(part)
		if name != "" {
			hidden[name] = true
		}
	}
	if len(hidden) == 0 {
		return nil
	}
	return hidden
}

func formatHiddenTools(hidden map[string]bool) string {
	if len(hidden) == 0 {
		return ""
	}
	names := make([]string, 0, len(hidden))
	for name, on := range hidden {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ",")
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

// storedShellsPinned reads the persisted terminal placement. Pinned is the
// default; only an explicit "inline" leaves shells among the agents.
func storedShellsPinned(st *store.Store) bool {
	chosen, err := st.Setting(terminalPlacementSetting)
	if err != nil {
		return true
	}
	return chosen != "inline"
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

// storedArrowStep reads the persisted ←→ step choice. On is the default;
// only an explicit "off" turns the pair off.
func storedArrowStep(st *store.Store) bool {
	chosen, err := st.Setting(arrowStepSetting)
	if err != nil {
		return true
	}
	return chosen != "off"
}

func storedNotifications(st *store.Store) bool {
	chosen, err := st.Setting(notificationsSetting)
	if err != nil {
		return true
	}
	return chosen != "off"
}

func storedNotifyFinished(st *store.Store) bool {
	chosen, err := st.Setting(notifyFinishedSetting)
	if err != nil {
		return false
	}
	return chosen == "on"
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
		arrowStep:      m.arrowStep,

		comfortableRows: m.comfortableRows,
		worktreeDefault: m.defaultWorktree(),
		shellsPinned:    m.shellsPinned,
		notifications:   storedNotifications(m.store),
		notifyFinished:  storedNotifyFinished(m.store),
		themeAuto:       themeAutoEnabled(m.store),
		manualTheme:     themes[themeIndex(storedTheme(m.store))].Name,
	}
	m.mode = modeSettings
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.cliPicker {
		return m.handleCLIPickerKey(msg)
	}
	switch msg.String() {
	case "up", "k":
		m.settings.field = (m.settings.field + settingsFieldCount - 1) % settingsFieldCount
	case "down", "j":
		m.settings.field = (m.settings.field + 1) % settingsFieldCount
	case "left", "h":
		return m, m.cycleSetting(-1)
	case "right", "l":
		return m, m.cycleSetting(1)
	case "enter":
		switch m.settings.field {
		case settingsFieldBugReport:
			if err := openBrowser(bugReportURL(m.update.version)); err != nil {
				m.errBar.text = err.Error()
			}
			return m, nil
		case settingsFieldCLIs:
			m.openCLIPicker()
			return m, nil
		case settingsFieldUpdate:
			if m.update.applying {
				return m, nil
			}
			if m.update.latest != "" {
				// A successful swap quits to exec the new build, so
				// everything staged this visit must land first.
				m.persistSettings()
				m.update.applying = true
				m.errBar.text = ""
				return m, m.applyUpdateCmd()
			}
		}
		return m.saveAndCloseSettings()
	case "esc":
		return m.saveAndCloseSettings()
	}
	return m, nil
}

func (m *Model) saveAndCloseSettings() (tea.Model, tea.Cmd) {
	m.persistSettings()
	// Placement changes the tree's shape, so the rail has to be rebuilt
	// here rather than waiting for the next poll.
	m.rebuildRows()
	m.mode = modeList
	return m, nil
}

func (m *Model) persistSettings() {
	if len(m.settings.toolNames) > 0 {
		if err := m.store.SetSetting("default_tool", m.settings.toolNames[m.settings.toolIndex]); err != nil {
			m.errBar.text = err.Error()
		}
	}
	// With auto-detect on, the picker shows the detected theme; the theme
	// key keeps the manual choice so turning auto off returns to it.
	manualTheme := themes[m.settings.themeIndex].Name
	if m.settings.themeAuto {
		manualTheme = m.settings.manualTheme
	}
	if err := m.store.SetSetting(themeSetting, manualTheme); err != nil {
		m.errBar.text = err.Error()
	}
	themeAuto := "off"
	if m.settings.themeAuto {
		themeAuto = "on"
	}
	if err := m.store.SetSetting(themeAutoSetting, themeAuto); err != nil {
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
	arrowStep := "on"
	if !m.settings.arrowStep {
		arrowStep = "off"
	}
	if err := m.store.SetSetting(arrowStepSetting, arrowStep); err != nil {
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
	placement := "pinned"
	if !m.settings.shellsPinned {
		placement = "inline"
	}
	if err := m.store.SetSetting(terminalPlacementSetting, placement); err != nil {
		m.errBar.text = err.Error()
	}
	notifications := "off"
	if m.settings.notifications {
		notifications = "on"
	}
	if err := m.store.SetSetting(notificationsSetting, notifications); err != nil {
		m.errBar.text = err.Error()
	}
	notifyFinished := "off"
	if m.settings.notifyFinished {
		notifyFinished = "on"
	}
	if err := m.store.SetSetting(notifyFinishedSetting, notifyFinished); err != nil {
		m.errBar.text = err.Error()
	}
	m.focusOnEnter = m.settings.enterFocuses
	m.arrowStep = m.settings.arrowStep
	m.comfortableRows = m.settings.comfortableRows
	m.shellsPinned = m.settings.shellsPinned
}

func (m *Model) openCLIPicker() {
	names := sortedToolNames(m.cfg)
	hidden := make(map[string]bool)
	for name, on := range m.hiddenTools() {
		if on {
			if _, ok := m.cfg.Tools[name]; ok {
				hidden[name] = true
			}
		}
	}
	m.settings.cliPicker = true
	m.settings.cliNames = names
	m.settings.cliHidden = hidden
	m.settings.cliCursor = 0
}

func (m *Model) handleCLIPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// cursor 0..len(names)-1 = tools; len(names) = request-support action.
	count := len(m.settings.cliNames) + 1
	if count < 1 {
		count = 1
	}
	switch msg.String() {
	case "up", "k":
		m.settings.cliCursor = (m.settings.cliCursor + count - 1) % count
	case "down", "j":
		m.settings.cliCursor = (m.settings.cliCursor + 1) % count
	case " ", "space":
		if m.settings.cliCursor < len(m.settings.cliNames) {
			m.toggleCLIHidden(m.settings.cliNames[m.settings.cliCursor])
		}
	case "enter":
		if m.settings.cliCursor >= len(m.settings.cliNames) {
			if err := openBrowser(requestCLISupportURL()); err != nil {
				m.errBar.text = err.Error()
			}
			return m, nil
		}
		m.toggleCLIHidden(m.settings.cliNames[m.settings.cliCursor])
	case "esc":
		if err := m.store.SetSetting(hiddenToolsSetting, formatHiddenTools(m.settings.cliHidden)); err != nil {
			m.errBar.text = err.Error()
		}
		m.settings.cliPicker = false
		// Refresh the quick-spawn tool list so it matches the new filter.
		names, index := m.defaultToolSelection()
		m.settings.toolNames = names
		m.settings.toolIndex = index
	}
	return m, nil
}

// toggleCLIHidden flips visibility for one tool. At least one CLI must stay
// enabled so new sessions still have something to launch.
func (m *Model) toggleCLIHidden(name string) {
	if m.settings.cliHidden == nil {
		m.settings.cliHidden = map[string]bool{}
	}
	if m.settings.cliHidden[name] {
		delete(m.settings.cliHidden, name)
		m.errBar.text = ""
		return
	}
	enabled := 0
	for _, toolName := range m.settings.cliNames {
		if !m.settings.cliHidden[toolName] {
			enabled++
		}
	}
	if enabled <= 1 {
		m.errBar.text = "keep at least one CLI enabled"
		return
	}
	m.settings.cliHidden[name] = true
	m.errBar.text = ""
}

// requestCLISupportURL opens a prefilled feature request for another CLI.
func requestCLISupportURL() string {
	body := "**What are you trying to do**\n\n" +
		"I want agent-manager to support another coding CLI.\n\n" +
		"**What you have in mind**\n\n" +
		"CLI name:\nHow to launch it:\nResume / session flags (if any):\n\n" +
		"**Area**\nConfig and tool support\n"
	return repoURL + "/issues/new?labels=enhancement&body=" + url.QueryEscape(body)
}

// cycleSetting steps the focused setting by one. The theme applies as it
// is stepped so the picker doubles as a live preview of the palette. A theme
// step pushes the pane background to tmux, which shells out, so it returns a
// command rather than blocking the update path.
func (m *Model) cycleSetting(step int) tea.Cmd {
	switch m.settings.field {
	case settingsFieldTool:
		count := len(m.settings.toolNames)
		if count == 0 {
			return nil
		}
		m.settings.toolIndex = (m.settings.toolIndex + step + count) % count
	case settingsFieldTheme:
		// Stepping the theme is a manual choice; it wins over auto-detect
		// rather than being silently overridden on the next start.
		m.settings.themeAuto = false
		m.settings.themeIndex = (m.settings.themeIndex + step + len(themes)) % len(themes)
		m.settings.manualTheme = themes[m.settings.themeIndex].Name
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalBackground()
		return m.syncPaneTheme()
	case settingsFieldThemeAuto:
		m.settings.themeAuto = !m.settings.themeAuto
		name := m.settings.manualTheme
		if m.settings.themeAuto {
			name = autoThemeName(m.settings.manualTheme, systheme.Detect())
		}
		m.settings.themeIndex = themeIndex(name)
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalBackground()
		return m.syncPaneTheme()
	case settingsFieldDensity:
		m.settings.comfortableRows = !m.settings.comfortableRows
	case settingsFieldLayout:
		m.settings.layoutSplit = !m.settings.layoutSplit
	case settingsFieldQuickClose:
		m.settings.quickCloseSend = !m.settings.quickCloseSend
	case settingsFieldFocusKey:
		m.settings.enterFocuses = !m.settings.enterFocuses
	case settingsFieldArrowStep:
		m.settings.arrowStep = !m.settings.arrowStep
	case settingsFieldWorktree:
		m.settings.worktreeDefault = !m.settings.worktreeDefault
	case settingsFieldTerminals:
		m.settings.shellsPinned = !m.settings.shellsPinned
	case settingsFieldNotify:
		m.settings.notifications = !m.settings.notifications
	case settingsFieldNotifyFinish:
		m.settings.notifyFinished = !m.settings.notifyFinished
	}
	return nil
}
