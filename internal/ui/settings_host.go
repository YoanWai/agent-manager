package ui

import (
	"github.com/YoanWai/agent-manager/internal/keybind"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

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

// settingsEditorProbe is the editor row's off-path probe, captured from the
// root's environment when the dialog opens.
func settingsEditorProbe(m *Model) tea.Cmd {
	resolution, generation := detectedEditors(), m.settings.Gen
	probe := uisettings.EditorProbe{
		GUI:    resolution.gui,
		Visual: resolution.visual,
		Editor: resolution.editor,
		Lookup: resolution.lookup,
		Line:   resolution.editorLine,
	}
	return func() tea.Msg {
		msg := probe.Resolve()
		msg.Generation = generation
		return msg
	}
}
