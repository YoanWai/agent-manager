package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/pathcomplete"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// spawnHost is the root as the New Session form and its choice see it.
type spawnHost struct{ m *Model }

func (m *Model) spawnHost() spawnHost { return spawnHost{m: m} }

func (h spawnHost) ChoiceCaps(toolName string) spawn.Caps {
	tool := h.m.services.cfg.Tools[toolName]
	return spawn.Caps{
		Catalog:  tool.Catalog != "",
		Profiles: tool.ProfileArgs != "",
		Models:   tool.ModelArgs != "" || tool.ModelEnv != "",
		Efforts:  tool.EffortArgs != "",
	}
}

// ChoiceAnswer hands the choice a copy, so only the root's catalog
// handlers change what the CLI answered.
func (h spawnHost) ChoiceAnswer(toolName string) *spawn.Answer {
	answer := h.m.ledger.spawner.Answer(toolName)
	if answer == nil {
		return nil
	}
	return &spawn.Answer{Cat: answer.Cat, Err: answer.Err, Loaded: answer.Loaded, Loading: answer.Loading}
}

func (h spawnHost) ChoiceSetting(key string) string { return h.m.settings.Cache.Value(key) }

func (h spawnHost) SaveChoiceSetting(key, value string) { h.m.saveChoiceSetting(key, value) }

func (h spawnHost) ReportErr(text string) { h.m.reportErr(text) }

func (h spawnHost) ChoiceRemoteHost() string {
	m := h.m
	if f := m.form(); f != nil {
		return f.Remote.Host
	}
	if m.quick() != nil {
		if target, ok := m.remoteTarget(); ok {
			return target.host
		}
	}
	return ""
}

func (h spawnHost) ClearErr() { h.m.clearErr() }

func (h spawnHost) Prompt() spawn.Prompt {
	return spawnedPrompt(h.m.form(), composer.Form, h.m)
}

func (h spawnHost) Paths() spawn.Paths {
	return h.m.spawnedPaths(h.m.form())
}

// promptBox lends a feature package one of the root's prompt boxes.
type promptBox struct {
	c      *composer.Composer
	target composer.ID
	host   composer.Host
}

func (p promptBox) Focus() { p.c.Input.Focus() }

func (p promptBox) Blur() { p.c.Input.Blur() }

func (p promptBox) Fit(width, rows int) {
	p.c.Input.SetWidth(width)
	p.c.MaxRows = rows
}

func (p promptBox) View() string { return p.c.View(composerPaint()) }

func (p promptBox) StepRow(msg tea.KeyMsg) (tea.Cmd, bool) { return p.c.StepRow(msg) }

func (p promptBox) ChipKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return p.c.ChipKey(p.host, p.target, msg)
}

func (p promptBox) Type(msg tea.KeyMsg) tea.Cmd { return p.c.TypeKey(msg) }

func (p promptBox) Release() { p.c.Release() }

func (p promptBox) Pasting() bool { return p.c.Pasting() }

func (p promptBox) Message() string { return p.c.Message() }

// spawnStyles copies the active theme for one paint.
func (m *Model) spawnStyles() spawn.Styles {
	return spawn.Styles{
		GlyphMode:   m.prefs.glyphMode,
		Subtle:      uitheme.SubtleStyle(),
		Value:       uitheme.ValueStyle(),
		Muted:       uitheme.MutedStyle(),
		Warn:        uitheme.WarnStyle(),
		Label:       uitheme.LabelStyle(),
		SubtleColor: uitheme.ColorSubtle(),
		Accent:      uitheme.ColorAccent(),
		Accent2:     uitheme.ColorAccent2(),
		Input:       uiframe.TextInputView,
	}
}

const formPromptMaxRows = 4

func promptField() composer.Composer {
	in := textarea.New()
	in.CharLimit = 2000
	in.Placeholder = "first task (optional)"
	in.ShowLineNumbers = false
	in.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return "> "
		}
		return "  "
	})
	in.FocusedStyle.CursorLine = lipgloss.NewStyle()
	composer.HoldOpen(&in)
	return composer.Composer{Input: in, MaxRows: formPromptMaxRows}
}

func (m *Model) formField(label, value string, focused bool) string {
	return spawn.FormField(m.spawnStyles(), label, value, focused)
}

func pathSuggestHint(chosen bool) [][2]string {
	if chosen {
		return [][2]string{{"↑↓", "pick"}, {"↵/tab", "complete"}, {"esc", "close"}}
	}
	return [][2]string{{"↑↓", "pick"}, {"tab", "complete"}, {"↵", "create"}, {"esc", "close"}}
}

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return uiframe.CardInnerWidth(m.cardWidth()) - spawn.LabelColumn
}

// syncFormFieldWidths fits the fields to the card so long values scroll
// (inputs) or wrap (prompt) instead of clipping at the card edge.
func (m *Model) syncFormFieldWidths() {
	f := m.form()
	if f == nil {
		return
	}
	inner := m.formValueWidth()
	f.SyncWidths(inner)
	m.formPromptFor().Input.SetWidth(inner)
}

// contextGroup is the group the cursor currently sits in: a highlighted
// group row itself, or the group holding a highlighted session.
func (m *Model) contextGroup() string {
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			return entry.group
		}
		return entry.sess.Group
	}
	return ""
}

func (m *Model) openForm() tea.Cmd {
	return m.openFormWithReader(uipersist.StoreWriter{Store: m.services.store})
}

func (m *Model) openFormWithReader(reader uipersist.Reader) tea.Cmd {
	tools, toolIndex := m.cachedSpawnToolSelection()
	if len(tools) == 0 {
		m.reportErr("no CLIs enabled: open settings (s), then CLIs, to turn some on")
		return nil
	}
	prompt := promptField()
	prompt.Gen = m.nextComposerGen()
	m.closeQuick()
	f := m.newSpawnSurface(&prompt, pathcomplete.State{Generation: uint64(prompt.Gen) << 32}, spawn.NewForm(m.spawnHost(), tools, toolIndex), m.groupPickerFor(m.contextGroup()))
	m.push(f)
	m.clearErr()
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	f.SetDir(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	f.SeedWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	if target, ok := m.remoteTarget(); ok {
		f.AimRemote(spawn.Remote{Host: target.host, Group: target.group})
		f.RestartChoice(m.spawnHost())
	}
	catalog := m.ensureCatalog(tools[toolIndex])
	saved := uipersist.SavedCmd(m.services.store, uipersist.SavedForForm, uint64(prompt.Gen), 0)
	if m.settings.Pending > 0 {
		return tea.Batch(m.formWorktreeProbeCmd(false), catalog, saved)
	}
	return tea.Batch(uipersist.LoadCmd(uipersist.LoadRequest{Target: uipersist.FormTarget, Generation: uint64(prompt.Gen), Extra: m.choiceSettingKeys()}, reader), catalog, saved)
}

// applyCachedFormDefaults takes the loaded tool and worktree defaults. A
// tool the load moved to starts its choice rows over and asks its CLI.
func (m *Model) applyCachedFormDefaults() tea.Cmd {
	f := m.form()
	before := f.Tool()
	f.SetTools(m.cachedSpawnToolSelection())
	f.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	toolName := f.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	f.RestartChoice(m.spawnHost())
	m.syncFormFieldWidths()
	return m.ensureCatalog(toolName)
}

func (m *Model) formTool() string { return m.form().Tool() }

// selectedGroupPath is the group the open picker highlights: the move
// card's, or the New Session form's.
func (m *Model) selectedGroupPath() string {
	if s, ok := surfaceOf[*uidialogs.MoveSurface](m); ok {
		return s.Picker().GroupPath()
	}
	if f := m.form(); f != nil {
		return f.GroupPath()
	}
	return ""
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.form().HandleKey(m.spawnHost(), composer.TypedText(msg))
	return m, tea.Batch(cmd, m.runFormRequest(request, systemDirectoryPreflight{git: m.services.gitDrv}))
}

// runFormRequest executes what a form key or click asked of the root.
func (m *Model) runFormRequest(request spawn.FormRequest, reader directoryPreflight) tea.Cmd {
	if cmd, taken := m.remoteFormRequest(request); taken {
		return cmd
	}
	switch {
	case request.Close:
		m.closeForm()
	case request.ApplyPath:
		return m.applyPathSuggestion()
	case request.PickPath:
		if m.form().Complete.Pick(request.PathEntry) {
			return m.applyPathSuggestion()
		}
	case request.Group != 0:
		return m.moveGroupCursor(request.Group)
	case request.PickGroup:
		if delta := request.GroupEntry - m.form().GroupIndex; delta != 0 {
			return m.moveGroupCursor(delta)
		}
	case request.Toggle:
		return m.toggleFormWorktree()
	case request.Probe:
		return m.formWorktreeProbeCmd(false)
	case request.Catalog != "":
		m.syncFormFieldWidths()
		return m.ensureCatalog(request.Catalog)
	case request.Spawn != nil:
		req := m.formSpawnRequest(*request.Spawn)
		req.dirReader = reader
		m.clearErr()
		m.dispatchSpawn(req)
		return m.nextEffectCmd()
	}
	return nil
}

func (m *Model) handleFormClick(line int) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form().Form.Click(m.spawnHost(), line), nil)
}

func (m *Model) formWheel(step int) tea.Cmd {
	return m.runFormRequest(m.form().Form.Wheel(m.spawnHost(), step), nil)
}

// moveGroupCursor moves within the form's group picker, wrapping at the
// ends; a delta of 0 re-resolves the dependent defaults in place.
func (m *Model) moveGroupCursor(delta int) tea.Cmd {
	f := m.form()
	if !f.Step(delta) {
		return nil
	}
	if f.DirAuto() {
		f.SetDir(m.capturedGroupDefaultDir(f.GroupPath()))
	}
	f.DefaultWorktree(m.cachedSpawnWorktreeDefault(f.GroupPath()))
	return m.formWorktreeProbeCmd(false)
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	return m.submitFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form().Submit(m.spawnHost()), reader)
}

func (m *Model) viewForm() string {
	f := m.form()
	facts := spawn.FormFacts{
		Width:  m.formValueWidth(),
		NoRepo: uilaunch.WorktreeUnavailable,
		Group:  presentation.EscapeControlsInline(uiframe.DisplayGroup(f.GroupPath())),
		Styles: m.spawnStyles(),
		Height: m.layout.height,
		Chrome: m.cardChromeRows,
	}
	if f.Focused() == spawn.FieldGroup {
		facts.GroupPicker = strings.Split(m.viewGroupPicker(&f.Picker), "\n")
	}
	facts.WorktreeCapable, facts.WorktreeKnown = m.cachedWorktreeCapability(m.formSpawnDir())
	if f.Remote.On() {
		facts.WorktreeCapable, facts.WorktreeKnown = true, true
		facts.Host = lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Render(f.Remote.Host)
	} else if m.formWorktreeOn() {
		facts.Base = m.spawnBaseLabel(m.formSpawnDir(), m.selectedGroupPath())
	}
	body, hint := f.Form.View(m.spawnHost(), facts)
	return m.CardSized(m.cardWidth(), m.glyphs().NewSessionGlyph+" New Session", body, hint)
}

func (m *Model) rememberSpawnPick(tool string, worktree bool) {
	m.ledger.spawner.RememberSpawn(tool, worktree)
}

func (m *Model) routeSpawnMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case uilaunch.ProbeMsg:
		return routed(m.handleWorktreeProbe(msg))

	case pathcomplete.Msg:
		m.completer(msg.Request.Target).Handle(pathCompleter{m}, msg)
		return routed(m, nil)

	case terminalDirectoryMsg:
		return routed(m.handleTerminalDirectory(msg))

	case uidialogs.GroupFormSubmitMsg:
		return routed(m, m.handleGroupFormSubmit(msg, systemDirectoryPreflight{git: m.services.gitDrv}))

	case uidialogs.GroupBaseAskMsg:
		return routed(m, m.handleGroupBaseAsk(msg))
	}
	return nil, nil, false
}
