package ui

import (
	"net/url"
	"slices"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	tea "github.com/charmbracelet/bubbletea"
)

// defaultTool is the CLI quick spawn launches. A store error still yields
// the fallback but is surfaced, never swallowed.
func (m *Model) defaultTool() string {
	hidden := m.hiddenTools()
	chosen, err := m.store.DefaultTool()
	if err != nil {
		m.errBar.text = "reading default tool setting: " + err.Error()
	}
	return m.cfg.DefaultAgentTool(chosen, hidden)
}

// hiddenTools returns the set of CLI names the user turned off for new sessions.
func (m *Model) hiddenTools() map[string]bool {
	hidden, err := m.store.HiddenTools()
	if err != nil {
		m.errBar.text = "reading hidden tools setting: " + err.Error()
		return nil
	}
	return hidden
}

func (m *Model) defaultWorktree() bool {
	chosen, err := m.store.Setting(worktreeSetting)
	if err != nil {
		m.errBar.text = "reading worktree setting: " + err.Error()
		return false
	}
	return chosen == "on"
}

// proactiveCoordination reads how sessions treat each other for the
// settings row. A store error is surfaced but still yields the default.
func (m *Model) proactiveCoordination() bool {
	proactive, err := m.store.ProactiveCoordination()
	if err != nil {
		m.errBar.text = "reading coordination setting: " + err.Error()
	}
	return proactive
}

func (m *Model) spawnWorktreeDefault(group string) bool {
	for g := group; g != ""; g = parentGroup(g) {
		switch m.groupWorktrees[g] {
		case "on":
			return true
		case "off":
			return false
		}
	}
	for _, name := range m.enabledToolNames() {
		if name == m.lastSpawnTool {
			return m.lastSpawnWorktree
		}
	}
	return m.defaultWorktree()
}

// groupBase is the ref a spawn into group branches from: the nearest
// ancestor group's choice, or "" to detect the repo's default branch.
func (m *Model) groupBase(group string) string {
	for g := group; g != ""; g = parentGroup(g) {
		if base := m.groupBases[g]; base != "" {
			return base
		}
	}
	return ""
}

// stepGroupBase moves a group's base choice through auto and the branches
// of the repo at dir.
func (m *Model) stepGroupBase(dir, current string, delta int) string {
	if m.gitDrv == nil {
		m.errBar.text = "a group base needs git installed"
		return current
	}
	refs, err := m.gitDrv.BranchRefs(dir)
	if err != nil {
		m.errBar.text = "group base: " + err.Error()
		return current
	}
	m.errBar.text = ""
	choices := append([]string{""}, refs...)
	at := max(slices.Index(choices, current), 0)
	return choices[(at+delta+len(choices))%len(choices)]
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
	if last, seen := m.baseFetches[key]; seen && time.Since(last.at) < baseFetchInterval {
		return nil
	}
	if m.baseFetches == nil {
		m.baseFetches = map[baseFetchKey]baseFetch{}
	}
	m.baseFetches[key] = baseFetch{at: time.Now()}
	driver := m.gitDrv
	return func() tea.Msg {
		return baseFetchedMsg{key: key, detected: driver.DefaultBase(dir)}
	}
}

// recordBaseFetch keeps what a step of a base refresh found, and starts the
// fetch once the resolving step is in.
func (m *Model) recordBaseFetch(msg baseFetchedMsg) tea.Cmd {
	fetch, ok := m.baseFetches[msg.key]
	if !ok {
		return nil
	}
	fetch.resolved, fetch.detected = true, msg.detected
	if msg.fetched {
		fetch.fetched, fetch.err = true, msg.err
	}
	m.baseFetches[msg.key] = fetch
	if msg.fetched || m.baseFetchOff {
		return nil
	}
	driver, key := m.gitDrv, msg.key
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
	fetch := m.baseFetches[baseFetchKey{dir: dir, override: override}]
	label := valueStyle.Render(override) + subtleStyle.Render(" (group)")
	if override == "" {
		switch {
		case !fetch.resolved:
			label = subtleStyle.Render("…")
		case fetch.detected == "":
			label = valueStyle.Render("HEAD") + subtleStyle.Render(" (auto)")
		default:
			label = valueStyle.Render(fetch.detected) + subtleStyle.Render(" (auto)")
		}
	}
	switch {
	case m.baseFetchOff:
	case !fetch.fetched:
		label += subtleStyle.Render(" · fetching")
	case fetch.err != nil:
		label += subtleStyle.Render(" · fetch failed")
	}
	return label
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

// storedFullLayout reads the persisted sessions layout. Split is the
// default; a stored "full" choice gives the rail the whole width.
func storedFullLayout(st *store.Store) bool {
	chosen, err := st.Setting(sessionLayoutSetting)
	if err != nil {
		return false
	}
	return chosen == "full"
}

func sessionLayoutValue(full bool) string {
	if full {
		return "full"
	}
	return "split"
}

func storedHideHeader(st *store.Store) bool {
	chosen, err := st.Setting(hideHeaderSetting)
	if err != nil {
		return false
	}
	return chosen == "on"
}

func storedHideStats(st *store.Store) bool {
	chosen, err := st.Setting(hideStatsSetting)
	if err != nil {
		return false
	}
	return chosen == "on"
}

// storedTerminalBackground reads the background row. A store error is
// surfaced but still yields the painted default.
func (m *Model) storedTerminalBackground() bool {
	chosen, err := m.store.Setting(backgroundSetting)
	if err != nil {
		m.errBar.text = "reading background setting: " + err.Error()
	}
	return chosen == "terminal"
}

// storedMouseDisabled reads the persisted mouse-reporting choice. On is the
// default; only an explicit "off" gives the rail back to the terminal.
func storedMouseDisabled(st *store.Store) bool {
	chosen, err := st.Setting(mouseSetting)
	if err != nil {
		return false
	}
	return chosen == "off"
}

// storedBaseFetchOff reads the persisted fetch-on-spawn choice. On is the
// default; only an explicit "off" skips the fetch.
func storedBaseFetchOff(st *store.Store) bool {
	chosen, err := st.Setting(baseFetchSetting)
	if err != nil {
		return false
	}
	return chosen == "off"
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

func (m *Model) openSettings() tea.Cmd {
	if len(m.cfg.Tools) == 0 {
		m.errBar.text = "no tools configured"
		return nil
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
		fullLayout:      m.fullLayout,
		hideHeader:      m.hideHeader,
		hideStats:       m.hideStats,
		mouseDisabled:   m.mouseDisabled,
		worktreeDefault: m.defaultWorktree(),
		baseFetch:       !m.baseFetchOff,
		proactive:       m.proactiveCoordination(),
		notifications:   storedNotifications(m.store),
		notifyFinished:  storedNotifyFinished(m.store),
		notifyCommand:   m.loadNotifyCommandRow(),
		themeAuto:       themeAutoEnabled(m.store),
		manualTheme:     themes[themeIndex(storedTheme(m.store))].Name,
		editor:          newEditorRow(m.editor),

		terminalBackground: m.terminalBackground,
	}
	m.mode = modeSettings
	return probeEditors
}

func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.cliPicker {
		return m.handleCLIPickerKey(msg)
	}
	if m.settings.keyPicker {
		return m.handleKeyPickerKey(msg)
	}
	if m.settings.editor.typing {
		return m.handleEditorTypingKey(msg)
	}
	if m.settings.notifyCommand.typing {
		return m.handleNotifyCommandTypingKey(msg)
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
			return m, openLink(bugReportURL(m.update.version))
		case settingsFieldFeatureRequest:
			return m, openLink(featureRequestURL())
		case settingsFieldCLIs:
			m.openCLIPicker()
			return m, nil
		case settingsFieldKeybindings:
			m.openKeyPicker()
			return m, nil
		case settingsFieldNotifyCommand:
			m.openNotifyCommandTyping()
			return m, nil
		case settingsFieldEditor:
			if m.settings.editor.custom {
				m.openEditorTyping()
				return m, nil
			}
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
	m.rebuildRows()
	m.mode = modeList
	return m, nil
}

func (m *Model) persistSettings() {
	if len(m.settings.toolNames) > 0 {
		if err := m.store.SetDefaultTool(m.settings.toolNames[m.settings.toolIndex]); err != nil {
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
	if err := m.store.SetSetting(sessionLayoutSetting, sessionLayoutValue(m.settings.fullLayout)); err != nil {
		m.errBar.text = err.Error()
	}
	hideHeader := "off"
	if m.settings.hideHeader {
		hideHeader = "on"
	}
	if err := m.store.SetSetting(hideHeaderSetting, hideHeader); err != nil {
		m.errBar.text = err.Error()
	}
	hideStats := "off"
	if m.settings.hideStats {
		hideStats = "on"
	}
	if err := m.store.SetSetting(hideStatsSetting, hideStats); err != nil {
		m.errBar.text = err.Error()
	}
	background := "theme"
	if m.settings.terminalBackground {
		background = "terminal"
	}
	if err := m.store.SetSetting(backgroundSetting, background); err != nil {
		m.errBar.text = err.Error()
	}
	mouseMode := "on"
	if m.settings.mouseDisabled {
		mouseMode = "off"
	}
	if err := m.store.SetSetting(mouseSetting, mouseMode); err != nil {
		m.errBar.text = err.Error()
	}
	worktreeChoice := "off"
	if m.settings.worktreeDefault {
		worktreeChoice = "on"
	}
	if err := m.store.SetSetting(worktreeSetting, worktreeChoice); err != nil {
		m.errBar.text = err.Error()
	}
	baseFetch := "on"
	if !m.settings.baseFetch {
		baseFetch = "off"
	}
	if err := m.store.SetSetting(baseFetchSetting, baseFetch); err != nil {
		m.errBar.text = err.Error()
	}
	if err := m.store.SetProactiveCoordination(m.settings.proactive); err != nil {
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
	if m.settings.notifyCommand.loaded {
		if err := m.store.SetSetting(notifyCommandSetting, m.settings.notifyCommand.value); err != nil {
			m.errBar.text = err.Error()
		}
	}
	if err := m.store.SetEditor(m.settings.editor.line()); err != nil {
		m.errBar.text = err.Error()
	}
	m.editor = m.settings.editor.line()
	m.focusOnEnter = m.settings.enterFocuses
	m.arrowStep = m.settings.arrowStep
	m.comfortableRows = m.settings.comfortableRows
	m.fullLayout = m.settings.fullLayout
	m.hideHeader = m.settings.hideHeader
	m.hideStats = m.settings.hideStats
	m.mouseDisabled = m.settings.mouseDisabled
	m.baseFetchOff = !m.settings.baseFetch
}

func (m *Model) openCLIPicker() {
	names := m.cfg.AgentToolNames()
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
			return m, openLink(requestCLISupportURL())
		}
		m.toggleCLIHidden(m.settings.cliNames[m.settings.cliCursor])
	case "esc":
		if err := m.store.SetHiddenTools(m.settings.cliHidden); err != nil {
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
		SyncTerminalColors()
		return m.syncPaneTheme()
	case settingsFieldThemeAuto:
		m.settings.themeAuto = !m.settings.themeAuto
		name := m.settings.manualTheme
		if m.settings.themeAuto {
			name = autoThemeName(m.settings.manualTheme, systheme.Detect())
		}
		m.settings.themeIndex = themeIndex(name)
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalColors()
		return m.syncPaneTheme()
	case settingsFieldBackground:
		m.settings.terminalBackground = !m.settings.terminalBackground
		m.terminalBackground = m.settings.terminalBackground
	case settingsFieldDensity:
		m.settings.comfortableRows = !m.settings.comfortableRows
	case settingsFieldSessionLayout:
		m.settings.fullLayout = !m.settings.fullLayout
	case settingsFieldHeader:
		m.settings.hideHeader = !m.settings.hideHeader
	case settingsFieldStats:
		m.settings.hideStats = !m.settings.hideStats
	case settingsFieldLayout:
		m.settings.layoutSplit = !m.settings.layoutSplit
	case settingsFieldQuickClose:
		m.settings.quickCloseSend = !m.settings.quickCloseSend
	case settingsFieldFocusKey:
		m.settings.enterFocuses = !m.settings.enterFocuses
	case settingsFieldArrowStep:
		m.settings.arrowStep = !m.settings.arrowStep
	case settingsFieldMouse:
		m.settings.mouseDisabled = !m.settings.mouseDisabled
	case settingsFieldWorktree:
		m.settings.worktreeDefault = !m.settings.worktreeDefault
	case settingsFieldBaseFetch:
		m.settings.baseFetch = !m.settings.baseFetch
	case settingsFieldCoordination:
		m.settings.proactive = !m.settings.proactive
	case settingsFieldNotify:
		m.settings.notifications = !m.settings.notifications
	case settingsFieldNotifyFinish:
		m.settings.notifyFinished = !m.settings.notifyFinished
	case settingsFieldEditor:
		m.settings.editor.cycle(step)
	}
	return nil
}
