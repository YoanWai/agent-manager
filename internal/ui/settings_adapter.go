package ui

import (
	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uisurface "github.com/YoanWai/agent-manager/internal/ui/surface"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// SettingSpecs is every key Settings writes and the values each takes, so
// the shell checks a value against the same choices the modal steps
// through. A key with no row is one the shell alone sets.
func SettingSpecs() ([]store.SettingSpec, error) {
	cfg, err := config.Default()
	if err != nil {
		return nil, err
	}
	specs := uisettings.Specs(cfg.AgentToolNames())
	out := make([]store.SettingSpec, len(specs))
	for i, spec := range specs {
		out[i] = store.SettingSpec(spec)
	}
	return out, nil
}

func (m *Model) openSettings() tea.Cmd {
	return m.openSettingsWithReader(uipersist.StoreWriter{Store: m.services.store})
}

// openSettingsWithReader builds the dialog from the cache and asks the
// store for anything another writer changed meanwhile.
func (m *Model) openSettingsWithReader(reader uipersist.Reader) tea.Cmd {
	s := &m.settings
	s.Gen++
	if len(m.services.cfg.Tools) == 0 {
		m.reportErr("no tools configured")
		return nil
	}
	m.clearErr()
	state := s.FromCache(m.services.cfg, uitheme.Current().Name)
	state.MarkBaseline()
	m.push(newSettingsSurface(m, state))
	probe := m.probeEditorsCmd()
	if s.Pending > 0 {
		return probe
	}
	return tea.Batch(uipersist.LoadCmd(uipersist.LoadRequest{Target: uipersist.DialogTarget, Generation: s.Gen}, reader), probe)
}

func (m *Model) handleSettingsLoaded(msg uipersist.LoadedMsg) (tea.Model, tea.Cmd) {
	return m, m.settings.UpdateLoad(settingsLoadHost{m}, msg)
}

// applySettingsResult runs the work a dialog key asked beyond its own
// state, and says whether the card closes: the links, the palette repaint,
// the saves, and the exits that close the dialog or start the in-place
// update.
func (m *Model) applySettingsResult(d *uisettings.State, result uisettings.Result) (tea.Cmd, bool) {
	cmd := m.runSettingsResult(d, result)
	switch result.Exit {
	case uisettings.OpenDocs:
		return openLink(docsURL), false
	case uisettings.OpenFeatureRequest:
		return openLink(uinotices.FeatureRequestURL()), false
	case uisettings.RequestCLISupport:
		return openLink(uinotices.SupportRequestURL()), false
	case uisettings.ReportBug:
		return openLink(uinotices.BugReportURL(m.release.Version)), false
	case uisettings.OpenSetup:
		request := openSetupMsg{Screen: m.screenID(), ForegroundGen: m.gens.foreground}
		request.Screen.depth--
		return func() tea.Msg { return request }, true
	case uisettings.Update:
		if m.release.Applying {
			return nil, false
		}
		if m.release.Latest != "" {
			// A successful swap quits to exec the new build, so
			// everything staged this visit must land first; the
			// update command follows the save's completion.
			m.release.Applying = true
			m.applySettingsPrefs(d)
			m.clearErr()
			return m.captureSettingsSave(d, false, true), false
		}
		return nil, true
	case uisettings.Save:
		return nil, true
	}
	return cmd, false
}

// runSettingsResult runs what a dialog key asked beyond its own state. A
// palette step pushes the pane background to tmux, which shells out, so it
// returns a command rather than blocking the update path.
func (m *Model) runSettingsResult(d *uisettings.State, result uisettings.Result) tea.Cmd {
	if result.Notice.Changed {
		m.setNotice(result.Notice)
	}
	switch {
	case result.Palette:
		if result.DetectTheme {
			d.ShowTheme(uitheme.Auto(d.ManualTheme, systheme.Detect()))
		}
		applyTheme(uitheme.Palettes[d.ThemeIndex])
		SyncTerminalColors()
		return m.syncPaneTheme()
	case result.Glyphs:
		m.prefs.glyphMode = d.GlyphMode
	case result.Background:
		m.prefs.terminalBackground = d.TerminalBackground
	case result.SaveHidden:
		return m.captureHiddenSave(d)
	case result.SaveKeys:
		return m.saveSettingsKeys(d)
	case result.LoadPrompts, result.SavePrompt != nil, result.DeletePrompt != "":
		return m.runPromptsResult(d, result)
	}
	return result.Cmd
}

// leaveSettings saves what the card chose as it comes off the stack, by esc,
// the save button or ctrl+c alike. Update starts the queued job.
func (m *Model) leaveSettings(d *uisettings.State) {
	m.applySettingsPrefs(d)
	m.rebuildRows()
	m.queueSettingsSave(d, true, false)
}

// captureSettingsSave enqueues the dialog's chosen preferences on the
// effect lane; the store writes run outside Update. The captured
// generation fences a completion against a newer dialog.
func (m *Model) captureSettingsSave(d *uisettings.State, includeHidden, followUpdate bool) tea.Cmd {
	m.queueSettingsSave(d, includeHidden, followUpdate)
	return m.nextEffectCmd()
}

func (m *Model) queueSettingsSave(d *uisettings.State, includeHidden, followUpdate bool) {
	s := &m.settings
	s.Gen++
	request := uipersist.SaveRequest{
		Values:       d.ChangedValues(),
		StagedUpdate: followUpdate,
		Generation:   s.Gen,
	}
	if hidden := d.HiddenList(); includeHidden && !d.HiddenAtBaseline(hidden) {
		request.Hidden = hidden
	}
	d.AdvanceBaseline(request.Values, request.Hidden)
	s.Cache.ApplyValues(request.Values)
	if request.Hidden != nil {
		s.Cache.ApplyHidden(request.Hidden)
	}
	s.Pending++
	m.enqueueEffect(request, 0, false)
}

func (m *Model) captureHiddenSave(d *uisettings.State) tea.Cmd {
	s := &m.settings
	s.Gen++
	request := uipersist.SaveRequest{
		Hidden:     d.HiddenList(),
		Generation: s.Gen,
	}
	d.AdvanceBaseline(nil, request.Hidden)
	s.Cache.ApplyHidden(request.Hidden)
	s.Pending++
	return m.submitSettings(request)
}

// saveSettingsKeys submits the picker's tables when they differ from what
// the store will hold once the key saves already on the lane land; the
// writes happen off the update path and a partial commit reconciles the
// runtime to the store.
func (m *Model) saveSettingsKeys(d *uisettings.State) tea.Cmd {
	expectedSession, expectedList := m.settingsKeyTables()
	request, ok := uipersist.KeysToSubmit(d.KeysToSave(expectedSession, expectedList))
	if !ok {
		return nil
	}
	return m.submitKeys(request)
}

func (m *Model) settingsKeyTables() (keybind.Table, keybind.Table) {
	expectedList, expectedSession := uipersist.Expected(m.services.listKeys, m.services.keys, m.queuedKeys())
	return expectedSession, expectedList
}

// queuedKeys lists the key saves on the lane, running first, in the order
// they commit.
func (m *Model) queuedKeys() []uipersist.KeysRequest {
	var requests []uipersist.KeysRequest
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(uipersist.KeysRequest); ok {
			requests = append(requests, request)
		}
	}
	return requests
}

func (m *Model) submitSettings(request uipersist.SaveRequest) tea.Cmd {
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

func (m *Model) submitKeys(request uipersist.KeysRequest) tea.Cmd {
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// keySaver writes one key table through the instance writer the job
// captured, or the store when the model was built without one.
func (s effectServices) keySaver() uipersist.KeySaver {
	if s.saveKeys != nil {
		return s.saveKeys
	}
	return s.store.SetKeys
}

// reconcileSettingsPrefs restores the live-session prefs to the committed
// read-back; keys whose read failed keep their current value.
func (m *Model) reconcileSettingsPrefs(restored []uisettings.Readback) string {
	return uipersist.Reconcile(restored, m.mirrorPreference)
}

// applySettingsPrefs mirrors the just chosen preferences into the live
// session: a deliberate live preview of the staged choices, like the
// theme's. It is not a persistence receipt — a save that later fails
// reconciles these prefs back to the committed values on completion.
func (m *Model) applySettingsPrefs(d *uisettings.State) {
	m.prefs.glyphMode = d.GlyphMode
	m.services.editor = d.Editor.Line()
	for _, key := range uisettings.FlagKeys() {
		m.mirrorFlag(key, d.Flag(key))
	}
}

func (m *Model) probeEditorsCmd() tea.Cmd {
	resolution, generation := m.captureEditorResolution(), m.settings.Gen
	return func() tea.Msg {
		msg := editorProbe(resolution)
		msg.Generation = generation
		return msg
	}
}

// probe walks PATH, which is I/O that Update must not do.
func editorProbe(r editorResolution) uisettings.EditorsProbe {
	probe := r.Probe()
	return uisettings.EditorsProbe{Found: probe.Found, Auto: probe.Auto}
}

// applyEditorsProbe keeps the answer for the rows later loads rebuild, and
// fills the row of the visit that asked while the dialog is open.
func (m *Model) applyEditorsProbe(msg uisettings.EditorsProbe) {
	m.settings.Cache.SetEditors(&msg)
	if s, open := surfaceOf[*uisettings.Surface](m); open && msg.Generation == m.settings.Gen {
		s.State().Editor.ApplyProbe(msg)
	}
}

func (m *Model) routeSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uipersist.LoadedMsg:
		return routed(m.handleSettingsLoaded(msg))

	case uilaunch.BaseFetchedMsg:
		return routed(m, m.recordBaseFetch(msg))

	case uisettings.EditorsProbe:
		m.applyEditorsProbe(msg)
		return routed(m, nil)

	case uipersist.SavedMsg:
		m.handleSavedPromptsLoaded(msg)
		return routed(m, nil)

	case uilaunch.BaseStepMsg:
		m.handleGroupBaseStep(msg)
		return routed(m, nil)

	case uilaunch.CatalogMsg:
		return routed(m, m.handleCatalog(msg))
	}
	return nil, nil, false
}

// defaultSplitLayout reports whether review mode should open in split
// (side-by-side) layout. Split is the default; a stored "unified" choice
// opts out. A store error is surfaced but still yields the split default.
func (m *Model) defaultSplitLayout() bool {
	split, err := uipersist.StoredSplitLayout(m.services.store)
	if err != nil {
		m.reportErr("reading diff layout setting: " + err.Error())
	}
	return split
}

// storedTerminalBackground reads the background row. A store error is
// surfaced but still yields the painted default.
func (m *Model) storedTerminalBackground() bool {
	on, err := uipersist.StoredTerminalBackground(m.services.store)
	if err != nil {
		m.reportErr("reading background setting: " + err.Error())
	}
	return on
}

// enterFocuses reports which key opens a session where. Enter focuses the
// preview and A attaches full screen by default; a stored "attach" choice
// swaps the pair. Cached on the model because the footer reads it every
// frame.
func (m *Model) enterFocuses() bool {
	return m.prefs.focusOnEnter
}

// mirrorPreference copies one stored setting into the running manager; a
// key the frame never reads stays in the cache alone.
func (m *Model) mirrorPreference(key, value string) {
	mirrored := uisettings.Mirror(key, value)
	switch {
	case mirrored.Flag != nil:
		m.mirrorFlag(key, *mirrored.Flag)
	case key == uisettings.EditorSetting:
		m.services.editor = mirrored.Editor
	case key == uisettings.GlyphModeSetting:
		m.prefs.glyphMode = mirrored.GlyphMode
	}
}

// mirrorFlag is the one place a two-way row meets its preferences field.
func (m *Model) mirrorFlag(key string, on bool) {
	switch key {
	case uisettings.FocusKeySetting:
		m.prefs.focusOnEnter = on
	case uisettings.ArrowStepSetting:
		m.prefs.arrowStep = on
	case uisettings.ListDensitySetting:
		m.prefs.comfortableRows = on
	case uisettings.SessionLayoutSetting:
		m.prefs.fullLayout = on
	case uisettings.HideHeaderSetting:
		m.prefs.hideHeader = on
	case uisettings.HideStatsSetting:
		m.prefs.hideStats = on
	case uisettings.MouseSetting:
		m.prefs.mouseDisabled = on
	case uisettings.BackgroundSetting:
		m.prefs.terminalBackground = on
	case uisettings.BaseFetchSetting:
		m.prefs.baseFetchOff = !on
	}
}

func (m *Model) applyCachedSettingsPrefs() {
	for _, key := range uisettings.Keys() {
		m.mirrorPreference(key, m.settings.Cache.Value(key))
	}
}

func (m *Model) applyPromptsRead(dialogGen uint64, read uisettings.PromptsRead) {
	card, open := surfaceOf[*uisettings.Surface](m)
	if !open || dialogGen != m.settings.Gen {
		return
	}
	notice := card.State().LoadedPrompts(read)
	if notice.Changed {
		m.setNotice(notice)
	}
}

// runPromptsResult runs the catalog work a panel key asked for: reads go
// straight to the store off Update, writes go on the effect lane.
func (m *Model) runPromptsResult(d *uisettings.State, result uisettings.Result) tea.Cmd {
	request, read, ok := uipersist.Asked(result, d.Prompts.Gen(), m.settings.Gen)
	switch {
	case !ok:
		return nil
	case read:
		return uipersist.SavedCmd(m.services.store, uipersist.SavedForSettings, request.Gen, request.DialogGen)
	}
	m.enqueueEffect(request, 0, false)
	return m.nextEffectCmd()
}

// applyPromptsResult hands a committed catalog change back to the panel
// that asked, landing on the row the write named.
func (m *Model) applyPromptsResult(request uipersist.PromptRequest, result uipersist.PromptResult, err error) {
	selected := ""
	if request.Save != nil {
		selected = request.Save.SelectedName()
	}
	m.applyPromptsRead(request.DialogGen, uisettings.PromptsRead{
		Gen: request.Gen, Rows: uisettings.PromptRows(presetRows(result.Rows)), Err: err,
		Mutation: true, Committed: result.Committed, Selected: selected,
	})
}

func (m *Model) setNotice(notice uisettings.Notice) {
	if notice.Text == "" {
		m.clearErr()
		return
	}
	m.reportErr(notice.Text)
}

// handleSavedPromptsLoaded hands a catalog read to the surface that asked,
// while it is still the one that asked.
func (m *Model) handleSavedPromptsLoaded(msg uipersist.SavedMsg) {
	switch msg.Target {
	case uipersist.SavedForSettings:
		m.applyPromptsRead(msg.DialogGen, uisettings.PromptsRead{Gen: msg.Gen, Rows: uisettings.PromptRows(presetRows(msg.Rows)), Err: msg.Err})
	case uipersist.SavedForForm:
		if f, ok := m.formFor(int(msg.Gen)); ok {
			f.SetSaved(spawnSaved(msg.Rows), msg.Err)
		}
	case uipersist.SavedForQuick:
		if q, ok := m.quickFor(int(msg.Gen)); ok {
			q.LoadSaved(spawnSaved(msg.Rows), msg.Err)
		}
	case uipersist.SavedForFocus:
		m.loadFocusSaved(msg)
	}
}

func spawnSaved(rows []store.SessionPreset) []spawn.Saved {
	saved := make([]spawn.Saved, len(rows))
	for i, row := range rows {
		saved[i] = spawn.Saved{Name: row.Name, Text: row.Instructions}
	}
	return saved
}

func presetRows(rows []store.SessionPreset) []uisettings.Preset {
	presets := make([]uisettings.Preset, len(rows))
	for i, row := range rows {
		presets[i] = uisettings.Preset{Name: row.Name, Text: row.Instructions}
	}
	return presets
}

// settingsEnvs are the defined environment profiles the dialog lists.
func (m *Model) settingsEnvs() []uisettings.Env {
	envs := make([]uisettings.Env, len(m.workspace.envProfiles))
	for i, name := range m.workspace.envProfiles {
		envs[i] = uisettings.Env{Name: name, Keys: m.workspace.envKeys[name]}
	}
	return envs
}

// paintSettings frames the dialog in the root's card chrome.
func (m *Model) paintSettings(d *uisettings.State) string {
	frame := d.View(uisettings.ViewContext{
		GlyphMode:  m.prefs.glyphMode,
		Session:    m.services.keys,
		List:       m.services.listKeys,
		Envs:       m.settingsEnvs(),
		Version:    m.release.Version,
		Latest:     m.release.Latest,
		Applying:   m.release.Applying,
		Height:     m.layout.height,
		InnerWidth: uiframe.CardInnerWidth(m.cardWidth()),
		Chrome:     m.flexChromeRows,
		Styles:     uisettings.CurrentStyles(),
		TextInput:  uiframe.TextInputView,
		TextArea:   uiframe.TextAreaView,
	})
	switch frame.Card {
	case uisettings.Confirm:
		return m.confirmCard(frame.Title, frame.Question, frame.Consequence, frame.Destructive, frame.Answer)
	case uisettings.Fixed:
		return m.CardSized(m.cardWidth(), frame.Title, frame.Body, frame.Hints)
	}
	return m.CardSized(m.flexCardWidth(frame.Title, frame.Body, frame.Hints), frame.Title, frame.Body, frame.Hints)
}

func newSettingsSurface(m *Model, state uisettings.State) *uisettings.Surface {
	return uisettings.NewSurface(settingsHost{m}, state)
}

// columnClicker is the optional capability of a surface whose card acts on
// where in a line a click lands: the root hands it the column too. It
// bridges until surface.Click carries the column for every card.
type columnClicker interface {
	ClickAt(h uisurface.Host, col, line int) tea.Cmd
}

var _ columnClicker = (*uisettings.Surface)(nil)

// settingsHost is the root as the Settings card reads it: the live key
// tables, the cached store values and the update state a key reads.
type settingsHost struct{ m *Model }

func (h settingsHost) Context() uisettings.Context {
	session, list := h.m.settingsKeyTables()
	return uisettings.Context{
		Tools:       uipersist.Tools(h.m.services.cfg),
		Hidden:      h.m.settings.Cache.Hidden(),
		DefaultTool: h.m.settings.Cache.Value(uisettings.DefaultToolSetting),
		Session:     session,
		List:        list,
		UpdateReady: h.m.release.Latest != "",
	}
}

// Apply runs what a key asked beyond the dialog's own state and says
// whether the card closes: the links, the palette repaint, the saves, and
// the exits that close the dialog or start the in-place update.
func (h settingsHost) Apply(d *uisettings.State, result uisettings.Result) (tea.Cmd, bool) {
	return h.m.applySettingsResult(d, result)
}

// Leave applies and queues the save of what the card chose.
func (h settingsHost) Leave(d *uisettings.State) { h.m.leaveSettings(d) }

func (h settingsHost) Paint(d *uisettings.State) string { return h.m.paintSettings(d) }

// settingsLoadHost is the root as a landed settings read reaches it: the
// persist package decides which surface may still take the read.
type settingsLoadHost struct{ m *Model }

func (h settingsLoadHost) RebuildSettings(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	m := h.m
	card, open := surfaceOf[*uisettings.Surface](m)
	if !open || !m.settings.Fresh(generation) || card.State().Busy() {
		return uipersist.LoadOutcome{}
	}
	m.settings.Take(result)
	field := card.State().Field
	card.SetState(m.settings.FromCache(m.services.cfg, uitheme.Current().Name))
	var follow tea.Cmd
	// A manual theme the shell changed meanwhile repaints here, so esc
	// keeps it instead of writing back the palette cached at startup.
	if d := card.State(); !d.ThemeAuto && uitheme.Current().Name != d.ManualTheme {
		d.ShowTheme(d.ManualTheme)
		applyTheme(uitheme.Palettes[d.ThemeIndex])
		SyncTerminalColors()
		follow = m.syncPaneTheme()
	}
	card.State().MarkBaseline()
	card.State().Field = field
	return uipersist.LoadOutcome{Built: true, Follow: follow}
}

func (h settingsLoadHost) RebuildForm(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	m := h.m
	f, ok := m.formFor(int(generation))
	if !ok || f.DefaultsTouched() || m.settings.Pending > 0 {
		return uipersist.LoadOutcome{}
	}
	m.settings.Take(result)
	m.refreshFormChoicePrefs()
	return uipersist.LoadOutcome{Built: true, Follow: tea.Batch(m.applyCachedFormDefaults(), m.formWorktreeProbeCmd(false))}
}

func (h settingsLoadHost) RebuildQuick(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	m := h.m
	q, ok := m.quickFor(int(generation))
	if !ok || q.DefaultsTouched() || m.settings.Pending > 0 {
		return uipersist.LoadOutcome{}
	}
	m.settings.Take(result)
	m.refreshQuickChoicePrefs()
	return uipersist.LoadOutcome{Built: true, Follow: tea.Batch(m.applyCachedQuickDefaults(), m.quickWorktreeProbeCmd(false))}
}

func (h settingsLoadHost) SettleLoad(err error, follow tea.Cmd) tea.Cmd {
	m := h.m
	m.applyCachedSettingsPrefs()
	m.rebuildRows()
	if err != nil {
		m.reportErr(err.Error())
	} else {
		m.clearErr()
	}
	return follow
}

// settingsSaveHost is the root as a Settings completion reaches it: the
// persist package decides what committed, and every effect here answers in
// the manager's own state.
type settingsSaveHost struct{ m *Model }

func (h settingsSaveHost) ReportErr(text string) { h.m.reportErr(text) }

func (h settingsSaveHost) Caveat(text string) { h.m.reportErr(h.m.errBar.text + "; " + text) }

func (h settingsSaveHost) MirrorPrefs(values []uisettings.Readback) string {
	return h.m.reconcileSettingsPrefs(values)
}

func (h settingsSaveHost) Restore(values []uisettings.Readback, hiddenRaw string, hiddenErr error, editors *uisettings.EditorsProbe) {
	if card, open := surfaceOf[*uisettings.Surface](h.m); open {
		card.State().Restore(values, hiddenRaw, hiddenErr, editors)
	}
}

func (h settingsSaveHost) Update(staged bool) tea.Cmd {
	if !staged {
		h.m.release.Applying = false
		return nil
	}
	return h.m.release.Apply()
}

// SaveKeys re-applies the saved tables to the sessions already running.
func (h settingsSaveHost) SaveKeys(list, session keybind.Table, listChanged, sessionChanged bool) tea.Cmd {
	m := h.m
	if listChanged {
		m.services.listKeys = list
	}
	if sessionChanged {
		m.services.keys = session
		m.services.tmux.SetSessionKeys(session)
		return func() tea.Msg { return m.refreshExistingSessionUX() }
	}
	return nil
}
