package ui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"sort"
	"strings"
)

type renameTarget struct {
	isGroup       bool
	path          string
	sessID        string
	input         textinput.Model
	dir           textinput.Model
	worktreeIndex int
	base          string
	focus         int
	toolNames     []string
	toolIndex     int
}

// renameDialog edits the selected session's name and tool, or a group's
// name, default path, worktree and base choices. It owns the fields, focus,
// keys, validation and the rename request it submits. The root opens it from
// the selected row and configuration, shares its path completion and base
// probe, runs the request on the effect lane and closes it from the fenced
// completion.
type renameDialog struct{ renameTarget }

// renameHost is what the rename dialog reaches on the root: the shared path
// completion, the group base stepper, mode changes, the status bar and the
// rename effect lane.
type renameHost interface {
	pathSuggestions() *pathComplete
	applyPathSuggestion() tea.Cmd
	requestPathSuggestions(target pathSuggestionTarget, typed string) tea.Cmd
	stepRenameBase(current string, delta int) (string, tea.Cmd)
	setMode(next mode)
	reportErr(text string)
	queueRename(request renameRequest) tea.Cmd
}

var _ renameHost = (*Model)(nil)

// stepRenameBase steps the group base choice, probing the default path the
// open group edit would save.
func (m *Model) stepRenameBase(current string, delta int) (string, tea.Cmd) {
	return m.stepGroupBase(groupBaseRename, m.gens.dialog, m.renameGroupDir(), current, delta)
}

// openRename stays on the root: it reads the selected row, the workspace's
// group choices and the configured tools to seed the dialog.
func (m *Model) openRename() {
	m.gens.dialog++
	entry, ok := m.selectedRow()
	if !ok {
		return
	}
	if entry.isRoot() {
		m.reportErr("root is the top level, not a group to rename")
		return
	}
	input := textinput.New()
	input.CharLimit = 60
	input.Prompt = ""
	input.Focus()
	if entry.isGroup {
		input.SetValue(baseName(entry.group))
		dir := textField("default working directory", 400)
		dir.Prompt = ""
		dirValue := m.workspace.groupPaths[entry.group]
		if dirValue == "" {
			dirValue = m.capturedGroupDefaultDir(entry.group)
		}
		dir.SetValue(dirValue)
		m.pathSugg.reset()
		m.rename.renameTarget = renameTarget{
			isGroup:       true,
			path:          entry.group,
			input:         input,
			dir:           dir,
			worktreeIndex: groupWorktreeIndex(m.workspace.groupWorktrees[entry.group]),
			base:          m.workspace.groupBases[entry.group],
		}
	} else {
		input.SetValue(entry.sess.Name)
		tools := m.services.cfg.AgentToolNames()
		shells := []string{}
		for _, name := range m.services.cfg.ToolNames() {
			if m.services.cfg.Tools[name].Shell {
				shells = append(shells, name)
			}
		}
		sort.Strings(shells)
		tools = append(tools, shells...)
		toolIndex := 0
		for i, name := range tools {
			if name == entry.sess.Tool {
				toolIndex = i
				break
			}
		}
		// Current tool missing from config (removed block): keep it selectable
		// so save does not silently reassign to the first configured tool.
		if len(tools) == 0 || tools[toolIndex] != entry.sess.Tool {
			tools = append([]string{entry.sess.Tool}, tools...)
			toolIndex = 0
		}
		m.rename.renameTarget = renameTarget{
			sessID:    entry.sess.ID,
			input:     input,
			toolNames: tools,
			toolIndex: toolIndex,
		}
	}
	m.mode = modeRename
	m.clearErr()
}

func (d *renameDialog) focusField(h renameHost, delta int) {
	h.pathSuggestions().reset()
	fields := 2
	if d.isGroup {
		fields = 4
	}
	d.focus = (d.focus + delta + fields) % fields
	d.input.Blur()
	d.dir.Blur()
	switch d.focus {
	case 0:
		d.input.Focus()
	case 1:
		d.dir.Focus()
	}
}

func (d *renameDialog) handleKey(h renameHost, msg tea.KeyMsg) tea.Cmd {
	pathSugg := h.pathSuggestions()
	pathSuggesting := d.isGroup && d.focus == 1 && pathSugg.active()
	switch msg.String() {
	case "esc":
		if pathSuggesting {
			pathSugg.reset()
			return nil
		}
		h.setMode(modeList)
		return nil
	case "tab":
		if !d.isGroup {
			d.cycleTool(1)
			return nil
		}
		if pathSuggesting {
			return h.applyPathSuggestion()
		}
		d.focusField(h, 1)
		return nil
	case "shift+tab":
		if !d.isGroup {
			d.cycleTool(-1)
			return nil
		}
		if pathSuggesting {
			return nil
		}
		d.focusField(h, -1)
		return nil
	case "up", "down":
		if !d.isGroup {
			break
		}
		if pathSuggesting {
			if msg.String() == "up" {
				if !pathSugg.move(-1) {
					d.focusField(h, -1)
				}
			} else {
				if !pathSugg.move(1) {
					d.focusField(h, 1)
				}
			}
			return nil
		}
		if msg.String() == "up" {
			d.focusField(h, -1)
		} else {
			d.focusField(h, 1)
		}
		return nil
	case "left", "right":
		delta := 1
		if msg.String() == "left" {
			delta = -1
		}
		if d.isGroup && d.focus == 2 {
			count := len(groupWorktreeOptions)
			d.worktreeIndex = (d.worktreeIndex + delta + count) % count
			return nil
		}
		if d.isGroup && d.focus == 3 {
			var cmd tea.Cmd
			d.base, cmd = h.stepRenameBase(d.base, delta)
			return cmd
		}
	case "enter":
		if pathSuggesting && pathSugg.chosen {
			return h.applyPathSuggestion()
		}
		return d.submit(h)
	}
	var cmd tea.Cmd
	switch d.focus {
	case 0:
		d.input, cmd = d.input.Update(msg)
	case 1:
		d.dir, cmd = d.dir.Update(msg)
		cmd = tea.Batch(cmd, h.requestPathSuggestions(pathSuggestionRename, d.dir.Value()))
	}
	return cmd
}

// renameGroupDir is the default path the group edit would save, resolved
// the way the rename worker resolves it.
func (m *Model) renameGroupDir() string {
	return m.capturedAbsolutePath(m.rename.dir.Value(), m.capturedGroupDefaultDir(parentGroup(m.rename.path)))
}

func (d *renameDialog) cycleTool(delta int) {
	if len(d.toolNames) == 0 {
		return
	}
	n := len(d.toolNames)
	d.toolIndex = (d.toolIndex + delta + n) % n
}

func (d *renameDialog) tool() string {
	if len(d.toolNames) == 0 {
		return ""
	}
	return d.toolNames[d.toolIndex]
}

// submit validates the name and hands the root the rename it captured; the
// root fills in the inventory facts and the dialog generation.
func (d *renameDialog) submit(h renameHost) tea.Cmd {
	name := strings.TrimSpace(d.input.Value())
	name = strings.ReplaceAll(name, "/", "-")
	if name == "" {
		h.reportErr("name cannot be empty")
		return nil
	}
	if d.isGroup {
		parent := parentGroup(d.path)
		newPath := name
		if parent != "" {
			newPath = parent + "/" + name
		}
		return h.queueRename(renameRequest{
			kind:     renameGroup,
			oldGroup: d.path,
			newGroup: newPath,
			rawDir:   d.dir.Value(),
			draftDir: d.dir.Value(),
			worktree: groupWorktreeValue(d.worktreeIndex),
			base:     d.base,
			name:     name,
		})
	}
	return h.queueRename(renameRequest{
		kind:   renameSession,
		sessID: d.sessID,
		name:   name,
		tool:   d.tool(),
	})
}

// renameGroupLocally rewrites the in-memory tree right away, so the
// frames between saving and the poller's next refresh already show the
// new name and path instead of flashing the stale ones.
func (m *Model) renameGroupLocally(old, newPath, dir, worktree, base string) {
	m.renameGroupInventory(old, newPath)
	m.workspace.groupPaths[newPath] = dir
	m.workspace.groupWorktrees = setGroupChoice(m.workspace.groupWorktrees, newPath, worktree)
	m.workspace.groupBases = setGroupChoice(m.workspace.groupBases, newPath, base)
	m.applyRailStateDecision(m.rail.RenameGroup(old, newPath))
}

// renameGroupInventory moves the in-memory groups and sessions under a
// renamed path; metadata maps are mirrored by the caller, which decides
// which stages committed.
func (m *Model) renameGroupInventory(old, newPath string) {
	moved := func(group string) (string, bool) {
		if group == old || strings.HasPrefix(group, old+"/") {
			return newPath + group[len(old):], true
		}
		return group, false
	}
	for i := range m.workspace.groups {
		m.workspace.groups[i], _ = moved(m.workspace.groups[i])
	}
	for i := range m.workspace.sessions {
		m.workspace.sessions[i].Group, _ = moved(m.workspace.sessions[i].Group)
	}
	groupPaths := make(map[string]string, len(m.workspace.groupPaths))
	for group, path := range m.workspace.groupPaths {
		group, _ = moved(group)
		groupPaths[group] = path
	}
	m.workspace.groupPaths = groupPaths
	m.workspace.groupWorktrees = movedChoices(m.workspace.groupWorktrees, moved)
	m.workspace.groupBases = movedChoices(m.workspace.groupBases, moved)
}

// movedChoices rekeys a per-group choice map for a renamed subtree.
func movedChoices(choices map[string]string, moved func(string) (string, bool)) map[string]string {
	out := make(map[string]string, len(choices))
	for group, choice := range choices {
		group, _ = moved(group)
		out[group] = choice
	}
	return out
}

// setGroupChoice records a group's own choice, "" dropping it to inherit.
func setGroupChoice(choices map[string]string, group, value string) map[string]string {
	if choices == nil {
		choices = map[string]string{}
	}
	if value == "" {
		delete(choices, group)
	} else {
		choices[group] = value
	}
	return choices
}
