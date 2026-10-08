package ui

import (
	"net/url"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	tea "github.com/charmbracelet/bubbletea"
)

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
	return m.settingsCache.value(worktreeSetting) == "on"
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
		m.errBar.text = ""
		return stepBaseChoice(answer.refs, current, delta), nil
	}
	driver := m.services.gitDrv
	if driver == nil {
		m.errBar.text = "a group base needs git installed"
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
		if m.mode != modeGroupForm || uint64(m.groupForm.gen) != msg.gen || m.groupFormDir() != msg.dir {
			return
		}
		base = &m.groupForm.base
	case groupBaseRename:
		if m.mode != modeRename || m.dialogGen != msg.gen || m.renameGroupDir() != msg.dir {
			return
		}
		base = &m.rename.base
	default:
		return
	}
	if msg.err != nil {
		m.errBar.text = "group base: " + msg.err.Error()
		return
	}
	if m.ledger.baseRefs == nil {
		m.ledger.baseRefs = map[string]baseRefsAnswer{}
	}
	m.ledger.baseRefs[msg.dir] = baseRefsAnswer{refs: msg.refs, at: time.Now()}
	if *base != msg.from {
		return
	}
	m.errBar.text = ""
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
	if last, seen := m.baseFetches[key]; seen && time.Since(last.at) < baseFetchInterval {
		return nil
	}
	if m.baseFetches == nil {
		m.baseFetches = map[baseFetchKey]baseFetch{}
	}
	m.baseFetches[key] = baseFetch{at: time.Now()}
	driver := m.services.gitDrv
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
	case m.prefs.baseFetchOff:
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

// forgetWorktreeCapability drops the memo so the next look is a fresh one.
// Opening the form or the quick bar calls it.
func (m *Model) forgetWorktreeCapability() {
	m.ledger.worktreeRepos = nil
}

// defaultSplitLayout reports whether review mode should open in split
// (side-by-side) layout. Split is the default; a stored "unified" choice
// opts out. A store error is surfaced but still yields the split default.
func (m *Model) defaultSplitLayout() bool {
	chosen, err := m.services.store.Setting(diffLayoutSetting)
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

// storedMouseDisabled reads the persisted mouse-reporting choice. On is the
// default; only an explicit "off" gives the rail back to the terminal.
func storedMouseDisabled(st *store.Store) bool {
	chosen, err := st.Setting(mouseSetting)
	if err != nil {
		return false
	}
	return chosen == "off"
}

// storedTerminalBackground reads the background row. A store error is
// surfaced but still yields the painted default.
func (m *Model) storedTerminalBackground() bool {
	chosen, err := m.services.store.Setting(backgroundSetting)
	if err != nil {
		m.errBar.text = "reading background setting: " + err.Error()
	}
	return chosen == "terminal"
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
	return m.prefs.focusOnEnter
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

func (m *Model) cachedHiddenTools() map[string]bool {
	hidden := make(map[string]bool)
	for name, on := range m.settingsCache.hidden {
		if on {
			if _, ok := m.services.cfg.Tools[name]; ok {
				hidden[name] = true
			}
		}
	}
	return hidden
}

func (m *Model) cachedToolSelection(hidden map[string]bool, preferred string) ([]string, int) {
	names := m.services.cfg.EnabledAgentTools(hidden)
	if preferred == "" {
		preferred = m.settingsCache.value("default_tool")
	}
	for index, name := range names {
		if name == preferred {
			return names, index
		}
	}
	return names, 0
}

func (m *Model) settingsStateFromCache() settingsState {
	hidden := m.cachedHiddenTools()
	names, index := m.cachedToolSelection(hidden, "")
	manualTheme := themes[themeIndex(m.settingsCache.value(themeSetting))].Name
	return settingsState{
		toolNames:       names,
		toolIndex:       index,
		themeIndex:      themeIndex(current.Name),
		layoutSplit:     m.settingsCache.value(diffLayoutSetting) != "unified",
		quickCloseSend:  m.settingsCache.value(quickCloseSetting) == "close",
		enterFocuses:    m.settingsCache.value(focusKeySetting) != "attach",
		arrowStep:       m.settingsCache.value(arrowStepSetting) != "off",
		comfortableRows: m.settingsCache.value(listDensitySetting) == "comfortable",
		fullLayout:      m.settingsCache.value(sessionLayoutSetting) == "full",
		hideHeader:      m.settingsCache.value(hideHeaderSetting) == "on",
		hideStats:       m.settingsCache.value(hideStatsSetting) == "on",
		mouseDisabled:   m.settingsCache.value(mouseSetting) == "off",
		worktreeDefault: m.settingsCache.value(worktreeSetting) == "on",
		baseFetch:       m.settingsCache.value(baseFetchSetting) != "off",
		proactive:       m.settingsCache.value("coordination") == "on",
		notifications:   m.settingsCache.value(notificationsSetting) != "off",
		notifyFinished:  m.settingsCache.value(notifyFinishedSetting) == "on",
		themeAuto:       m.settingsCache.value(themeAutoSetting) == "on",
		manualTheme:     manualTheme,
		cliHidden:       hidden,
		editor:          m.cachedEditorRow(),

		terminalBackground: m.settingsCache.value(backgroundSetting) == "terminal",
	}
}

func (m *Model) applyCachedSettingsPrefs() {
	m.prefs.focusOnEnter = m.settingsCache.value(focusKeySetting) != "attach"
	m.prefs.arrowStep = m.settingsCache.value(arrowStepSetting) != "off"
	m.prefs.comfortableRows = m.settingsCache.value(listDensitySetting) == "comfortable"
	m.prefs.fullLayout = m.settingsCache.value(sessionLayoutSetting) == "full"
	m.prefs.hideHeader = m.settingsCache.value(hideHeaderSetting) == "on"
	m.prefs.hideStats = m.settingsCache.value(hideStatsSetting) == "on"
	m.prefs.mouseDisabled = m.settingsCache.value(mouseSetting) == "off"
	m.prefs.terminalBackground = m.settingsCache.value(backgroundSetting) == "terminal"
	m.prefs.baseFetchOff = m.settingsCache.value(baseFetchSetting) == "off"
	m.services.editor = m.settingsCache.value(editorSetting)
}

func (m *Model) openSettings() tea.Cmd {
	return m.openSettingsWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openSettingsWithReader(reader settingsValueReader) tea.Cmd {
	m.settingsGen++
	if len(m.services.cfg.Tools) == 0 {
		m.errBar.text = "no tools configured"
		return nil
	}
	m.errBar.text = ""
	m.settings = m.settingsStateFromCache()
	m.mode = modeSettings
	probe := m.probeEditorsCmd()
	if m.settingsPending > 0 {
		return probe
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadDialog, generation: m.settingsGen}, reader), probe)
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
				// everything staged this visit must land first; the
				// update command follows the save's completion.
				m.update.applying = true
				m.applySettingsPrefs()
				m.errBar.text = ""
				return m, m.captureSettingsSave(false, true)
			}
		}
		return m.saveAndCloseSettings()
	case "esc":
		return m.saveAndCloseSettings()
	}
	return m, nil
}

func (m *Model) saveAndCloseSettings() (tea.Model, tea.Cmd) {
	m.applySettingsPrefs()
	m.rebuildRows()
	m.mode = modeList
	return m, m.captureSettingsSave(true, false)
}

// captureSettingsSave enqueues the dialog's chosen preferences on the
// effect lane; the store writes run outside Update. The captured
// generation fences a completion against a newer dialog.
func (m *Model) captureSettingsSave(includeHidden, followUpdate bool) tea.Cmd {
	m.settingsGen++
	request := settingsRequest{
		values:       m.captureSettingValues(),
		followUpdate: followUpdate,
		generation:   m.settingsGen,
	}
	if includeHidden {
		request.hidden = m.hiddenToolList()
	}
	m.settingsCache.applyValues(request.values)
	if request.hidden != nil {
		m.settingsCache.applyHidden(request.hidden)
	}
	m.settingsPending++
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) captureHiddenSave() tea.Cmd {
	m.settingsGen++
	request := settingsRequest{
		hidden:     m.hiddenToolList(),
		generation: m.settingsGen,
	}
	m.settingsCache.applyHidden(request.hidden)
	m.settingsPending++
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// hiddenToolList is the picker's hidden set in persist order (sorted
// names, comma-joined by the worker).
func (m *Model) hiddenToolList() []string {
	names := make([]string, 0, len(m.settings.cliHidden))
	for name, on := range m.settings.cliHidden {
		if on {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}

// captureSettingValues copies the dialog's chosen preferences into the
// ordered write list the effect worker persists, in persistSettings'
// former order.
func (m *Model) captureSettingValues() []settingValue {
	values := make([]settingValue, 0, 16)
	if len(m.settings.toolNames) > 0 {
		values = append(values, settingValue{key: "default_tool", value: m.settings.toolNames[m.settings.toolIndex]})
	}
	// With auto-detect on, the picker shows the detected theme; the theme
	// key keeps the manual choice so turning auto off returns to it.
	manualTheme := themes[m.settings.themeIndex].Name
	if m.settings.themeAuto {
		manualTheme = m.settings.manualTheme
	}
	values = append(values, settingValue{key: themeSetting, value: manualTheme})
	themeAuto := "off"
	if m.settings.themeAuto {
		themeAuto = "on"
	}
	values = append(values, settingValue{key: themeAutoSetting, value: themeAuto})
	layout := "split"
	if !m.settings.layoutSplit {
		layout = "unified"
	}
	values = append(values, settingValue{key: diffLayoutSetting, value: layout})
	quickClose := "stay"
	if m.settings.quickCloseSend {
		quickClose = "close"
	}
	values = append(values, settingValue{key: quickCloseSetting, value: quickClose})
	focusKey := "focus"
	if !m.settings.enterFocuses {
		focusKey = "attach"
	}
	values = append(values, settingValue{key: focusKeySetting, value: focusKey})
	arrowStep := "on"
	if !m.settings.arrowStep {
		arrowStep = "off"
	}
	values = append(values, settingValue{key: arrowStepSetting, value: arrowStep})
	density := "compact"
	if m.settings.comfortableRows {
		density = "comfortable"
	}
	values = append(values, settingValue{key: listDensitySetting, value: density})
	values = append(values, settingValue{key: sessionLayoutSetting, value: sessionLayoutValue(m.settings.fullLayout)})
	hideHeader := "off"
	if m.settings.hideHeader {
		hideHeader = "on"
	}
	values = append(values, settingValue{key: hideHeaderSetting, value: hideHeader})
	hideStats := "off"
	if m.settings.hideStats {
		hideStats = "on"
	}
	values = append(values, settingValue{key: hideStatsSetting, value: hideStats})
	background := "theme"
	if m.settings.terminalBackground {
		background = "terminal"
	}
	values = append(values, settingValue{key: backgroundSetting, value: background})
	mouseMode := "on"
	if m.settings.mouseDisabled {
		mouseMode = "off"
	}
	values = append(values, settingValue{key: mouseSetting, value: mouseMode})
	worktreeChoice := "off"
	if m.settings.worktreeDefault {
		worktreeChoice = "on"
	}
	values = append(values, settingValue{key: worktreeSetting, value: worktreeChoice})
	baseFetch := "on"
	if !m.settings.baseFetch {
		baseFetch = "off"
	}
	values = append(values, settingValue{key: baseFetchSetting, value: baseFetch})
	proactive := "off"
	if m.settings.proactive {
		proactive = "on"
	}
	values = append(values, settingValue{key: "coordination", value: proactive, proactive: true})
	notifications := "off"
	if m.settings.notifications {
		notifications = "on"
	}
	values = append(values, settingValue{key: notificationsSetting, value: notifications})
	notifyFinished := "off"
	if m.settings.notifyFinished {
		notifyFinished = "on"
	}
	values = append(values, settingValue{key: notifyFinishedSetting, value: notifyFinished})
	values = append(values, settingValue{key: editorSetting, value: m.settings.editor.line()})
	return values
}

// applySettingsPrefs mirrors the just chosen preferences into the live
// session: a deliberate live preview of the staged choices, like the
// theme's. It is not a persistence receipt — a save that later fails
// reconciles these prefs back to the committed values on completion.
func (m *Model) applySettingsPrefs() {
	m.prefs.focusOnEnter = m.settings.enterFocuses
	m.prefs.arrowStep = m.settings.arrowStep
	m.prefs.comfortableRows = m.settings.comfortableRows
	m.prefs.fullLayout = m.settings.fullLayout
	m.prefs.hideHeader = m.settings.hideHeader
	m.prefs.hideStats = m.settings.hideStats
	m.prefs.mouseDisabled = m.settings.mouseDisabled
	m.prefs.terminalBackground = m.settings.terminalBackground
	m.prefs.baseFetchOff = !m.settings.baseFetch
	m.services.editor = m.settings.editor.line()
}

func (m *Model) openCLIPicker() {
	names := m.services.cfg.AgentToolNames()
	hidden := m.settings.cliHidden
	if hidden == nil {
		hidden = m.cachedHiddenTools()
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
		m.settings.cliPicker = false
		// Refresh the quick-spawn tool list so it matches the new filter.
		preferred := ""
		if len(m.settings.toolNames) > 0 && m.settings.toolIndex < len(m.settings.toolNames) {
			preferred = m.settings.toolNames[m.settings.toolIndex]
		}
		names, index := m.cachedToolSelection(m.settings.cliHidden, preferred)
		m.settings.toolNames = names
		m.settings.toolIndex = index
		return m, m.captureHiddenSave()
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
		m.settings.dirty = true
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
	m.settings.dirty = true
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
	changed := true
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
		m.settings.dirty = true
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
		m.settings.dirty = true
		applyTheme(themes[m.settings.themeIndex])
		SyncTerminalColors()
		return m.syncPaneTheme()
	case settingsFieldBackground:
		m.settings.terminalBackground = !m.settings.terminalBackground
		m.prefs.terminalBackground = m.settings.terminalBackground
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
	default:
		changed = false
	}
	if changed {
		m.settings.dirty = true
	}
	return nil
}
