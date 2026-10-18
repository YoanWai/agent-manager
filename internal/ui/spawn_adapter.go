package ui

import (
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// formDialog is the New Session form (internal/ui/spawn) with what the
// root holds for it: the prompt box, the path completer under its dir
// field, and the group picker the group form and move dialog share.
type formDialog struct {
	spawn.Form
	// prompt is a composer rather than a plain textarea: a first task is
	// often a screenshot, so the box has to hold pasted images the way the
	// quick prompt does.
	prompt     composer
	paths      pathComplete
	groups     []groupOption
	groupIndex int
}

// spawnHost is the root as the New Session form and its choice see it.
type spawnHost struct{ m *Model }

func (m *Model) spawnHost() spawnHost { return spawnHost{m: m} }

func (h spawnHost) ChoiceCaps(toolName string) spawn.Caps {
	tool := h.m.services.cfg.Tools[toolName]
	return spawn.Caps{
		Catalog:  tool.Catalog != "",
		Profiles: tool.ProfileArgs != "",
		Models:   tool.ModelArgs != "",
		Efforts:  tool.EffortArgs != "",
	}
}

// ChoiceAnswer hands the choice a copy, so only the root's catalog
// handlers change what the CLI answered.
func (h spawnHost) ChoiceAnswer(toolName string) *spawn.Answer {
	state := h.m.ledger.catalogs[toolName]
	if state == nil {
		return nil
	}
	return &spawn.Answer{Cat: state.cat, Err: state.err, Loaded: state.loaded, Loading: state.loading}
}

func (h spawnHost) ChoiceSetting(key string) string { return h.m.settings.cache.value(key) }

func (h spawnHost) SaveChoiceSetting(key, value string) { h.m.saveChoiceSetting(key, value) }

func (h spawnHost) ReportErr(text string) { h.m.reportErr(text) }

func (h spawnHost) ClearErr() { h.m.clearErr() }

func (h spawnHost) Prompt() spawn.Prompt {
	return promptBox{c: &h.m.form.prompt, target: composerForm, host: h.m}
}

func (h spawnHost) Paths() spawn.Paths {
	return pathField{pc: &h.m.form.paths, target: pathSuggestionForm}
}

// promptBox lends a feature package one of the root's composers.
type promptBox struct {
	c      *composer
	target composerID
	host   composerHost
}

func (p promptBox) Focus() { p.c.input.Focus() }

func (p promptBox) Blur() { p.c.input.Blur() }

func (p promptBox) Fit(width, rows int) {
	p.c.input.SetWidth(width)
	p.c.maxRows = rows
}

func (p promptBox) View() string { return p.c.view() }

func (p promptBox) StepRow(msg tea.KeyMsg) (tea.Cmd, bool) { return p.c.stepRow(msg) }

func (p promptBox) ChipKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	return p.c.handleChipKey(p.host, p.target, msg)
}

func (p promptBox) Type(msg tea.KeyMsg) tea.Cmd { return p.c.typeKey(msg) }

func (p promptBox) Release() { p.c.release() }

func (p promptBox) Pasting() bool { return p.c.pasting() }

func (p promptBox) Message() string { return p.c.message() }

// pathField lends the form the root's path completer.
type pathField struct {
	pc     *pathComplete
	target pathSuggestionTarget
}

func (p pathField) Active() bool { return p.pc.active() }

func (p pathField) Chosen() bool { return p.pc.chosen }

func (p pathField) Move(delta int) bool { return p.pc.move(delta) }

func (p pathField) Reset() { p.pc.reset() }

func (p pathField) Suggest(typed string) tea.Cmd {
	return p.pc.request(p.target, typed, systemPathSuggestionReader{})
}

func (p pathField) View() string { return p.pc.view() }

func (p pathField) Hint() [][2]string { return pathSuggestHint(p.pc.chosen) }

// spawnStyles copies the active theme for one paint.
func spawnStyles() spawn.Styles {
	return spawn.Styles{
		Subtle:      subtleStyle(),
		Value:       valueStyle(),
		Muted:       mutedStyle(),
		Warn:        warnStyle(),
		Label:       labelStyle(),
		SubtleColor: colorSubtle(),
		Accent:      colorAccent(),
		Accent2:     colorAccent2(),
		Input:       textInputView,
	}
}

func textField(placeholder string, limit int) textinput.Model {
	return spawn.TextField(placeholder, limit)
}

const formPromptMaxRows = 4

func promptField() composer {
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
	holdOpen(&in)
	return composer{input: in, maxRows: formPromptMaxRows}
}

func formField(label, value string, focused bool) string {
	return spawn.FormField(spawnStyles(), label, value, focused)
}

func groupBadge(path string) string {
	return lipgloss.NewStyle().Foreground(colorAccent2()).Render(path)
}

func pathSuggestHint(chosen bool) [][2]string {
	if chosen {
		return [][2]string{{"↑↓", "pick"}, {"↵/tab", "complete"}, {"esc", "close"}}
	}
	return [][2]string{{"↑↓", "pick"}, {"tab", "complete"}, {"↵", "create"}, {"esc", "close"}}
}

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return cardInnerWidth(m.cardWidth()) - spawn.LabelColumn
}

// syncFormFieldWidths fits the fields to the card so long values scroll
// (inputs) or wrap (prompt) instead of clipping at the card edge.
func (m *Model) syncFormFieldWidths() {
	inner := m.formValueWidth()
	m.form.SyncWidths(inner)
	m.form.prompt.input.SetWidth(inner)
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
	return m.openFormWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openFormWithReader(reader settingsValueReader) tea.Cmd {
	tools, toolIndex := m.cachedSpawnToolSelection()
	if len(tools) == 0 {
		m.reportErr("no CLIs enabled: open settings (s), then CLIs, to turn some on")
		return nil
	}
	prompt := promptField()
	prompt.gen = m.nextComposerGen()
	m.form = formDialog{
		Form:   spawn.NewForm(m.spawnHost(), tools, toolIndex),
		prompt: prompt,
		paths:  m.form.paths.fresh(),
	}
	m.clearErr()
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	m.rebuildGroupOptions(m.contextGroup())
	m.form.SetDir(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	m.form.SeedWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	m.mode = modeForm
	catalog := m.ensureCatalog(tools[toolIndex])
	saved := savedPromptsLoadCmd(m.services.store, savedForForm, uint64(m.form.prompt.gen), 0)
	if m.settings.pending > 0 {
		return tea.Batch(m.formWorktreeProbeCmd(false), catalog, saved)
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadForm, generation: uint64(m.form.prompt.gen), extra: m.choiceSettingKeys()}, reader), catalog, saved)
}

// applyCachedFormDefaults takes the loaded tool and worktree defaults. A
// tool the load moved to starts its choice rows over and asks its CLI.
func (m *Model) applyCachedFormDefaults() tea.Cmd {
	before := m.form.Tool()
	m.form.SetTools(m.cachedSpawnToolSelection())
	m.form.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	toolName := m.form.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	m.form.RestartChoice(m.spawnHost())
	m.syncFormFieldWidths()
	return m.ensureCatalog(toolName)
}

func (m *Model) formTool() string { return m.form.Tool() }

func (m *Model) selectedGroupPath() string { return m.form.groupPath() }

// groupPath is the group the picker highlights. The group form and the
// move dialog pick from the same list.
func (d *formDialog) groupPath() string {
	if d.groupIndex >= 0 && d.groupIndex < len(d.groups) {
		return d.groups[d.groupIndex].path
	}
	return ""
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.form.HandleKey(m.spawnHost(), typedText(msg))
	return m, tea.Batch(cmd, m.runFormRequest(request, systemDirectoryPreflight{git: m.services.gitDrv}))
}

// runFormRequest executes what a form key or click asked of the root.
func (m *Model) runFormRequest(request spawn.FormRequest, reader directoryPreflight) tea.Cmd {
	switch {
	case request.Close:
		m.mode = modeList
	case request.ApplyPath:
		return m.applyPathSuggestion()
	case request.Group != 0:
		return m.moveGroupCursor(request.Group)
	case request.PickGroup:
		if delta := request.GroupEntry - m.form.groupIndex; delta != 0 {
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

func (m *Model) handleFormClick(x, y int) (tea.Model, tea.Cmd) {
	// The body starts under the card's title row and the blank row after it.
	line := y - m.layout.cardTop - 2
	if x < m.layout.cardLeft || x >= m.layout.cardRight {
		return m, nil
	}
	return m, m.runFormRequest(m.form.Click(m.spawnHost(), line), nil)
}

// moveGroupCursor moves within the expanded group picker, wrapping at the
// ends; a delta of 0 re-resolves the dependent defaults in place.
func (m *Model) moveGroupCursor(delta int) tea.Cmd {
	count := len(m.form.groups)
	if count == 0 {
		return nil
	}
	m.form.groupIndex = (m.form.groupIndex + delta + count) % count
	if m.mode == modeForm && m.form.DirAuto() {
		m.form.SetDir(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	}
	if m.mode == modeForm {
		m.form.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.selectedGroupPath()))
	}
	if m.mode == modeGroupForm && m.groupForm.DirAuto() {
		m.groupForm.SetDir(m.capturedAncestorGroupDir(m.selectedGroupPath()))
	}
	if m.mode == modeForm {
		return m.formWorktreeProbeCmd(false)
	}
	return nil
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	return m.submitFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form.Submit(m.spawnHost()), reader)
}

func (m *Model) viewForm() string {
	facts := spawn.FormFacts{
		Width:  m.formValueWidth(),
		NoRepo: worktreeUnavailable,
		Group:  displayGroup(m.form.groups[m.form.groupIndex].path),
		Styles: spawnStyles(),
		Height: m.layout.height,
		Chrome: m.cardChromeRows,
	}
	if m.form.Focused() == spawn.FieldGroup {
		facts.GroupPicker = strings.Split(m.form.viewGroupPicker(), "\n")
	}
	facts.WorktreeCapable, facts.WorktreeKnown = m.cachedWorktreeCapability(m.formSpawnDir())
	if m.formWorktreeOn() {
		facts.Base = m.spawnBaseLabel(m.formSpawnDir(), m.selectedGroupPath())
	}
	body, hint := m.form.View(m.spawnHost(), facts)
	return m.card("◆ New Session", body, hint)
}

func (m *Model) rememberSpawnPick(tool string, worktree bool) {
	m.ledger.lastSpawnTool = tool
	m.ledger.lastSpawnWorktree = worktree
}

func (m *Model) buildLaunch(toolName string, tool config.Tool, baseCommand, id, cwd string, launchedAt time.Time) (string, map[string]string, error) {
	return launch.Environment(m.services.hooks, toolName, tool, baseCommand, id, cwd, launchedAt)
}

func (m *Model) routeSpawnMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case worktreeProbeMsg:
		return routed(m.handleWorktreeProbe(msg))

	case pathSuggestionsMsg:
		m.completer(msg.request.target).handle(m, msg)
		return routed(m, nil)

	case terminalDirectoryMsg:
		return routed(m.handleTerminalDirectory(msg))
	}
	return nil, nil, false
}
