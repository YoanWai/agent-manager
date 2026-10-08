package ui

import (
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
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
	return m.settings.cache.value(worktreeSetting) == "on"
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
		if m.mode != modeGroupForm || uint64(m.groupForm.gen) != msg.gen || m.groupFormDir() != msg.dir {
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
		m.reportErr("reading diff layout setting: " + err.Error())
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
		m.reportErr("reading background setting: " + err.Error())
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

func (m *Model) toolConfig() config.Config { return m.services.cfg }

func (m *Model) keyTables() (session, list keybind.Table) {
	return m.services.keys, m.services.listKeys
}

// queuedKeys lists the key saves on the lane, running first, in the order
// they commit.
func (m *Model) queuedKeys() []keysRequest {
	var requests []keysRequest
	for _, job := range append([]*effectJob{m.effects.main.active}, m.effects.main.pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.request.(keysRequest); ok {
			requests = append(requests, request)
		}
	}
	return requests
}

func (m *Model) submitSettings(request settingsRequest) tea.Cmd {
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) submitKeys(request keysRequest) tea.Cmd {
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) previewBackground(terminal bool) {
	m.prefs.terminalBackground = terminal
}

func (m *Model) release() (version, latest string, applying bool) {
	return m.update.version, m.update.latest, m.update.applying
}

func (m *Model) dialogHeight() int { return m.layout.height }

func (m *Model) cachedHiddenTools() map[string]bool {
	return m.settings.cachedHiddenTools(m.services.cfg)
}

func (s *settingsFeature) cachedHiddenTools(cfg config.Config) map[string]bool {
	hidden := make(map[string]bool)
	for name, on := range s.cache.hidden {
		if on {
			if _, ok := cfg.Tools[name]; ok {
				hidden[name] = true
			}
		}
	}
	return hidden
}

func (m *Model) cachedToolSelection(hidden map[string]bool, preferred string) ([]string, int) {
	return m.settings.cachedToolSelection(m.services.cfg, hidden, preferred)
}

func (s *settingsFeature) cachedToolSelection(cfg config.Config, hidden map[string]bool, preferred string) ([]string, int) {
	names := cfg.EnabledAgentTools(hidden)
	if preferred == "" {
		preferred = s.cache.value("default_tool")
	}
	for index, name := range names {
		if name == preferred {
			return names, index
		}
	}
	return names, 0
}

func (s *settingsFeature) settingsStateFromCache(cfg config.Config) settingsState {
	hidden := s.cachedHiddenTools(cfg)
	names, index := s.cachedToolSelection(cfg, hidden, "")
	manualTheme := themes[themeIndex(s.cache.value(themeSetting))].Name
	return settingsState{
		toolNames:       names,
		toolIndex:       index,
		themeIndex:      themeIndex(current.Name),
		layoutSplit:     s.cache.value(diffLayoutSetting) != "unified",
		quickCloseSend:  s.cache.value(quickCloseSetting) == "close",
		enterFocuses:    s.cache.value(focusKeySetting) != "attach",
		arrowStep:       s.cache.value(arrowStepSetting) != "off",
		comfortableRows: s.cache.value(listDensitySetting) == "comfortable",
		fullLayout:      s.cache.value(sessionLayoutSetting) == "full",
		hideHeader:      s.cache.value(hideHeaderSetting) == "on",
		hideStats:       s.cache.value(hideStatsSetting) == "on",
		mouseDisabled:   s.cache.value(mouseSetting) == "off",
		worktreeDefault: s.cache.value(worktreeSetting) == "on",
		baseFetch:       s.cache.value(baseFetchSetting) != "off",
		proactive:       s.cache.value("coordination") == "on",
		notifications:   s.cache.value(notificationsSetting) != "off",
		notifyFinished:  s.cache.value(notifyFinishedSetting) == "on",
		themeAuto:       s.cache.value(themeAutoSetting) == "on",
		manualTheme:     manualTheme,
		cliHidden:       hidden,
		editor:          s.cachedEditorRow(),

		terminalBackground: s.cache.value(backgroundSetting) == "terminal",
	}
}

func (m *Model) applyCachedSettingsPrefs() {
	m.prefs.focusOnEnter = m.settings.cache.value(focusKeySetting) != "attach"
	m.prefs.arrowStep = m.settings.cache.value(arrowStepSetting) != "off"
	m.prefs.comfortableRows = m.settings.cache.value(listDensitySetting) == "comfortable"
	m.prefs.fullLayout = m.settings.cache.value(sessionLayoutSetting) == "full"
	m.prefs.hideHeader = m.settings.cache.value(hideHeaderSetting) == "on"
	m.prefs.hideStats = m.settings.cache.value(hideStatsSetting) == "on"
	m.prefs.mouseDisabled = m.settings.cache.value(mouseSetting) == "off"
	m.prefs.terminalBackground = m.settings.cache.value(backgroundSetting) == "terminal"
	m.prefs.baseFetchOff = m.settings.cache.value(baseFetchSetting) == "off"
	m.services.editor = m.settings.cache.value(editorSetting)
}

func (m *Model) openSettings() tea.Cmd {
	return m.openSettingsWithReader(storeSettingWriter{st: m.services.store})
}

// openSettingsWithReader is the root's entry to the dialog: the feature
// builds its state and requests, the root switches the mode.
func (m *Model) openSettingsWithReader(reader settingsValueReader) tea.Cmd {
	cmd, opened := m.settings.open(m, reader)
	if opened {
		m.mode = modeSettings
	}
	return cmd
}

func (s *settingsFeature) open(h settingsHost, reader settingsValueReader) (tea.Cmd, bool) {
	s.gen++
	cfg := h.toolConfig()
	if len(cfg.Tools) == 0 {
		h.reportErr("no tools configured")
		return nil, false
	}
	h.clearErr()
	s.dialog = s.settingsStateFromCache(cfg)
	s.markSettingsBaseline()
	probe := s.probeEditorsCmd()
	if s.pending > 0 {
		return probe, true
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadDialog, generation: s.gen}, reader), probe), true
}

// handleSettingsKey is the root adapter for the dialog's keys: it runs
// the exits that close the dialog or start the in-place update.
func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.settings.dialog.keyPicker && !m.settings.dialog.cliPicker {
		return m, m.settings.handleKeyPickerKey(m, msg)
	}
	cmd, exit := m.settings.handleKey(m, msg)
	switch exit {
	case settingsReportBug:
		return m, openLink(bugReportURL(m.update.version))
	case settingsUpdate:
		if m.update.applying {
			return m, nil
		}
		if m.update.latest != "" {
			// A successful swap quits to exec the new build, so
			// everything staged this visit must land first; the
			// update command follows the save's completion.
			m.update.applying = true
			m.applySettingsPrefs()
			m.clearErr()
			return m, m.settings.captureSettingsSave(m, false, true)
		}
		return m.saveAndCloseSettings()
	case settingsSave:
		return m.saveAndCloseSettings()
	}
	return m, cmd
}

func (s *settingsFeature) handleKey(h settingsHost, msg tea.KeyMsg) (tea.Cmd, settingsExit) {
	if s.dialog.cliPicker {
		return s.handleCLIPickerKey(h, msg), settingsStay
	}
	if s.dialog.editor.typing {
		return s.handleEditorTypingKey(msg), settingsStay
	}
	switch msg.String() {
	case "up", "k":
		s.dialog.field = (s.dialog.field + settingsFieldCount - 1) % settingsFieldCount
	case "down", "j":
		s.dialog.field = (s.dialog.field + 1) % settingsFieldCount
	case "left", "h":
		return s.cycleSetting(h, -1), settingsStay
	case "right", "l":
		return s.cycleSetting(h, 1), settingsStay
	case "enter":
		switch s.dialog.field {
		case settingsFieldBugReport:
			return nil, settingsReportBug
		case settingsFieldFeatureRequest:
			return openLink(featureRequestURL()), settingsStay
		case settingsFieldCLIs:
			s.openCLIPicker(h)
			return nil, settingsStay
		case settingsFieldKeybindings:
			s.openKeyPicker(h)
			return nil, settingsStay
		case settingsFieldEditor:
			if s.dialog.editor.custom {
				s.openEditorTyping()
				return nil, settingsStay
			}
		case settingsFieldUpdate:
			return nil, settingsUpdate
		}
		return nil, settingsSave
	case "esc":
		return nil, settingsSave
	}
	return nil, settingsStay
}

func (m *Model) saveAndCloseSettings() (tea.Model, tea.Cmd) {
	m.applySettingsPrefs()
	m.rebuildRows()
	m.mode = modeList
	return m, m.settings.captureSettingsSave(m, true, false)
}

// captureSettingsSave enqueues the dialog's chosen preferences on the
// effect lane; the store writes run outside Update. The captured
// generation fences a completion against a newer dialog.
func (s *settingsFeature) captureSettingsSave(h settingsHost, includeHidden, followUpdate bool) tea.Cmd {
	s.gen++
	request := settingsRequest{
		values:       s.changedSettingValues(),
		followUpdate: followUpdate,
		generation:   s.gen,
	}
	if hidden := s.hiddenToolList(); includeHidden && !s.hiddenAtBaseline(hidden) {
		request.hidden = hidden
	}
	s.advanceSettingsBaseline(request.values, request.hidden)
	s.cache.applyValues(request.values)
	if request.hidden != nil {
		s.cache.applyHidden(request.hidden)
	}
	s.pending++
	return h.submitSettings(request)
}

func (s *settingsFeature) captureHiddenSave(h settingsHost) tea.Cmd {
	s.gen++
	request := settingsRequest{
		hidden:     s.hiddenToolList(),
		generation: s.gen,
	}
	s.advanceSettingsBaseline(nil, request.hidden)
	s.cache.applyHidden(request.hidden)
	s.pending++
	return h.submitSettings(request)
}

// markSettingsBaseline records the freshly built dialog as the state a
// save compares against.
func (s *settingsFeature) markSettingsBaseline() {
	s.dialog.baseline = make(map[string]string)
	for _, value := range s.captureSettingValues() {
		s.dialog.baseline[value.key] = value.value
	}
	hidden := strings.Join(s.hiddenToolList(), ",")
	s.dialog.baselineHidden = &hidden
}

// changedSettingValues is the write list narrowed to the keys the user
// changed in this dialog.
func (s *settingsFeature) changedSettingValues() []settingValue {
	all := s.captureSettingValues()
	changed := all[:0:0]
	for _, value := range all {
		if before, ok := s.dialog.baseline[value.key]; !ok || before != value.value {
			changed = append(changed, value)
		}
	}
	return changed
}

func (s *settingsFeature) hiddenAtBaseline(hidden []string) bool {
	return s.dialog.baselineHidden != nil && *s.dialog.baselineHidden == strings.Join(hidden, ",")
}

// advanceSettingsBaseline moves the baseline to a save's captured
// values, so a dialog left open after it (the update row) writes only
// later changes.
func (s *settingsFeature) advanceSettingsBaseline(values []settingValue, hidden []string) {
	if s.dialog.baseline == nil {
		s.dialog.baseline = make(map[string]string)
	}
	for _, value := range values {
		s.dialog.baseline[value.key] = value.value
	}
	if hidden != nil {
		raw := strings.Join(hidden, ",")
		s.dialog.baselineHidden = &raw
	}
}

// hiddenToolList is the picker's hidden set in persist order (sorted
// names, comma-joined by the worker).
func (s *settingsFeature) hiddenToolList() []string {
	names := make([]string, 0, len(s.dialog.cliHidden))
	for name, on := range s.dialog.cliHidden {
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
func (s *settingsFeature) captureSettingValues() []settingValue {
	values := make([]settingValue, 0, 16)
	if len(s.dialog.toolNames) > 0 {
		values = append(values, settingValue{key: "default_tool", value: s.dialog.toolNames[s.dialog.toolIndex]})
	}
	// With auto-detect on, the picker shows the detected theme; the theme
	// key keeps the manual choice so turning auto off returns to it.
	manualTheme := themes[s.dialog.themeIndex].Name
	if s.dialog.themeAuto {
		manualTheme = s.dialog.manualTheme
	}
	values = append(values, settingValue{key: themeSetting, value: manualTheme})
	themeAuto := "off"
	if s.dialog.themeAuto {
		themeAuto = "on"
	}
	values = append(values, settingValue{key: themeAutoSetting, value: themeAuto})
	layout := "split"
	if !s.dialog.layoutSplit {
		layout = "unified"
	}
	values = append(values, settingValue{key: diffLayoutSetting, value: layout})
	quickClose := "stay"
	if s.dialog.quickCloseSend {
		quickClose = "close"
	}
	values = append(values, settingValue{key: quickCloseSetting, value: quickClose})
	focusKey := "focus"
	if !s.dialog.enterFocuses {
		focusKey = "attach"
	}
	values = append(values, settingValue{key: focusKeySetting, value: focusKey})
	arrowStep := "on"
	if !s.dialog.arrowStep {
		arrowStep = "off"
	}
	values = append(values, settingValue{key: arrowStepSetting, value: arrowStep})
	density := "compact"
	if s.dialog.comfortableRows {
		density = "comfortable"
	}
	values = append(values, settingValue{key: listDensitySetting, value: density})
	values = append(values, settingValue{key: sessionLayoutSetting, value: sessionLayoutValue(s.dialog.fullLayout)})
	hideHeader := "off"
	if s.dialog.hideHeader {
		hideHeader = "on"
	}
	values = append(values, settingValue{key: hideHeaderSetting, value: hideHeader})
	hideStats := "off"
	if s.dialog.hideStats {
		hideStats = "on"
	}
	values = append(values, settingValue{key: hideStatsSetting, value: hideStats})
	background := "theme"
	if s.dialog.terminalBackground {
		background = "terminal"
	}
	values = append(values, settingValue{key: backgroundSetting, value: background})
	mouseMode := "on"
	if s.dialog.mouseDisabled {
		mouseMode = "off"
	}
	values = append(values, settingValue{key: mouseSetting, value: mouseMode})
	worktreeChoice := "off"
	if s.dialog.worktreeDefault {
		worktreeChoice = "on"
	}
	values = append(values, settingValue{key: worktreeSetting, value: worktreeChoice})
	baseFetch := "on"
	if !s.dialog.baseFetch {
		baseFetch = "off"
	}
	values = append(values, settingValue{key: baseFetchSetting, value: baseFetch})
	proactive := "off"
	if s.dialog.proactive {
		proactive = "on"
	}
	values = append(values, settingValue{key: "coordination", value: proactive, proactive: true})
	notifications := "off"
	if s.dialog.notifications {
		notifications = "on"
	}
	values = append(values, settingValue{key: notificationsSetting, value: notifications})
	notifyFinished := "off"
	if s.dialog.notifyFinished {
		notifyFinished = "on"
	}
	values = append(values, settingValue{key: notifyFinishedSetting, value: notifyFinished})
	values = append(values, settingValue{key: editorSetting, value: s.dialog.editor.line()})
	return values
}

// applySettingsPrefs mirrors the just chosen preferences into the live
// session: a deliberate live preview of the staged choices, like the
// theme's. It is not a persistence receipt — a save that later fails
// reconciles these prefs back to the committed values on completion.
func (m *Model) applySettingsPrefs() {
	m.prefs.focusOnEnter = m.settings.dialog.enterFocuses
	m.prefs.arrowStep = m.settings.dialog.arrowStep
	m.prefs.comfortableRows = m.settings.dialog.comfortableRows
	m.prefs.fullLayout = m.settings.dialog.fullLayout
	m.prefs.hideHeader = m.settings.dialog.hideHeader
	m.prefs.hideStats = m.settings.dialog.hideStats
	m.prefs.mouseDisabled = m.settings.dialog.mouseDisabled
	m.prefs.terminalBackground = m.settings.dialog.terminalBackground
	m.prefs.baseFetchOff = !m.settings.dialog.baseFetch
	m.services.editor = m.settings.dialog.editor.line()
}

// cycleSetting steps the focused setting by one. The theme applies as it
// is stepped so the picker doubles as a live preview of the palette. A theme
// step pushes the pane background to tmux, which shells out, so it returns a
// command rather than blocking the update path.
func (s *settingsFeature) cycleSetting(h settingsHost, step int) tea.Cmd {
	changed := true
	switch s.dialog.field {
	case settingsFieldTool:
		count := len(s.dialog.toolNames)
		if count == 0 {
			return nil
		}
		s.dialog.toolIndex = (s.dialog.toolIndex + step + count) % count
	case settingsFieldTheme:
		// Stepping the theme is a manual choice; it wins over auto-detect
		// rather than being silently overridden on the next start.
		s.dialog.themeAuto = false
		s.dialog.themeIndex = (s.dialog.themeIndex + step + len(themes)) % len(themes)
		s.dialog.manualTheme = themes[s.dialog.themeIndex].Name
		s.dialog.dirty = true
		applyTheme(themes[s.dialog.themeIndex])
		SyncTerminalColors()
		return h.syncPaneTheme()
	case settingsFieldThemeAuto:
		s.dialog.themeAuto = !s.dialog.themeAuto
		name := s.dialog.manualTheme
		if s.dialog.themeAuto {
			name = autoThemeName(s.dialog.manualTheme, systheme.Detect())
		}
		s.dialog.themeIndex = themeIndex(name)
		s.dialog.dirty = true
		applyTheme(themes[s.dialog.themeIndex])
		SyncTerminalColors()
		return h.syncPaneTheme()
	case settingsFieldBackground:
		s.dialog.terminalBackground = !s.dialog.terminalBackground
		h.previewBackground(s.dialog.terminalBackground)
	case settingsFieldDensity:
		s.dialog.comfortableRows = !s.dialog.comfortableRows
	case settingsFieldSessionLayout:
		s.dialog.fullLayout = !s.dialog.fullLayout
	case settingsFieldHeader:
		s.dialog.hideHeader = !s.dialog.hideHeader
	case settingsFieldStats:
		s.dialog.hideStats = !s.dialog.hideStats
	case settingsFieldLayout:
		s.dialog.layoutSplit = !s.dialog.layoutSplit
	case settingsFieldQuickClose:
		s.dialog.quickCloseSend = !s.dialog.quickCloseSend
	case settingsFieldFocusKey:
		s.dialog.enterFocuses = !s.dialog.enterFocuses
	case settingsFieldArrowStep:
		s.dialog.arrowStep = !s.dialog.arrowStep
	case settingsFieldMouse:
		s.dialog.mouseDisabled = !s.dialog.mouseDisabled
	case settingsFieldWorktree:
		s.dialog.worktreeDefault = !s.dialog.worktreeDefault
	case settingsFieldBaseFetch:
		s.dialog.baseFetch = !s.dialog.baseFetch
	case settingsFieldCoordination:
		s.dialog.proactive = !s.dialog.proactive
	case settingsFieldNotify:
		s.dialog.notifications = !s.dialog.notifications
	case settingsFieldNotifyFinish:
		s.dialog.notifyFinished = !s.dialog.notifyFinished
	case settingsFieldEditor:
		s.dialog.editor.cycle(step)
	default:
		changed = false
	}
	if changed {
		s.dialog.dirty = true
	}
	return nil
}

func (m *Model) routeSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		return routed(m.handleSettingsLoaded(msg))

	case baseFetchedMsg:
		return routed(m, m.recordBaseFetch(msg))

	case editorsProbedMsg:
		m.settings.applyEditorsProbe(msg, m.mode == modeSettings)
		return routed(m, nil)

	case groupBaseStepMsg:
		m.handleGroupBaseStep(msg)
		return routed(m, nil)

	case catalogMsg:
		return routed(m, m.handleCatalog(msg))
	}
	return nil, nil, false
}
