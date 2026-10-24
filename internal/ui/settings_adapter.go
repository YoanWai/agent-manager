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
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
)

type (
	settingsState    = uisettings.State
	settingValue     = uisettings.Value
	restoredValue    = uisettings.Readback
	editorsProbedMsg = uisettings.EditorsProbe
)

// settingsCache is the feature's cache of the stored settings. The spawn,
// quick and connections call sites and the settings tests read it by the
// lower-case names it had before the cache moved into the package.
type settingsCache struct{ uisettings.Cache }

func (c settingsCache) value(key string) string { return c.Value(key) }

func (c *settingsCache) applyValues(values []settingValue) { c.ApplyValues(values) }

func (c *settingsCache) applyHidden(names []string) { c.ApplyHidden(names) }

func (c *settingsCache) applyReadback(values []restoredValue, hiddenRaw string, hiddenErr error) {
	c.ApplyReadback(values, hiddenRaw, hiddenErr)
}

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

func knownTools(cfg config.Config) map[string]bool {
	known := make(map[string]bool, len(cfg.Tools))
	for name := range cfg.Tools {
		known[name] = true
	}
	return known
}

func settingsTools(cfg config.Config) uisettings.Tools {
	return uisettings.Tools{Agents: cfg.AgentToolNames(), Known: knownTools(cfg)}
}

func (s *settingsFeature) stateFromCache(cfg config.Config) settingsState {
	return uisettings.FromSnapshot(uisettings.Snapshot{
		Values:  s.cache.Values(),
		Hidden:  s.cache.Hidden(),
		Editors: s.cache.Editors(),
		Tools:   settingsTools(cfg),
		Theme:   current().Name,
	})
}

func (m *Model) settingsContext() uisettings.Context {
	session, list := m.settingsKeyTables()
	return uisettings.Context{
		Tools:       settingsTools(m.services.cfg),
		Hidden:      m.settings.cache.Hidden(),
		DefaultTool: m.settings.cache.Value(uisettings.DefaultToolSetting),
		Session:     session,
		List:        list,
		UpdateReady: m.release.Latest != "",
	}
}

func (m *Model) openSettings() tea.Cmd {
	return m.openSettingsWithReader(storeSettingWriter{st: m.services.store})
}

// openSettingsWithReader builds the dialog from the cache and asks the
// store for anything another writer changed meanwhile.
func (m *Model) openSettingsWithReader(reader settingsValueReader) tea.Cmd {
	s := &m.settings
	s.gen++
	if len(m.services.cfg.Tools) == 0 {
		m.reportErr("no tools configured")
		return nil
	}
	m.clearErr()
	state := s.stateFromCache(m.services.cfg)
	state.MarkBaseline()
	m.push(newSettingsSurface(m, state))
	probe := s.probeEditorsCmd()
	if s.pending > 0 {
		return probe
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadDialog, generation: s.gen}, reader), probe)
}

func (m *Model) handleSettingsLoaded(msg settingsLoadedMsg) (tea.Model, tea.Cmd) {
	var follow tea.Cmd
	switch msg.request.target {
	case settingsLoadDialog:
		card, open := surfaceOf[*uisettings.Surface](m)
		if msg.request.generation != m.settings.gen || !open ||
			card.State().Busy() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.ApplyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		field := card.State().Field
		card.SetState(m.settings.stateFromCache(m.services.cfg))
		// A manual theme the shell changed meanwhile repaints here, so esc
		// keeps it instead of writing back the palette cached at startup.
		if d := card.State(); !d.ThemeAuto && current().Name != d.ManualTheme {
			d.ShowTheme(d.ManualTheme)
			applyTheme(themes[d.ThemeIndex])
			SyncTerminalColors()
			follow = m.syncPaneTheme()
		}
		card.State().MarkBaseline()
		card.State().Field = field
	case settingsLoadForm:
		f, ok := m.formFor(int(msg.request.generation))
		if !ok || f.DefaultsTouched() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.ApplyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshFormChoicePrefs()
		follow = tea.Batch(m.applyCachedFormDefaults(), m.formWorktreeProbeCmd(false))
	case settingsLoadQuick:
		q, ok := m.quickFor(int(msg.request.generation))
		if !ok || q.DefaultsTouched() || m.settings.pending > 0 {
			return m, nil
		}
		m.settings.cache.ApplyReadback(msg.result.values, msg.result.hiddenRaw, msg.result.hiddenErr)
		m.refreshQuickChoicePrefs()
		follow = tea.Batch(m.applyCachedQuickDefaults(), m.quickWorktreeProbeCmd(false))
	default:
		return m, nil
	}
	m.applyCachedSettingsPrefs()
	m.rebuildRows()
	if msg.err != nil {
		m.reportErr(msg.err.Error())
	} else {
		m.clearErr()
	}
	return m, follow
}

// applySettingsResult runs the work a dialog key asked beyond its own
// state, and says whether the card closes: the links, the palette repaint,
// the saves, and the exits that close the dialog or start the in-place
// update.
func (m *Model) applySettingsResult(d *settingsState, result uisettings.Result) (tea.Cmd, bool) {
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
func (m *Model) runSettingsResult(d *settingsState, result uisettings.Result) tea.Cmd {
	if result.Notice.Changed {
		m.setNotice(result.Notice)
	}
	switch {
	case result.Palette:
		if result.DetectTheme {
			d.ShowTheme(autoThemeName(d.ManualTheme, systheme.Detect()))
		}
		applyTheme(themes[d.ThemeIndex])
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
func (m *Model) leaveSettings(d *settingsState) {
	m.applySettingsPrefs(d)
	m.rebuildRows()
	m.queueSettingsSave(d, true, false)
}

// captureSettingsSave enqueues the dialog's chosen preferences on the
// effect lane; the store writes run outside Update. The captured
// generation fences a completion against a newer dialog.
func (m *Model) captureSettingsSave(d *settingsState, includeHidden, followUpdate bool) tea.Cmd {
	m.queueSettingsSave(d, includeHidden, followUpdate)
	return m.nextEffectCmd()
}

func (m *Model) queueSettingsSave(d *settingsState, includeHidden, followUpdate bool) {
	s := &m.settings
	s.gen++
	request := settingsRequest{
		values:       d.ChangedValues(),
		followUpdate: followUpdate,
		generation:   s.gen,
	}
	if hidden := d.HiddenList(); includeHidden && !d.HiddenAtBaseline(hidden) {
		request.hidden = hidden
	}
	d.AdvanceBaseline(request.values, request.hidden)
	s.cache.ApplyValues(request.values)
	if request.hidden != nil {
		s.cache.ApplyHidden(request.hidden)
	}
	s.pending++
	m.enqueueEffect(request, 0, false)
}

func (m *Model) captureHiddenSave(d *settingsState) tea.Cmd {
	s := &m.settings
	s.gen++
	request := settingsRequest{
		hidden:     d.HiddenList(),
		generation: s.gen,
	}
	d.AdvanceBaseline(nil, request.hidden)
	s.cache.ApplyHidden(request.hidden)
	s.pending++
	return m.submitSettings(request)
}

// saveSettingsKeys submits the picker's tables when they differ from what
// the store will hold once the key saves already on the lane land; the
// writes happen off the update path and a partial commit reconciles the
// runtime to the store.
func (m *Model) saveSettingsKeys(d *settingsState) tea.Cmd {
	expectedSession, expectedList := m.settingsKeyTables()
	change, ok := d.KeysToSave(expectedSession, expectedList)
	if !ok {
		return nil
	}
	return m.submitKeys(keysRequest{
		list:           change.List,
		session:        change.Session,
		listChanged:    change.ListChanged,
		sessionChanged: change.SessionChanged,
	})
}

func (m *Model) settingsKeyTables() (keybind.Table, keybind.Table) {
	expectedSession, expectedList := m.services.keys, m.services.listKeys
	for _, pending := range m.queuedKeys() {
		if pending.listChanged {
			expectedList = pending.list
		}
		if pending.sessionChanged {
			expectedSession = pending.session
		}
	}
	return expectedSession, expectedList
}

// queuedKeys lists the key saves on the lane, running first, in the order
// they commit.
func (m *Model) queuedKeys() []keysRequest {
	var requests []keysRequest
	for _, job := range append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...) {
		if job == nil {
			continue
		}
		if request, ok := job.Request.(keysRequest); ok {
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

// applySettingsPrefs mirrors the just chosen preferences into the live
// session: a deliberate live preview of the staged choices, like the
// theme's. It is not a persistence receipt — a save that later fails
// reconciles these prefs back to the committed values on completion.
func (m *Model) applySettingsPrefs(d *settingsState) {
	m.prefs.glyphMode = d.GlyphMode
	m.services.editor = d.Editor.Line()
	for _, key := range uisettings.FlagKeys() {
		m.mirrorFlag(key, d.Flag(key))
	}
}

func (s *settingsFeature) probeEditorsCmd() tea.Cmd {
	resolution, generation := detectedEditors(), s.gen
	return func() tea.Msg {
		msg := resolution.probe()
		msg.Generation = generation
		return msg
	}
}

// probe walks PATH, which is I/O that Update must not do.
func (r editorResolution) probe() editorsProbedMsg {
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
	return editorsProbedMsg{Found: found, Auto: r.resolve()}
}

// applyEditorsProbe keeps the answer for the rows later loads rebuild, and
// fills the row of the visit that asked while the dialog is open.
func (m *Model) applyEditorsProbe(msg editorsProbedMsg) {
	m.settings.cache.SetEditors(&msg)
	if s, open := surfaceOf[*uisettings.Surface](m); open && msg.Generation == m.settings.gen {
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
	return repoURL + "/issues/new?body=" + url.QueryEscape(body)
}

func (m *Model) routeSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		return routed(m.handleSettingsLoaded(msg))

	case baseFetchedMsg:
		return routed(m, m.recordBaseFetch(msg))

	case editorsProbedMsg:
		m.applyEditorsProbe(msg)
		return routed(m, nil)

	case savedPromptsLoadedMsg:
		m.handleSavedPromptsLoaded(msg)
		return routed(m, nil)

	case groupBaseStepMsg:
		m.handleGroupBaseStep(msg)
		return routed(m, nil)

	case catalogMsg:
		return routed(m, m.handleCatalog(msg))
	}
	return nil, nil, false
}
