package ui

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
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
	path  string
	depth int
}

type form struct {
	name         textinput.Model
	dir          textinput.Model
	prompt       textarea.Model
	dirAuto      bool
	toolNames    []string
	toolIndex    int
	groups       []groupOption
	groupIndex   int
	worktree     bool
	worktreeAuto bool
	focus        int
}

type groupForm struct {
	name          textinput.Model
	path          textinput.Model
	pathAuto      bool
	worktreeIndex int
	focus         int
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
	info, err := os.Stat(dir)
	return dir, err == nil && info.IsDir()
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

func promptField() textarea.Model {
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
	return in
}

// formValueWidth is the columns a field value can occupy inside the card.
func (m *Model) formValueWidth() int {
	return m.cardWidth() - 2*cardPaddingX - formLabelColumn
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
	m.form.prompt.SetWidth(inner)
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

// ancestorGroupDir finds the closest configured default path walking up
// from the group to the root; empty when no ancestor has one.
func (m *Model) ancestorGroupDir(group string) string {
	for g := group; g != ""; g = parentGroup(g) {
		if p := m.groupPaths[g]; p != "" {
			if info, err := os.Stat(p); err == nil && info.IsDir() {
				return p
			}
		}
	}
	return ""
}

// groupDefaultDir resolves the working directory for a session in a group:
// the nearest inherited default path, else the current directory.
func (m *Model) groupDefaultDir(group string) string {
	if p := m.ancestorGroupDir(group); p != "" {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	return cwd
}

// toolDisplayOrder fixes the order tools appear in when creating a session and
// when cycling the quick-spawn tool. Tools outside this list follow, sorted
// alphabetically.
var toolDisplayOrder = []string{"claude", "opencode", "codex", "grok", "gemini"}

func sortedToolNames(cfg config.Config) []string {
	names := cfg.ToolNames()
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

// enabledToolNames is the create-session picker: configured tools minus any
// the user hid in settings. Existing sessions keep their tool even when hidden.
func (m *Model) enabledToolNames() []string {
	all := sortedToolNames(m.cfg)
	hidden := m.hiddenTools()
	if len(hidden) == 0 {
		return all
	}
	out := make([]string, 0, len(all))
	for _, name := range all {
		if !hidden[name] {
			out = append(out, name)
		}
	}
	return out
}

func (m *Model) openForm() {
	tools := m.enabledToolNames()
	if len(tools) == 0 {
		m.errBar.text = "no CLIs enabled: open settings (s), then CLIs, to turn some on"
		return
	}

	name := textField("my-session", 60)
	name.Focus()

	dir := textField("", 400)
	prompt := promptField()

	m.form = form{
		name:      name,
		dir:       dir,
		prompt:    prompt,
		dirAuto:   true,
		toolNames: tools,
		focus:     fieldName,
	}
	m.errBar.text = ""
	m.syncFormFieldWidths()
	m.forgetWorktreeCapability()
	m.rebuildGroupOptions(m.contextGroup())
	m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
	m.form.worktree = m.groupWorktree(m.selectedGroupPath())
	m.form.worktreeAuto = true
	m.pathSugg.reset()
	m.mode = modeForm
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
	paths := groupClosure(m.groups, m.sessions)
	for path := range paths {
		if m.groupEffectivelyArchived(path) {
			delete(paths, path)
		}
	}
	children := childIndex(paths, m.groups)

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
	switch msg.String() {
	case "esc":
		if dirSuggesting {
			m.pathSugg.reset()
			return m, nil
		}
		m.mode = modeList
		return m, nil
	case "tab":
		if dirSuggesting {
			m.applyPathSuggestion()
			return m, nil
		}
		m.formFocus(1)
		return m, nil
	case "shift+tab":
		m.formFocus(-1)
		return m, nil
	case "up":
		if dirSuggesting {
			if !m.pathSugg.move(-1) {
				m.formFocus(-1)
			}
		} else {
			m.formFocus(-1)
		}
		return m, nil
	case "down":
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
			m.toggleFormWorktree()
			return m, nil
		}
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(-1)
			return m, nil
		}
	case "right":
		if m.form.focus == fieldTool {
			m.cycleTool(1)
			return m, nil
		}
		if m.form.focus == fieldWorktree {
			m.toggleFormWorktree()
			return m, nil
		}
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(1)
			return m, nil
		}
	case "enter":
		if dirSuggesting && m.pathSugg.chosen {
			m.applyPathSuggestion()
			return m, nil
		}
		return m.submitForm()
	}

	var cmd tea.Cmd
	switch m.form.focus {
	case fieldName:
		m.form.name, cmd = m.form.name.Update(msg)
	case fieldDir:
		m.form.dir, cmd = m.form.dir.Update(msg)
		m.form.dirAuto = false
		m.pathSugg.recompute(m.form.dir.Value())
	case fieldPrompt:
		// Update repositions the viewport against the height set at the
		// last render; full cap height here keeps the first row from
		// scrolling away when a keystroke adds a wrapped row.
		m.form.prompt.SetHeight(formPromptMaxRows)
		m.form.prompt, cmd = m.form.prompt.Update(msg)
	}
	return m, cmd
}

// moveGroupCursor moves within the expanded group picker, wrapping at the
// ends; a delta of 0 re-resolves the dependent defaults in place.
func (m *Model) moveGroupCursor(delta int) {
	count := len(m.form.groups)
	if count == 0 {
		return
	}
	m.form.groupIndex = (m.form.groupIndex + delta + count) % count
	if m.mode == modeForm && m.form.dirAuto {
		m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
	}
	if m.mode == modeForm && m.form.worktreeAuto {
		m.form.worktree = m.groupWorktree(m.selectedGroupPath())
	}
	if m.mode == modeGroupForm && m.groupForm.pathAuto {
		m.groupForm.path.SetValue(m.ancestorGroupDir(m.selectedGroupPath()))
	}
}

func (m *Model) formFocus(delta int) {
	m.pathSugg.reset()
	m.form.focus = (m.form.focus + delta + fieldCount) % fieldCount
	m.form.name.Blur()
	m.form.dir.Blur()
	m.form.prompt.Blur()
	switch m.form.focus {
	case fieldName:
		m.form.name.Focus()
	case fieldDir:
		m.form.dir.Focus()
	case fieldPrompt:
		m.form.prompt.Focus()
	}
}

func (m *Model) cycleTool(delta int) {
	if len(m.form.toolNames) == 0 {
		return
	}
	m.form.toolIndex = (m.form.toolIndex + delta + len(m.form.toolNames)) % len(m.form.toolNames)
}

// formSpawnDir is the directory the form would launch in, resolved the
// same way submit resolves it.
func (m *Model) formSpawnDir() string {
	cwd, _ := os.Getwd()
	dir, _ := resolveExistingDir(m.form.dir.Value(), cwd)
	return dir
}

// formWorktreeOn is the worktree state the form shows and spawns with: the
// toggle, unless the chosen directory cannot host a worktree.
func (m *Model) formWorktreeOn() bool {
	return m.form.worktree && m.worktreeCapable(m.formSpawnDir())
}

// toggleFormWorktree flips the toggle, or explains why the chosen
// directory rules a worktree out.
func (m *Model) toggleFormWorktree() {
	dir := m.formSpawnDir()
	if !m.worktreeCapable(dir) {
		m.errBar.text = "worktree sessions need a git repository: " + dir + " is not one"
		return
	}
	m.errBar.text = ""
	m.form.worktree = !m.form.worktree
	m.form.worktreeAuto = false
}

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	if len(m.form.toolNames) == 0 {
		m.errBar.text = "no tools configured"
		m.mode = modeList
		return m, nil
	}
	toolName := m.form.toolNames[m.form.toolIndex]

	name := strings.TrimSpace(m.form.name.Value())
	autoNamed := name == ""
	if autoNamed {
		name = toolName + "-" + newID()[:4]
	}
	cwd, _ := os.Getwd()
	dir, ok := resolveExistingDir(m.form.dir.Value(), cwd)
	if !ok {
		m.errBar.text = "working directory does not exist: " + dir
		return m, nil
	}
	group := m.selectedGroupPath()
	prompt := strings.TrimSpace(m.form.prompt.Value())
	if strings.HasPrefix(prompt, "-") {
		m.errBar.text = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}

	if err := m.spawnSession(toolName, name, dir, group, prompt, autoNamed, m.formWorktreeOn()); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.mode = modeList
	return m, m.refreshCmd()
}

// renameDirective asks the agent, as the first line of its first prompt,
// to name its own session via the rename subcommand. Injected only for
// auto-named sessions that launch with a prompt, so it fires exactly once.
const renameDirective = `First, run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then do the task:`

// deferredRenameDirective is the standalone message sent into sessions
// whose first prompt could not carry the directive: slash-command
// prompts (the command must open the message) and promptless launches.
const deferredRenameDirective = `Run this exact shell command once, replacing <name> with a short 2-4 word kebab-case name for the broad feature or theme of this whole session (not one subtask of a larger feature): agent-manager rename "<name>". Run rename only this once. Do not rename again later in the conversation unless the user explicitly asks you to rename; if they do, pick a broad name from context, not a narrow step. Then continue.`

// renameAvailableNote tells a custom-named session that rename exists for
// later use without asking it to rename now.
const renameAvailableNote = `This session is already named. You can rename it later with agent-manager rename "<name>" only if the user asks. Do not rename it now. Then do the task:`

// directiveEmbeddable reports whether a launch note can ride the
// session's first prompt; otherwise auto-named sessions get the rename
// directive later as its own message.
func directiveEmbeddable(prompt string) bool {
	return prompt != "" && !strings.HasPrefix(prompt, "/")
}

// launchPrompt prepends a short agent note when the first prompt can
// carry one: auto-named sessions must rename once; custom-named sessions
// only learn that rename is available later.
func launchPrompt(prompt string, autoNamed bool) string {
	if !directiveEmbeddable(prompt) {
		return prompt
	}
	if autoNamed {
		return renameDirective + "\n\n" + prompt
	}
	return renameAvailableNote + "\n\n" + prompt
}

// spawnSession creates the tmux session and its store record for both
// the New Session form and quick spawn. autoNamed marks sessions whose
// name is a generated placeholder; those are asked to rename once.
// Custom-named sessions only get a short note that rename is available later.
// discardWorktree rolls back a worktree created for a spawn that failed
// partway; a fresh worktree is clean by construction, so the removal fires.
func (m *Model) discardWorktree(repo, path, branch string) {
	if repo == "" {
		return
	}
	_, _ = m.gitDrv.RemoveWorktreeIfClean(repo, path, branch)
}

func (m *Model) spawnSession(toolName, name, dir, group, prompt string, autoNamed, worktree bool) error {
	tool := m.cfg.Tools[toolName]
	id := newID()
	worktreeRepo, worktreeBranch := "", ""
	if worktree {
		if m.gitDrv == nil {
			return errors.New("worktree sessions need git installed")
		}
		root, err := m.gitDrv.RepoRoot(dir)
		if err != nil {
			return err
		}
		path, branch, err := m.gitDrv.AddWorktree(root, name)
		if err != nil {
			return err
		}
		dir = path
		worktreeRepo, worktreeBranch = root, branch
	}
	deferDirective := autoNamed && !directiveEmbeddable(prompt)
	prompt = launchPrompt(prompt, autoNamed)
	base := withPrompt(tool, tool.Command, prompt)
	// Tools that accept a chosen session id launch with one, so a later
	// revive resumes this exact conversation rather than the directory's
	// most recent one. Tools without the flag mint their own id, captured
	// after launch by the poller.
	agentSessionID := ""
	if tool.SessionIDFlag != "" {
		agentSessionID = uuid.NewString()
		base += " " + tool.SessionIDFlag + " " + agentSessionID
	}
	return m.launchNewSession(store.Session{
		ID:    id,
		Name:  name,
		Tool:  toolName,
		Cwd:   dir,
		Group: group,
		// Starting until the agent first draws to its pane, so the row shows
		// a launch state immediately; the poller flips it to the real status.
		Status:         status.Starting,
		AgentSessionID: agentSessionID,
		WorktreeRepo:   worktreeRepo,
		WorktreeBranch: worktreeBranch,
	}, base, deferDirective)
}

// withPrompt embeds an optional starting prompt into a tool's launch
// command; tools whose positional argument is not a prompt route it
// through their prompt_flag.
func withPrompt(tool config.Tool, command, prompt string) string {
	if prompt == "" {
		return command
	}
	if tool.PromptFlag != "" {
		return command + " " + tool.PromptFlag + " " + tmux.ShellQuote(prompt)
	}
	return command + " " + tmux.ShellQuote(prompt)
}

// buildLaunch resolves the shell command and environment a session
// launches with. Every session carries its id so the rename subcommand
// can find it; tools backed by hooks additionally get the generated
// settings file and their status-file path, plus a clean slate from any
// earlier files under the same id.
func (m *Model) buildLaunch(toolName string, tool config.Tool, baseCommand, id string) (string, map[string]string, error) {
	if err := m.hooks.RemoveName(id); err != nil {
		return "", nil, err
	}
	env := map[string]string{hooks.EnvSessionID: id}
	command, err := mcpreg.Apply(mcpreg.Style(toolName, tool.MCP), mcpExecutable(), m.hooks.Dir(), baseCommand, env)
	if err != nil {
		return "", nil, err
	}
	if tool.StatusSource != hooks.StatusSourceClaude {
		return command, env, nil
	}
	settingsPath, err := m.hooks.EnsureSettings()
	if err != nil {
		return "", nil, err
	}
	if err := m.hooks.Remove(id); err != nil {
		return "", nil, err
	}
	env[hooks.EnvStatusFile] = m.hooks.StatusFile(id)
	return command + " --settings " + tmux.ShellQuote(settingsPath), env, nil
}

// mcpExecutable names the binary generated MCP configs point at: the
// running manager itself, falling back to the PATH-resolved name.
func mcpExecutable() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "agent-manager"
}

func (m *Model) openGroupForm() {
	name := textField("group-name", 60)
	name.Focus()
	m.groupForm = groupForm{
		name:     name,
		path:     textField("default working directory", 400),
		pathAuto: true,
		focus:    gfName,
	}
	m.rebuildGroupOptions(m.contextGroup())
	m.groupForm.path.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
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
			m.applyPathSuggestion()
			return m, nil
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
			m.moveGroupCursor(-1)
			return m, nil
		}
	case "right":
		if m.groupForm.focus == gfWorktree {
			m.groupForm.worktreeIndex = (m.groupForm.worktreeIndex + 1) % len(groupWorktreeOptions)
			return m, nil
		}
		if m.groupForm.focus == gfParent {
			m.moveGroupCursor(1)
			return m, nil
		}
	case "enter":
		if pathSuggesting && m.pathSugg.chosen {
			m.applyPathSuggestion()
			return m, nil
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
		m.pathSugg.recompute(m.groupForm.path.Value())
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
	path, ok := resolveExistingDir(m.groupForm.path.Value(), m.groupDefaultDir(parent))
	if !ok {
		m.errBar.text = "default path does not exist: " + path
		return m, nil
	}
	worktree := groupWorktreeValue(m.groupForm.worktreeIndex)
	if err := m.store.AddGroup(full, path, worktree); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	m.materializeGroupsLocal([]string{full})
	if m.groupPaths == nil {
		m.groupPaths = map[string]string{}
	}
	m.groupPaths[full] = path
	if m.groupWorktrees == nil {
		m.groupWorktrees = map[string]string{}
	}
	if worktree == "" {
		delete(m.groupWorktrees, full)
	} else {
		m.groupWorktrees[full] = worktree
	}
	for group := parent; group != ""; group = parentGroup(group) {
		delete(m.collapsed, group)
	}
	m.persistCollapsed()
	m.search = ""
	m.searching = false
	m.showArchived = false
	m.hideEmptyGroups = false
	m.errBar.text = ""
	m.mode = modeList
	m.rebuildRows()
	for i, row := range m.rows {
		if row.isGroup && row.group == full {
			m.cursor = i
			break
		}
	}
	return m, m.refreshCmd()
}
