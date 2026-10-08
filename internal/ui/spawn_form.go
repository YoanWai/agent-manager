package ui

import (
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	fieldName = iota
	fieldTool
	fieldProfile
	fieldModel
	fieldEffort
	fieldDir
	fieldWorktree
	fieldPrompt
	fieldGroup
	// fieldBase labels the read-only base line, which never takes focus.
	fieldBase
)

type groupOption struct {
	path   string
	depth  int
	sessID string
	name   string
}

type form struct {
	name textinput.Model
	dir  textinput.Model
	// prompt is a composer rather than a plain textarea: a first task is
	// often a screenshot, so the box has to hold pasted images the way the
	// quick prompt does.
	prompt       composer
	dirAuto      bool
	toolNames    []string
	toolIndex    int
	groups       []groupOption
	groupIndex   int
	worktree     bool
	worktreeAuto bool
	// defaultsTouched protects an explicit tool or worktree choice from an
	// external settings refresh that began when the form opened.
	defaultsTouched bool
	focus           int
	choice          choice
	// hits maps each painted body line to what a click there does.
	hits []formHit
}

// formDialog is the New Session form: its fields, keys, view, validation
// and the spawn request enter builds. The root opens it, resolves its
// defaults and runs what its formRequest asks for.
type formDialog struct{ form }

// formHost is what the form reads from the root: the choice facts, the
// status bar, the path completer under its dir field, and the launch
// inputs of a spawn into a group.
type formHost interface {
	choiceHost
	clearErr()
	pathSuggestions() *pathComplete
	spawnDefaults(group string) spawnDefaults
}

// formRequest is root work a form key or click asks for once the form has
// taken its own share. At most one is set.
type formRequest struct {
	close     bool
	applyPath bool
	// group steps the group picker, which the group form and the move
	// dialog share.
	group int
	// toggle flips the worktree choice once the directory is known to host
	// one; probe only asks.
	toggle bool
	probe  bool
	// catalog names the CLI the form moved to, whose choice rows are new.
	catalog string
	spawn   *spawnRequest
}

// resolveExistingDir turns raw field input into a usable directory:
// expand ~, fall back when empty, absolutize, and require it to exist.
// The resolved value returns either way so error messages can show it.
func resolveExistingDir(raw, fallback string) (string, bool) {
	dir := expandHome(strings.TrimSpace(raw))
	if dir == "" {
		dir = fallback
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	return dir, isDir(dir)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func textField(placeholder string, limit int) textinput.Model {
	in := textinput.New()
	in.Placeholder = placeholder
	in.CharLimit = limit
	return in
}

const (
	// formLabelColumn is the columns before a field value: marker (2),
	// label (9), separator space (1).
	formLabelColumn   = 12
	formPromptMaxRows = 4
)

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

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return cardInnerWidth(m.cardWidth()) - formLabelColumn
}

func (m *Model) syncFormFieldWidths() {
	m.form.syncWidths(m.formValueWidth())
}

// syncWidths fits the field widgets to the card so long values scroll
// (inputs) or wrap (prompt) instead of clipping at the card edge. Inputs
// reserve 3 columns: their "> " prompt plus the cursor cell that renders
// past the last character.
func (d *formDialog) syncWidths(inner int) {
	d.name.Width = inner - 3
	d.dir.Width = inner - 3
	// textinput recomputes its scroll window only inside Update/SetValue/
	// SetCursor, so a width change alone would render a stale window until
	// the next keystroke.
	d.name.SetCursor(d.name.Position())
	d.dir.SetCursor(d.dir.Position())
	d.prompt.input.SetWidth(inner)
	d.choice.filter.Width = inner - 3
	d.choice.filter.SetCursor(d.choice.filter.Position())
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

	name := textField("my-session", 60)
	name.Focus()

	dir := textField("", 400)
	prompt := promptField()
	prompt.gen = m.nextComposerGen()

	m.form = formDialog{form{
		name:      name,
		dir:       dir,
		prompt:    prompt,
		dirAuto:   true,
		toolNames: tools,
		toolIndex: toolIndex,
		focus:     fieldName,
		choice:    newChoice(m, tools[toolIndex]),
	}}
	m.clearErr()
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	m.rebuildGroupOptions(m.contextGroup())
	m.form.dir.SetValue(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	m.form.worktree = m.cachedSpawnWorktreeDefault(m.selectedGroupPath())
	m.form.worktreeAuto = true
	m.pathSugg.reset()
	m.mode = modeForm
	catalog := m.ensureCatalog(tools[toolIndex])
	if m.settings.pending > 0 {
		return tea.Batch(m.formWorktreeProbeCmd(false), catalog)
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadForm, generation: uint64(m.form.prompt.gen), extra: m.choiceSettingKeys()}, reader), catalog)
}

// applyCachedFormDefaults takes the loaded tool and worktree defaults. A
// tool the load moved to starts its choice rows over and asks its CLI.
func (m *Model) applyCachedFormDefaults() tea.Cmd {
	before := m.form.tool()
	m.form.toolNames, m.form.toolIndex = m.cachedSpawnToolSelection()
	if m.form.worktreeAuto {
		m.form.worktree = m.cachedSpawnWorktreeDefault(m.selectedGroupPath())
	}
	toolName := m.form.tool()
	if toolName == before || toolName == "" {
		return nil
	}
	m.form.choice = newChoice(m, toolName)
	m.syncFormFieldWidths()
	return m.ensureCatalog(toolName)
}

func (m *Model) formTool() string { return m.form.tool() }

func (d *formDialog) tool() string {
	if len(d.toolNames) == 0 {
		return ""
	}
	return d.toolNames[d.toolIndex]
}

func (m *Model) selectedGroupPath() string { return m.form.groupPath() }

// groupPath is the group the picker highlights. The group form and the
// move dialog pick from the same list.
func (d *formDialog) groupPath() string {
	if d.groupIndex >= 0 && d.groupIndex < len(d.groups) {
		return d.groups[d.groupIndex].path
	}
	return ""
}

// rebuildGroupOptions flattens the group tree into picker rows.
// Index 0 is always the root; selectPath moves the highlight when given.
func (m *Model) rebuildGroupOptions(selectPath string) {
	paths := groupClosure(m.workspace.groups, m.workspace.sessions)
	for path := range paths {
		if m.groupEffectivelyArchived(path) {
			delete(paths, path)
		}
	}
	children := childIndex(paths, m.workspace.groups)

	options := []groupOption{{path: "", depth: 0}}
	var walk func(path string, depth int)
	walk = func(path string, depth int) {
		options = append(options, groupOption{path: path, depth: depth})
		for _, child := range children[path] {
			walk(child, depth+1)
		}
	}
	for _, root := range children[""] {
		walk(root, 1)
	}

	m.form.groups = options
	m.form.groupIndex = 0
	for i, opt := range options {
		if selectPath != "" && opt.path == selectPath {
			m.form.groupIndex = i
			return
		}
	}
}

func (m *Model) handleFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.form.handleKey(m, msg)
	return m, tea.Batch(cmd, m.runFormRequest(request, systemDirectoryPreflight{git: m.services.gitDrv}))
}

// runFormRequest executes what a form key or click asked of the root.
func (m *Model) runFormRequest(request formRequest, reader directoryPreflight) tea.Cmd {
	switch {
	case request.close:
		m.mode = modeList
	case request.applyPath:
		return m.applyPathSuggestion()
	case request.group != 0:
		return m.moveGroupCursor(request.group)
	case request.toggle:
		return m.toggleFormWorktree()
	case request.probe:
		return m.formWorktreeProbeCmd(false)
	case request.catalog != "":
		m.syncFormFieldWidths()
		return m.ensureCatalog(request.catalog)
	case request.spawn != nil:
		spawn := *request.spawn
		spawn.dirReader = reader
		m.clearErr()
		m.dispatchSpawn(spawn)
		return m.nextEffectCmd()
	}
	return nil
}

func (d *formDialog) handleKey(h formHost, msg tea.KeyMsg) (tea.Cmd, formRequest) {
	msg = typedText(msg)
	if d.focus == fieldModel {
		if cmd, handled := d.handleModelKey(h, msg); handled {
			return cmd, formRequest{}
		}
	}
	paths := h.pathSuggestions()
	dirSuggesting := d.focus == fieldDir && paths.active()
	promptFocused := d.focus == fieldPrompt
	switch msg.String() {
	case "esc":
		if dirSuggesting {
			paths.reset()
			return nil, formRequest{}
		}
		// The form is gone, and with it the only text naming the images it
		// was holding.
		d.prompt.release()
		return nil, formRequest{close: true}
	case "tab":
		if dirSuggesting {
			return nil, formRequest{applyPath: true}
		}
		d.focusStep(h, 1)
		return nil, formRequest{}
	case "shift+tab":
		d.focusStep(h, -1)
		return nil, formRequest{}
	case "up":
		if promptFocused {
			if cmd, stepped := d.prompt.stepRow(msg); stepped {
				return cmd, formRequest{}
			}
		}
		if dirSuggesting {
			if !paths.move(-1) {
				d.focusStep(h, -1)
			}
		} else {
			d.focusStep(h, -1)
		}
		return nil, formRequest{}
	case "down":
		if promptFocused {
			if cmd, stepped := d.prompt.stepRow(msg); stepped {
				return cmd, formRequest{}
			}
		}
		if dirSuggesting {
			if !paths.move(1) {
				d.focusStep(h, 1)
			}
		} else {
			d.focusStep(h, 1)
		}
		return nil, formRequest{}
	case "left":
		if d.focus == fieldTool {
			return nil, d.cycleTool(h, -1)
		}
		if d.stepChoice(h, -1) {
			return nil, formRequest{}
		}
		if d.focus == fieldWorktree {
			return nil, formRequest{toggle: true}
		}
		if d.focus == fieldGroup {
			return nil, formRequest{group: -1}
		}
	case "right":
		if d.focus == fieldTool {
			return nil, d.cycleTool(h, 1)
		}
		if d.stepChoice(h, 1) {
			return nil, formRequest{}
		}
		if d.focus == fieldWorktree {
			return nil, formRequest{toggle: true}
		}
		if d.focus == fieldGroup {
			return nil, formRequest{group: 1}
		}
	case "enter":
		if dirSuggesting && paths.chosen {
			return nil, formRequest{applyPath: true}
		}
		return nil, d.submit(h)
	}

	if promptFocused {
		if cmd, handled := d.prompt.handleChipKey(h, composerForm, msg); handled {
			return cmd, formRequest{}
		}
	}

	var cmd tea.Cmd
	switch d.focus {
	case fieldName:
		d.name, cmd = d.name.Update(msg)
	case fieldDir:
		d.dir, cmd = d.dir.Update(msg)
		d.dirAuto = false
		return tea.Batch(cmd, paths.request(pathSuggestionForm, d.dir.Value(), systemPathSuggestionReader{})), formRequest{probe: true}
	case fieldPrompt:
		cmd = d.prompt.typeKey(msg)
	case fieldEffort:
		if d.choice.effortTyped(h, d.tool()) {
			before := d.choice.typedEffort.Value()
			d.choice.typedEffort, cmd = d.choice.typedEffort.Update(msg)
			if d.choice.typedEffort.Value() != before {
				d.choice.keep(h, d.tool())
			}
		}
	}
	return cmd, formRequest{}
}

// handleModelKey lets the keys the list does not take fall through.
func (d *formDialog) handleModelKey(h formHost, msg tea.KeyMsg) (tea.Cmd, bool) {
	toolName, ch := d.tool(), &d.choice
	list := ch.suggestions(h, toolName, ch.query())
	open := ch.sugg.open && len(list) > 0
	pick := func() { ch.pickModel(h, toolName, list[ch.sugg.index].model.Key()) }
	var cmd tea.Cmd
	switch msg.String() {
	case "esc":
		if !ch.sugg.open {
			return nil, false
		}
		ch.sugg = modelSuggest{}
		return nil, true
	case "tab":
		if !open {
			return nil, false
		}
		pick()
		return nil, true
	case "enter":
		if !open || !ch.sugg.chosen {
			return nil, false
		}
		pick()
		return nil, true
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		if !open || !ch.sugg.move(len(list), delta) {
			d.focusStep(h, delta)
		}
		return nil, true
	case "shift+tab":
		return nil, false
	case "left", "right":
		ch.filter, cmd = ch.filter.Update(msg)
		return cmd, true
	}
	ch.filter, cmd = ch.filter.Update(msg)
	ch.filtering = true
	if strings.TrimSpace(ch.filter.Value()) == "" {
		ch.pickModel(h, toolName, "")
	}
	ch.sugg = modelSuggest{open: true}
	return cmd, true
}

func (m *Model) handleFormClick(x, y int) (tea.Model, tea.Cmd) {
	// The body starts under the card's title row and the blank row after it.
	line := y - m.layout.cardTop - 2
	if line < 0 || line >= len(m.form.hits) || x < m.layout.cardLeft || x >= m.layout.cardRight {
		return m, nil
	}
	return m, m.runFormRequest(m.form.click(m, line), nil)
}

// click acts on the painted body line a click landed on.
func (d *formDialog) click(h formHost, line int) formRequest {
	hit := d.hits[line]
	toolName, ch := d.tool(), &d.choice
	switch {
	case hit.field == fieldModel && hit.entry >= 0:
		if list := ch.suggestions(h, toolName, ch.query()); hit.entry < len(list) {
			ch.pickModel(h, toolName, list[hit.entry].model.Key())
		}
		return formRequest{}
	case hit.field == fieldGroup && hit.entry >= 0:
		return formRequest{group: hit.entry - d.groupIndex}
	case hit.field != d.focus:
		if slices.Contains(d.fields(h), hit.field) {
			d.focusField(h, hit.field)
		}
		return formRequest{}
	}
	switch hit.field {
	case fieldTool:
		return d.cycleTool(h, 1)
	case fieldWorktree:
		return formRequest{toggle: true}
	default:
		d.stepChoice(h, 1)
	}
	return formRequest{}
}

func (d *formDialog) stepChoice(h choiceHost, delta int) bool {
	toolName, ch := d.tool(), &d.choice
	switch {
	case d.focus == fieldProfile:
		ch.cycleProfile(h, toolName, delta)
	case d.focus == fieldEffort && !ch.effortTyped(h, toolName):
		ch.cycleEffort(h, toolName, delta)
	default:
		return false
	}
	return true
}

// fields holds a choice row only where the CLI has something to pick.
func (d *formDialog) fields(h choiceHost) []int {
	toolName, ch := d.tool(), &d.choice
	fields := []int{fieldName, fieldTool}
	if _, shown := ch.profileRow(h, toolName); shown {
		fields = append(fields, fieldProfile)
	}
	if _, listed := modelRowNote(h, toolName); listed {
		fields = append(fields, fieldModel)
	}
	if _, shown, active := ch.effortRow(h, toolName); shown && active {
		fields = append(fields, fieldEffort)
	}
	return append(fields, fieldDir, fieldWorktree, fieldPrompt, fieldGroup)
}

// moveGroupCursor moves within the expanded group picker, wrapping at the
// ends; a delta of 0 re-resolves the dependent defaults in place.
func (m *Model) moveGroupCursor(delta int) tea.Cmd {
	count := len(m.form.groups)
	if count == 0 {
		return nil
	}
	m.form.groupIndex = (m.form.groupIndex + delta + count) % count
	if m.mode == modeForm && m.form.dirAuto {
		m.form.dir.SetValue(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	}
	if m.mode == modeForm && m.form.worktreeAuto {
		m.form.worktree = m.cachedSpawnWorktreeDefault(m.selectedGroupPath())
	}
	if m.mode == modeGroupForm && m.groupForm.pathAuto {
		m.groupForm.path.SetValue(m.capturedAncestorGroupDir(m.selectedGroupPath()))
	}
	if m.mode == modeForm {
		return m.formWorktreeProbeCmd(false)
	}
	return nil
}

func (d *formDialog) focusStep(h formHost, delta int) {
	fields := d.fields(h)
	at := max(slices.Index(fields, d.focus), 0)
	d.focusField(h, fields[(at+delta+len(fields))%len(fields)])
}

func (d *formDialog) focusField(h formHost, field int) {
	h.pathSuggestions().reset()
	d.focus = field
	d.name.Blur()
	d.dir.Blur()
	d.prompt.input.Blur()
	d.choice.filter.Blur()
	d.choice.typedEffort.Blur()
	d.choice.sugg = modelSuggest{}
	switch field {
	case fieldName:
		d.name.Focus()
	case fieldModel:
		d.choice.filter.Focus()
		d.choice.openModelList(h, d.tool())
	case fieldEffort:
		d.choice.typedEffort.Focus()
	case fieldDir:
		d.dir.Focus()
	case fieldPrompt:
		d.prompt.input.Focus()
	}
}

// cycleTool starts the choice rows over; the root fits them to the card and
// asks the new CLI what it offers.
func (d *formDialog) cycleTool(h choiceHost, delta int) formRequest {
	if len(d.toolNames) == 0 {
		return formRequest{}
	}
	d.toolIndex = (d.toolIndex + delta + len(d.toolNames)) % len(d.toolNames)
	d.defaultsTouched = true
	toolName := d.tool()
	d.choice = newChoice(h, toolName)
	return formRequest{catalog: toolName}
}

// formSpawnDir is the directory the form would launch in, resolved the
// same way submit resolves it.
func (m *Model) formSpawnDir() string {
	return m.capturedAbsolutePath(m.form.dir.Value(), m.env.workDir)
}

// formWorktreeOn is the worktree state the form shows and spawns with: the
// toggle, unless the chosen directory cannot host a worktree.
func (m *Model) formWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.formSpawnDir())
	return m.form.worktree && known && capable
}

// toggleFormWorktree flips the toggle, or explains why the chosen
// directory rules a worktree out.
func (m *Model) toggleFormWorktree() tea.Cmd {
	dir := m.formSpawnDir()
	capable, known := m.cachedWorktreeCapability(dir)
	if !known {
		return m.formWorktreeProbeCmd(true)
	}
	if !capable {
		m.reportErr("worktree sessions need a git repository: " + dir + " is not one")
		return nil
	}
	m.clearErr()
	m.form.setWorktree(!m.form.worktree)
	return nil
}

// setWorktree is an explicit worktree choice, which later defaults leave be.
func (d *formDialog) setWorktree(on bool) {
	d.worktree = on
	d.worktreeAuto = false
	d.defaultsTouched = true
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	return m.submitFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	return m, m.runFormRequest(m.form.submit(m), reader)
}

// submit validates the form and builds its spawn; the root checks the
// directory on the effect lane.
func (d *formDialog) submit(h formHost) formRequest {
	if len(d.toolNames) == 0 {
		h.reportErr("no tools configured")
		return formRequest{close: true}
	}
	toolName := d.toolNames[d.toolIndex]
	if d.prompt.pasting() {
		h.reportErr("still reading the pasted image - try again in a moment")
		return formRequest{}
	}

	name := strings.TrimSpace(d.name.Value())
	autoNamed := name == ""
	if autoNamed {
		name = toolName + "-" + newID()[:4]
	}
	group := d.groupPath()
	// Chips become the paths of the images they stand for, so a first task
	// reaches the agent with its screenshot named where it was pasted.
	prompt := d.prompt.message()
	if strings.HasPrefix(prompt, "-") {
		h.reportErr(`prompt cannot start with "-": the tool would read it as a flag`)
		return formRequest{}
	}
	picked, err := d.choice.launch(h, toolName, d.choice.filter.Value())
	if err != nil {
		h.reportErr(err.Error())
		return formRequest{}
	}

	defaults := h.spawnDefaults(group)
	request := spawnRequest{
		kind:         spawnForm,
		toolName:     toolName,
		name:         name,
		group:        group,
		prompt:       prompt,
		autoNamed:    autoNamed,
		pickWorktree: d.worktree,
		choice:       picked,
		base:         defaults.base,
		pane:         defaults.pane,
		composerGen:  d.prompt.gen,
		images:       d.prompt.attachments,
		draft:        d.prompt.input.Value(),
		draftName:    d.name.Value(),
		draftDir:     d.dir.Value(),
		rawDir:       d.dir.Value(),
		dirFallbacks: defaults.fallbacks,
		wantWorktree: d.worktree,
	}
	return formRequest{spawn: &request}
}

func (m *Model) rememberSpawnPick(tool string, worktree bool) {
	m.ledger.lastSpawnTool = tool
	m.ledger.lastSpawnWorktree = worktree
}

func (m *Model) buildLaunch(toolName string, tool config.Tool, baseCommand, id string) (string, map[string]string, error) {
	return launch.Environment(m.services.hooks, toolName, tool, baseCommand, id)
}

func (m *Model) routeSpawnMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case worktreeProbeMsg:
		return routed(m.handleWorktreeProbe(msg))

	case pathSuggestionsMsg:
		m.pathSugg.handle(m, msg)
		return routed(m, nil)

	case terminalDirectoryMsg:
		return routed(m.handleTerminalDirectory(msg))
	}
	return nil, nil, false
}
