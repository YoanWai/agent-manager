package ui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/hooks"
	"github.com/YoanWai/agent-manager/internal/mcpreg"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

const (
	fieldName = iota
	fieldTool
	fieldDir
	fieldPrompt
	fieldGroup
	fieldCount
)

const (
	gfName = iota
	gfParent
	gfPath
	gfCount
)

type groupOption struct {
	path  string
	depth int
}

type form struct {
	name       textinput.Model
	dir        textinput.Model
	prompt     textinput.Model
	dirAuto    bool
	toolNames  []string
	toolIndex  int
	groups     []groupOption
	groupIndex int
	focus      int
}

type groupForm struct {
	name     textinput.Model
	path     textinput.Model
	pathAuto bool
	focus    int
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
var toolDisplayOrder = []string{"claude", "opencode", "codex", "grok"}

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

func (m *Model) openForm() {
	tools := sortedToolNames(m.cfg)

	name := textField("my-session", 60)
	name.Focus()

	dir := textField("", 400)
	prompt := textField("first task (optional)", 2000)

	m.form = form{
		name:      name,
		dir:       dir,
		prompt:    prompt,
		dirAuto:   true,
		toolNames: tools,
		focus:     fieldName,
	}
	m.rebuildGroupOptions(m.contextGroup())
	m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
	m.pathSugg.reset()
	m.mode = modeForm
	m.err = ""
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
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(-1)
		} else if dirSuggesting {
			m.pathSugg.move(-1)
		} else {
			m.formFocus(-1)
		}
		return m, nil
	case "down":
		if m.form.focus == fieldGroup {
			m.moveGroupCursor(1)
		} else if dirSuggesting {
			m.pathSugg.move(1)
		} else {
			m.formFocus(1)
		}
		return m, nil
	case "left":
		if m.form.focus == fieldTool {
			m.cycleTool(-1)
			return m, nil
		}
	case "right":
		if m.form.focus == fieldTool {
			m.cycleTool(1)
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
		m.form.prompt, cmd = m.form.prompt.Update(msg)
	}
	return m, cmd
}

func (m *Model) moveGroupCursor(delta int) {
	m.form.groupIndex += delta
	if m.form.groupIndex < 0 {
		m.form.groupIndex = 0
	}
	if m.form.groupIndex >= len(m.form.groups) {
		m.form.groupIndex = len(m.form.groups) - 1
	}
	if m.mode == modeForm && m.form.dirAuto {
		m.form.dir.SetValue(m.groupDefaultDir(m.selectedGroupPath()))
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

func (m *Model) submitForm() (tea.Model, tea.Cmd) {
	if len(m.form.toolNames) == 0 {
		m.err = "no tools configured"
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
		m.err = "working directory does not exist: " + dir
		return m, nil
	}
	group := m.selectedGroupPath()
	prompt := strings.TrimSpace(m.form.prompt.Value())
	if strings.HasPrefix(prompt, "-") {
		m.err = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}

	if err := m.spawnSession(toolName, name, dir, group, prompt, autoNamed); err != nil {
		m.err = err.Error()
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

// directiveEmbeddable reports whether the rename directive can ride the
// session's first prompt; otherwise it is sent later as its own message.
func directiveEmbeddable(prompt string) bool {
	return prompt != "" && !strings.HasPrefix(prompt, "/")
}

// launchPrompt prepends the rename directive when an auto-named session
// launches with an embeddable prompt; other launches stay clean.
func launchPrompt(prompt string, autoNamed bool) string {
	if autoNamed && directiveEmbeddable(prompt) {
		return renameDirective + "\n\n" + prompt
	}
	return prompt
}

// spawnSession creates the tmux session and its store record for both
// the New Session form and quick spawn. autoNamed marks sessions whose
// name is a generated placeholder; when they launch with a prompt, the
// agent is asked to rename the session itself.
func (m *Model) spawnSession(toolName, name, dir, group, prompt string, autoNamed bool) error {
	tool := m.cfg.Tools[toolName]
	id := newID()
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
	command, env, err := m.buildLaunch(toolName, tool, base, id)
	if err != nil {
		return err
	}
	if err := m.tmux.Create(id, dir, command, env, m.previewPaneWidth(), m.previewPaneHeight()); err != nil {
		return err
	}
	sess := store.Session{
		ID:             id,
		Name:           name,
		Tool:           toolName,
		Cwd:            dir,
		Group:          group,
		Status:         tool.DefaultStatus,
		AgentSessionID: agentSessionID,
	}
	if err := m.store.CreateSession(sess); err != nil {
		_ = m.tmux.Kill(id)
		_ = m.hooks.Remove(id)
		return err
	}
	if deferDirective {
		m.poller.markDirectivePending(id)
	}
	return m.tmux.SetLabel(id, sessionLabel(group, name))
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
	m.pathSugg.reset()
	m.mode = modeGroupForm
	m.err = ""
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
		if m.groupForm.focus == gfParent {
			m.moveGroupCursor(-1)
		} else if pathSuggesting {
			m.pathSugg.move(-1)
		} else {
			m.groupFormFocus(-1)
		}
		return m, nil
	case "down":
		if m.groupForm.focus == gfParent {
			m.moveGroupCursor(1)
		} else if pathSuggesting {
			m.pathSugg.move(1)
		} else {
			m.groupFormFocus(1)
		}
		return m, nil
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
		m.err = "group name cannot be empty"
		return m, nil
	}
	parent := m.selectedGroupPath()
	full := name
	if parent != "" {
		full = parent + "/" + name
	}
	path, ok := resolveExistingDir(m.groupForm.path.Value(), m.groupDefaultDir(parent))
	if !ok {
		m.err = "default path does not exist: " + path
		return m, nil
	}
	if err := m.store.CreateGroup(full, path); err != nil {
		m.err = err.Error()
		return m, nil
	}
	m.mode = modeList
	return m, m.refreshCmd()
}
