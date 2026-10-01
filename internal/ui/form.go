package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/launch"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	fieldName = iota
	fieldTool
	fieldDir
	fieldWorktree
	fieldPrompt
	fieldGroup
	fieldCount
)

const (
	gfName = iota
	gfParent
	gfPath
	gfWorktree
	gfCount
)

// groupWorktreeOptions are the picker states for a group's spawn-in-worktree
// choice; index 0 stores as "" so the group keeps inheriting.
var groupWorktreeOptions = []string{"inherit", "on", "off"}

func groupWorktreeValue(index int) string {
	switch index {
	case 1:
		return "on"
	case 2:
		return "off"
	}
	return ""
}

func groupWorktreeIndex(value string) int {
	switch value {
	case "on":
		return 1
	case "off":
		return 2
	}
	return 0
}

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
}

type groupForm struct {
	name          textinput.Model
	path          textinput.Model
	pathAuto      bool
	worktreeIndex int
	focus         int
	// gen tells this group form from the one that stood in the same place
	// before it, so a completion cannot close a form the user since reopened.
	gen int
}

// sessionLabel renders a session's identity for the tmux status bar.
func sessionLabel(group, name string) string {
	if group == "" {
		return name
	}
	return group + " · " + name
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
	in.SetHeight(1)
	return composer{input: in, maxRows: formPromptMaxRows}
}

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return cardInnerWidth(m.cardWidth()) - formLabelColumn
}

// syncFormFieldWidths fits the field widgets to the card so long values
// scroll (inputs) or wrap (prompt) instead of clipping at the card edge.
// Inputs reserve 3 columns: their "> " prompt plus the cursor cell that
// renders past the last character.
func (m *Model) syncFormFieldWidths() {
	inner := m.formValueWidth()
	m.form.name.Width = inner - 3
	m.form.dir.Width = inner - 3
	// textinput recomputes its scroll window only inside Update/SetValue/
	// SetCursor, so a width change alone would render a stale window until
	// the next keystroke.
	m.form.name.SetCursor(m.form.name.Position())
	m.form.dir.SetCursor(m.form.dir.Position())
	m.form.prompt.input.SetWidth(inner)
}

func (m *Model) syncGroupFormFieldWidths() {
	width := m.formValueWidth() - 3
	m.groupForm.name.Width = width
	m.groupForm.path.Width = width
	m.groupForm.name.SetCursor(m.groupForm.name.Position())
	m.groupForm.path.SetCursor(m.groupForm.path.Position())
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

// toolDisplayOrder fixes the order tools appear in when creating a session and
// when cycling the quick-spawn tool. Tools outside this list follow, sorted
// alphabetically.
var toolDisplayOrder = []string{"claude", "opencode", "codex", "grok", "gemini", "pi"}

// sortedToolNames is every configured agent CLI in picker order. A block
// declaring shell = true is not a CLI to spawn agents with, so it is left
// out; its own key launches it, and a rename still keeps a shell session
// on it.
func sortedToolNames(cfg config.Config) []string {
	names := make([]string, 0, len(cfg.Tools))
	for _, name := range cfg.ToolNames() {
		if !cfg.Tools[name].Shell {
			names = append(names, name)
		}
	}
	rank := make(map[string]int, len(toolDisplayOrder))
	for i, name := range toolDisplayOrder {
		rank[name] = i
	}
	sort.Slice(names, func(i, j int) bool {
		ri, iRanked := rank[names[i]]
		rj, jRanked := rank[names[j]]
		if iRanked && jRanked {
			return ri < rj
		}
		if iRanked != jRanked {
			return iRanked
		}
		return names[i] < names[j]
	})
	return names
}

func (m *Model) openForm() tea.Cmd {
	return m.openFormWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openFormWithReader(reader settingsValueReader) tea.Cmd {
	tools, toolIndex := m.cachedSpawnToolSelection()
	if len(tools) == 0 {
		m.errBar.text = "no CLIs enabled: open settings (s), then CLIs, to turn some on"
		return nil
	}

	name := textField("my-session", 60)
	name.Focus()

	dir := textField("", 400)
	prompt := promptField()
	prompt.gen = m.nextComposerGen()

	m.form = form{
		name:      name,
		dir:       dir,
		prompt:    prompt,
		dirAuto:   true,
		toolNames: tools,
		toolIndex: toolIndex,
		focus:     fieldName,
	}
	m.errBar.text = ""
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	m.rebuildGroupOptions(m.contextGroup())
	m.form.dir.SetValue(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	m.form.worktree = m.cachedSpawnWorktreeDefault(m.selectedGroupPath())
	m.form.worktreeAuto = true
	m.pathSugg.reset()
	m.mode = modeForm
	if m.settingsPending > 0 {
		return m.formWorktreeProbeCmd(false)
	}
	return settingsLoadCmd(settingsLoadRequest{target: settingsLoadForm, generation: uint64(m.form.prompt.gen)}, reader)
}

func (m *Model) applyCachedFormDefaults() {
	m.form.toolNames, m.form.toolIndex = m.cachedSpawnToolSelection()
	if m.form.worktreeAuto {
		m.form.worktree = m.cachedSpawnWorktreeDefault(m.selectedGroupPath())
	}
}

func (m *Model) selectedGroupPath() string {
	if m.form.groupIndex >= 0 && m.form.groupIndex < len(m.form.groups) {
		return m.form.groups[m.form.groupIndex].path
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
	dirSuggesting := m.form.focus == fieldDir && m.pathSugg.active()
	promptFocused := m.form.focus == fieldPrompt
	switch msg.String() {
	case "esc":
		if dirSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		// The form is gone, and with it the only text naming the images it
		// was holding.
		m.form.prompt.release()
		m.mode = modeList
		return m, nil
	case "tab":
		if dirSuggesting {
			return m, m.applyPathSuggestion()
		}
		m.formFocus(1)
		return m, nil
	case "shift+tab":
		m.formFocus(-1)
		return m, nil
	case "up":
		if promptFocused {
			if cmd, stepped := m.form.prompt.stepRow(msg); stepped {
				return m, cmd
			}
		}
		if dirSuggesting {
			if !m.pathSugg.move(-1) {
				m.formFocus(-1)
			}
		} else {
			m.formFocus(-1)
		}
		return m, nil
	case "down":
		if promptFocused {
			if cmd, stepped := m.form.prompt.stepRow(msg); stepped {
				return m, cmd
			}
		}
		if dirSuggesting {
			if !m.pathSugg.move(1) {
				m.formFocus(1)
			}
		} else {
			m.formFocus(1)
		}
		return m, nil
	case "left":
		if m.form.focus == fieldTool {
			m.cycleTool(-1)
			return m, nil
		}
		if m.form.focus == fieldWorktree {
			return m, m.toggleFormWorktree()
		}
		if m.form.focus == fieldGroup {
			return m, m.moveGroupCursor(-1)
		}
	case "right":
		if m.form.focus == fieldTool {
			m.cycleTool(1)
			return m, nil
		}
		if m.form.focus == fieldWorktree {
			return m, m.toggleFormWorktree()
		}
		if m.form.focus == fieldGroup {
			return m, m.moveGroupCursor(1)
		}
	case "enter":
		if dirSuggesting && m.pathSugg.chosen {
			return m, m.applyPathSuggestion()
		}
		return m.submitForm()
	}

	if promptFocused {
		if cmd, handled := m.composerKey(composerForm, msg); handled {
			return m, cmd
		}
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case fieldName:
		m.form.name, cmd = m.form.name.Update(msg)
	case fieldDir:
		m.form.dir, cmd = m.form.dir.Update(msg)
		m.form.dirAuto = false
		cmd = tea.Batch(cmd, m.requestPathSuggestions(pathSuggestionForm, m.form.dir.Value()), m.formWorktreeProbeCmd(false))
	case fieldPrompt:
		cmd = m.form.prompt.typeKey(msg)
	}
	return m, cmd
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

func (m *Model) formFocus(delta int) {
	m.pathSugg.reset()
	m.form.focus = (m.form.focus + delta + fieldCount) % fieldCount
	m.form.name.Blur()
	m.form.dir.Blur()
	m.form.prompt.input.Blur()
	switch m.form.focus {
	case fieldName:
		m.form.name.Focus()
	case fieldDir:
		m.form.dir.Focus()
	case fieldPrompt:
		m.form.prompt.input.Focus()
	}
}

func (m *Model) cycleTool(delta int) {
	if len(m.form.toolNames) == 0 {
		return
	}
	m.form.toolIndex = (m.form.toolIndex + delta + len(m.form.toolNames)) % len(m.form.toolNames)
	m.form.defaultsTouched = true
}

// formSpawnDir is the directory the form would launch in, resolved the
// same way submit resolves it.
func (m *Model) formSpawnDir() string {
	return m.capturedAbsolutePath(m.form.dir.Value(), m.workDir)
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
		m.errBar.text = "worktree sessions need a git repository: " + dir + " is not one"
		return nil
	}
	m.errBar.text = ""
	m.form.worktree = !m.form.worktree
	m.form.worktreeAuto = false
	m.form.defaultsTouched = true
	return nil
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	return m.submitFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	if len(m.form.toolNames) == 0 {
		m.errBar.text = "no tools configured"
		m.mode = modeList
		return m, nil
	}
	toolName := m.form.toolNames[m.form.toolIndex]
	if m.form.prompt.pasting() {
		m.errBar.text = "still reading the pasted image - try again in a moment"
		return m, nil
	}

	name := strings.TrimSpace(m.form.name.Value())
	autoNamed := name == ""
	if autoNamed {
		name = toolName + "-" + newID()[:4]
	}
	group := m.selectedGroupPath()
	// Chips become the paths of the images they stand for, so a first task
	// reaches the agent with its screenshot named where it was pasted.
	prompt := m.form.prompt.message()
	if strings.HasPrefix(prompt, "-") {
		m.errBar.text = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}

	paneW, paneH := m.paneTargetSize()
	request := spawnRequest{
		kind:         spawnForm,
		toolName:     toolName,
		name:         name,
		group:        group,
		prompt:       prompt,
		autoNamed:    autoNamed,
		pickWorktree: m.form.worktree,
		pane:         sessioncmd.PaneSize{Width: paneW, Height: paneH},
		composerGen:  m.form.prompt.gen,
		images:       m.form.prompt.attachments,
		draft:        m.form.prompt.input.Value(),
		draftName:    m.form.name.Value(),
		draftDir:     m.form.dir.Value(),
		rawDir:       m.form.dir.Value(),
		dirFallbacks: m.groupDirCandidates(group),
		wantWorktree: m.form.worktree,
		dirReader:    reader,
	}
	m.errBar.text = ""
	m.dispatchSpawn(request)
	return m, m.nextEffectCmd()
}

func (m *Model) rememberSpawnPick(tool string, worktree bool) {
	m.ledger.lastSpawnTool = tool
	m.ledger.lastSpawnWorktree = worktree
}

func (m *Model) buildLaunch(toolName string, tool config.Tool, baseCommand, id string) (string, map[string]string, error) {
	return launch.Environment(m.services.hooks, toolName, tool, baseCommand, id)
}

func (m *Model) openGroupForm() {
	name := textField("group-name", 60)
	name.Focus()
	m.groupForm = groupForm{
		name:     name,
		path:     textField("default working directory", 400),
		pathAuto: true,
		focus:    gfName,
		gen:      m.nextComposerGen(),
	}
	m.rebuildGroupOptions(m.contextGroup())
	m.groupForm.path.SetValue(m.capturedGroupDefaultDir(m.selectedGroupPath()))
	m.syncGroupFormFieldWidths()
	m.pathSugg.reset()
	m.mode = modeGroupForm
	m.errBar.text = ""
}

func (m *Model) handleGroupFormKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	pathSuggesting := m.groupForm.focus == gfPath && m.pathSugg.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab":
		if pathSuggesting {
			return m, m.applyPathSuggestion()
		}
		m.groupFormFocus(1)
		return m, nil
	case "shift+tab":
		m.groupFormFocus(-1)
		return m, nil
	case "up":
		if pathSuggesting {
			if !m.pathSugg.move(-1) {
				m.groupFormFocus(-1)
			}
		} else {
			m.groupFormFocus(-1)
		}
		return m, nil
	case "down":
		if pathSuggesting {
			if !m.pathSugg.move(1) {
				m.groupFormFocus(1)
			}
		} else {
			m.groupFormFocus(1)
		}
		return m, nil
	case "left":
		if m.groupForm.focus == gfWorktree {
			count := len(groupWorktreeOptions)
			m.groupForm.worktreeIndex = (m.groupForm.worktreeIndex + count - 1) % count
			return m, nil
		}
		if m.groupForm.focus == gfParent {
			return m, m.moveGroupCursor(-1)
		}
	case "right":
		if m.groupForm.focus == gfWorktree {
			m.groupForm.worktreeIndex = (m.groupForm.worktreeIndex + 1) % len(groupWorktreeOptions)
			return m, nil
		}
		if m.groupForm.focus == gfParent {
			return m, m.moveGroupCursor(1)
		}
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			return m, m.applyPathSuggestion()
		}
		return m.submitGroupForm()
	}

	var cmd tea.Cmd
	switch m.groupForm.focus {
	case gfName:
		m.groupForm.name, cmd = m.groupForm.name.Update(msg)
	case gfPath:
		m.groupForm.path, cmd = m.groupForm.path.Update(msg)
		m.groupForm.pathAuto = false
		cmd = tea.Batch(cmd, m.requestPathSuggestions(pathSuggestionGroup, m.groupForm.path.Value()))
	}
	return m, cmd
}

func (m *Model) groupFormFocus(delta int) {
	m.pathSugg.reset()
	m.groupForm.focus = (m.groupForm.focus + delta + gfCount) % gfCount
	m.groupForm.name.Blur()
	m.groupForm.path.Blur()
	switch m.groupForm.focus {
	case gfName:
		m.groupForm.name.Focus()
	case gfPath:
		m.groupForm.path.Focus()
	}
}

func (m *Model) submitGroupForm() (tea.Model, tea.Cmd) {
	return m.submitGroupFormWithReader(systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) submitGroupFormWithReader(reader directoryPreflight) (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(m.groupForm.name.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		m.errBar.text = "group name cannot be empty"
		return m, nil
	}
	parent := m.selectedGroupPath()
	full := name
	if parent != "" {
		full = parent + "/" + name
	}
	worktree := groupWorktreeValue(m.groupForm.worktreeIndex)
	request := groupRequest{
		path: full, worktree: worktree, gen: m.groupForm.gen,
		draftName: m.groupForm.name.Value(), draftDir: m.groupForm.path.Value(),
		rawDir: m.groupForm.path.Value(), fallbacks: m.groupDirCandidates(parent), dirReader: reader,
	}
	m.errBar.text = ""
	m.dispatchGroup(request)
	return m, m.nextEffectCmd()
}

// dispatchGroup queues the group's store write on the effect lane; the row
// materialization and reveal snapshot land when it completes.
func (m *Model) dispatchGroup(request groupRequest) {
	request.draftName = m.groupForm.name.Value()
	request.draftDir = m.groupForm.path.Value()
	m.enqueueEffect(request, 0, false)
}
