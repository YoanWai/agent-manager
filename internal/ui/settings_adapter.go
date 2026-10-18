package ui

import (
	"net/url"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/systheme"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

type (
	settingsState    = uisettings.State
	settingValue     = uisettings.Value
	restoredValue    = uisettings.Readback
	editorsProbedMsg = uisettings.EditorsProbe
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
		Values:  s.cache.values,
		Hidden:  s.cache.hidden,
		Editors: s.cache.editors,
		Tools:   settingsTools(cfg),
		Theme:   current().Name,
	})
}

func (m *Model) settingsContext() uisettings.Context {
	session, list := m.settingsKeyTables()
	return uisettings.Context{
		Tools:       settingsTools(m.services.cfg),
		Hidden:      m.settings.cache.hidden,
		DefaultTool: m.settings.cache.value(uisettings.DefaultToolSetting),
		Session:     session,
		List:        list,
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
	s.dialog = s.stateFromCache(m.services.cfg)
	s.dialog.MarkBaseline()
	m.mode = modeSettings
	probe := s.probeEditorsCmd()
	if s.pending > 0 {
		return probe
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadDialog, generation: s.gen}, reader), probe)
}

// handleSettingsKey runs the dialog's key and the work it asks for: the
// links, the palette repaint, the saves, and the exits that close the
// dialog or start the in-place update.
func (m *Model) handleSettingsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	result := m.settings.dialog.Key(msg, m.settingsContext())
	cmd := m.runSettingsResult(result)
	switch result.Exit {
	case uisettings.OpenDocs:
		return m, openLink(docsURL)
	case uisettings.OpenFeatureRequest:
		return m, openLink(featureRequestURL())
	case uisettings.RequestCLISupport:
		return m, openLink(requestCLISupportURL())
	case uisettings.ReportBug:
		return m, openLink(bugReportURL(m.update.version))
	case uisettings.Update:
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
			return m, m.captureSettingsSave(false, true)
		}
		return m.saveAndCloseSettings()
	case uisettings.Save:
		return m.saveAndCloseSettings()
	}
	return m, cmd
}

// runSettingsResult runs what a dialog key asked beyond its own state. A
// palette step pushes the pane background to tmux, which shells out, so it
// returns a command rather than blocking the update path.
func (m *Model) runSettingsResult(result uisettings.Result) tea.Cmd {
	if result.Notice.Changed {
		m.setNotice(result.Notice)
	}
	d := &m.settings.dialog
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
		return m.captureHiddenSave()
	case result.SaveKeys:
		return m.saveSettingsKeys()
	case result.LoadPrompts, result.SavePrompt != nil, result.DeletePrompt != "":
		return m.runPromptsResult(result)
	}
	return result.Cmd
}

func (m *Model) handleSettingsClick(x, y int) (tea.Model, tea.Cmd) {
	d := &m.settings.dialog
	if d.InSubpanel() {
		return m, nil
	}
	left := m.layout.cardLeft + 1 + cardPaddingX
	inner := m.layout.cardRight - m.layout.cardLeft - cardChromeX - 2*cardPaddingX
	row := y - m.layout.cardTop - 2
	if x < left || x >= left+inner {
		return m, nil
	}
	if d.GlyphRow >= 0 && row == d.GlyphRow && x < left+d.GlyphRowWidth {
		d.Field = uisettings.FieldGlyphs
		return m, m.runSettingsResult(d.Cycle(1))
	}
	if row == d.DocsRow {
		d.Field = uisettings.FieldDocs
		return m, openLink(docsURL)
	}
	if d.PromptsRow >= 0 && row == d.PromptsRow {
		return m, m.runSettingsResult(d.OpenPrompts())
	}
	if row == d.SaveRow && x < left+d.SaveRowWidth {
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
	s := &m.settings
	s.gen++
	request := settingsRequest{
		values:       s.dialog.ChangedValues(),
		followUpdate: followUpdate,
		generation:   s.gen,
	}
	if hidden := s.dialog.HiddenList(); includeHidden && !s.dialog.HiddenAtBaseline(hidden) {
		request.hidden = hidden
	}
	s.dialog.AdvanceBaseline(request.values, request.hidden)
	s.cache.applyValues(request.values)
	if request.hidden != nil {
		s.cache.applyHidden(request.hidden)
	}
	s.pending++
	return m.submitSettings(request)
}

func (m *Model) captureHiddenSave() tea.Cmd {
	s := &m.settings
	s.gen++
	request := settingsRequest{
		hidden:     s.dialog.HiddenList(),
		generation: s.gen,
	}
	s.dialog.AdvanceBaseline(nil, request.hidden)
	s.cache.applyHidden(request.hidden)
	s.pending++
	return m.submitSettings(request)
}

// saveSettingsKeys submits the picker's tables when they differ from what
// the store will hold once the key saves already on the lane land; the
// writes happen off the update path and a partial commit reconciles the
// runtime to the store.
func (m *Model) saveSettingsKeys() tea.Cmd {
	expectedSession, expectedList := m.settingsKeyTables()
	change, ok := m.settings.dialog.KeysToSave(expectedSession, expectedList)
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

// applySettingsPrefs mirrors the just chosen preferences into the live
// session: a deliberate live preview of the staged choices, like the
// theme's. It is not a persistence receipt — a save that later fails
// reconciles these prefs back to the committed values on completion.
func (m *Model) applySettingsPrefs() {
	d := &m.settings.dialog
	m.prefs.glyphMode = d.GlyphMode
	m.services.editor = d.Editor.Line()
	for _, key := range uisettings.FlagKeys() {
		m.mirrorFlag(key, d.Flag(key))
	}
}

// viewSettings frames the dialog in the root's card chrome.
func (m *Model) viewSettings() string {
	frame := m.settings.dialog.View(uisettings.ViewContext{
		Session:    m.services.keys,
		List:       m.services.listKeys,
		Version:    m.update.version,
		Latest:     m.update.latest,
		Applying:   m.update.applying,
		Height:     m.layout.height,
		InnerWidth: cardInnerWidth(m.cardWidth()),
		Styles:     settingsStyles(),
		TextInput:  textInputView,
		TextArea:   textAreaView,
	})
	switch frame.Card {
	case uisettings.Confirm:
		return m.confirmCard(frame.Title, frame.Question, frame.Consequence, frame.Destructive, frame.Answer)
	case uisettings.Fixed:
		return m.card(frame.Title, frame.Body, frame.Hints)
	}
	d := &m.settings.dialog
	lines := strings.Split(frame.Body, "\n")
	save := keyStyle().Render("[ Save and close ]")
	width := m.flexCardWidth(frame.Title, save+"\n"+frame.Body, frame.Hints)
	hintRows := strings.Count(legendInline(frame.Hints, cardInnerWidth(width)), "\n") + 1
	room := max(m.layout.height-5-hintRows-1, 1)
	if m.errBar.text != "" {
		room = max(room-2, 1)
	}
	first := 0
	if len(lines) > room {
		first = min(max(d.Field-room+1, 0), len(lines)-room)
		lines = lines[first : first+room]
	}
	d.DocsRow = -1
	if uisettings.FieldDocs >= first && uisettings.FieldDocs < first+len(lines) {
		d.DocsRow = uisettings.FieldDocs - first + 1
	}
	d.PromptsRow = -1
	if uisettings.FieldPrompts >= first && uisettings.FieldPrompts < first+len(lines) {
		d.PromptsRow = uisettings.FieldPrompts - first + 1
	}
	d.GlyphRow = -1
	if uisettings.FieldGlyphs >= first && uisettings.FieldGlyphs < first+len(lines) {
		d.GlyphRow = uisettings.FieldGlyphs - first + 1
		d.GlyphRowWidth = ansi.StringWidth(lines[uisettings.FieldGlyphs-first])
	}
	d.SaveRow, d.SaveRowWidth = 0, ansi.StringWidth(save)
	return m.cardFlex(frame.Title, save+"\n"+strings.Join(lines, "\n"), frame.Hints)
}

func settingsStyles() uisettings.Styles {
	return uisettings.Styles{
		Value:      valueStyle(),
		Subtle:     subtleStyle(),
		Muted:      mutedStyle(),
		Key:        keyStyle(),
		Annotation: annotationStyle(),
		Marker:     lipgloss.NewStyle().Foreground(colorAccent()),
		Focused:    lipgloss.NewStyle().Foreground(colorAccent()).Bold(true),
		Action:     lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true),
		ActionText: lipgloss.NewStyle().Foreground(colorAccent2()),
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
func (s *settingsFeature) applyEditorsProbe(msg editorsProbedMsg, open bool) {
	s.cache.editors = &msg
	if open && msg.Generation == s.gen {
		s.dialog.Editor.ApplyProbe(msg)
	}
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

func (m *Model) routeSettingsMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case settingsLoadedMsg:
		return routed(m.handleSettingsLoaded(msg))

	case baseFetchedMsg:
		return routed(m, m.recordBaseFetch(msg))

	case editorsProbedMsg:
		m.settings.applyEditorsProbe(msg, m.mode == modeSettings)
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
