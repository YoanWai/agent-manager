package ui

import (
	"net/url"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
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

func (m *Model) settingsContext() uisettings.Context {
	session, list := m.settingsKeyTables()
	return uisettings.Context{
		Tools:       uipersist.Tools(m.services.cfg),
		Hidden:      m.settings.Cache.Hidden(),
		DefaultTool: m.settings.Cache.Value(uisettings.DefaultToolSetting),
		Session:     session,
		List:        list,
		UpdateReady: m.release.Latest != "",
	}
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
	return m, m.settings.UpdateLoad(settingsLoadRoot{m}, msg)
}

// settingsLoadRoot is the root as a landed settings read reaches it: the
// persist package decides which surface may still take the read, and each
// rebuild here answers with the work it asks for.
type settingsLoadRoot struct{ *Model }

func (r settingsLoadRoot) RebuildSettings(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	card, open := surfaceOf[*uisettings.Surface](r.Model)
	if !open || !r.settings.Fresh(generation) || card.State().Busy() {
		return uipersist.LoadOutcome{}
	}
	r.settings.Take(result)
	field := card.State().Field
	card.SetState(r.settings.FromCache(r.services.cfg, uitheme.Current().Name))
	var follow tea.Cmd
	// A manual theme the shell changed meanwhile repaints here, so esc
	// keeps it instead of writing back the palette cached at startup.
	if d := card.State(); !d.ThemeAuto && uitheme.Current().Name != d.ManualTheme {
		d.ShowTheme(d.ManualTheme)
		applyTheme(uitheme.Palettes[d.ThemeIndex])
		SyncTerminalColors()
		follow = r.syncPaneTheme()
	}
	card.State().MarkBaseline()
	card.State().Field = field
	return uipersist.LoadOutcome{Built: true, Follow: follow}
}

func (r settingsLoadRoot) RebuildForm(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	f, ok := r.formFor(int(generation))
	if !ok || f.DefaultsTouched() || r.settings.Pending > 0 {
		return uipersist.LoadOutcome{}
	}
	r.settings.Take(result)
	r.refreshFormChoicePrefs()
	return uipersist.LoadOutcome{Built: true, Follow: tea.Batch(r.applyCachedFormDefaults(), r.formWorktreeProbeCmd(false))}
}

func (r settingsLoadRoot) RebuildQuick(result uipersist.LoadResult, generation uint64) uipersist.LoadOutcome {
	q, ok := r.quickFor(int(generation))
	if !ok || q.DefaultsTouched() || r.settings.Pending > 0 {
		return uipersist.LoadOutcome{}
	}
	r.settings.Take(result)
	r.refreshQuickChoicePrefs()
	return uipersist.LoadOutcome{Built: true, Follow: tea.Batch(r.applyCachedQuickDefaults(), r.quickWorktreeProbeCmd(false))}
}

func (r settingsLoadRoot) SettleLoad(err error, follow tea.Cmd) tea.Cmd {
	r.applyCachedSettingsPrefs()
	r.rebuildRows()
	if err != nil {
		r.reportErr(err.Error())
	} else {
		r.clearErr()
	}
	return follow
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
		return openLink(featureRequestURL()), false
	case uisettings.RequestCLISupport:
		return openLink(requestCLISupportURL()), false
	case uisettings.ReportBug:
		return openLink(bugReportURL(m.release.Version)), false
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
	expectedSession, expectedList := m.services.keys, m.services.listKeys
	for _, pending := range m.queuedKeys() {
		if pending.ListChanged {
			expectedList = pending.List
		}
		if pending.SessionChanged {
			expectedSession = pending.Session
		}
	}
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

// settingsSaveRoot is the root as a Settings completion reaches it: the
// persist package decides what committed, and every effect here answers in
// the manager's own state.
type settingsSaveRoot struct{ *Model }

func (r settingsSaveRoot) ReportErr(text string) { r.reportErr(text) }

func (r settingsSaveRoot) Caveat(text string) { r.reportErr(r.errBar.text + "; " + text) }

func (r settingsSaveRoot) MirrorPrefs(values []uisettings.Readback) string {
	return r.reconcileSettingsPrefs(values)
}

func (r settingsSaveRoot) Restore(values []uisettings.Readback, hiddenRaw string, hiddenErr error, editors *uisettings.EditorsProbe) {
	if card, open := surfaceOf[*uisettings.Surface](r.Model); open {
		card.State().Restore(values, hiddenRaw, hiddenErr, editors)
	}
}

func (r settingsSaveRoot) Update(staged bool) tea.Cmd {
	if !staged {
		r.release.Applying = false
		return nil
	}
	return r.release.Apply()
}

// SaveKeys re-applies the saved tables to the sessions already running.
func (r settingsSaveRoot) SaveKeys(list, session keybind.Table, listChanged, sessionChanged bool) tea.Cmd {
	if listChanged {
		r.services.listKeys = list
	}
	if sessionChanged {
		r.services.keys = session
		r.services.tmux.SetSessionKeys(session)
		return func() tea.Msg { return r.refreshExistingSessionUX() }
	}
	return nil
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
	failed := 0
	for _, value := range restored {
		if value.Err != nil {
			failed++
			continue
		}
		m.mirrorPreference(value.Key, value.Value)
	}
	return uipersist.ReadFailureNote(failed)
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
	resolution, generation := detectedEditors(), m.settings.Gen
	return func() tea.Msg {
		msg := resolution.probe()
		msg.Generation = generation
		return msg
	}
}

// probe walks PATH, which is I/O that Update must not do.
func (r editorResolution) probe() uisettings.EditorsProbe {
	var found []string
	if r.lookup != nil {
		for _, name := range r.gui {
			if _, err := r.lookup(name); err == nil {
				found = append(found, name)
			}
		}
	}
	for _, line := range []string{r.visual, r.editor} {
		if line = strings.TrimSpace(line); line != "" && !slices.Contains(found, line) {
			found = append(found, line)
		}
	}
	r.configured = ""
	return uisettings.EditorsProbe{Found: found, Auto: r.editorLine()}
}

// applyEditorsProbe keeps the answer for the rows later loads rebuild, and
// fills the row of the visit that asked while the dialog is open.
func (m *Model) applyEditorsProbe(msg uisettings.EditorsProbe) {
	m.settings.Cache.SetEditors(&msg)
	if s, open := surfaceOf[*uisettings.Surface](m); open && msg.Generation == m.settings.Gen {
		s.State().Editor.ApplyProbe(msg)
	}
}

// requestCLISupportURL opens a prefilled feature request for another CLI.
func requestCLISupportURL() string {
	body := "**What are you trying to do**\n\n" +
		"I want agent-manager to support another coding CLI.\n\n" +
		"**What you have in mind**\n\n" +
		"CLI name:\nHow to launch it:\nResume / session flags (if any):\n\n" +
		"**Area**\nConfig and tool support\n"
	return uinotices.RepoURL + "/issues/new?body=" + url.QueryEscape(body)
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
