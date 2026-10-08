package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) openQuickMode() tea.Cmd {
	return m.openQuickModeWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openQuickModeWithReader(reader settingsValueReader) tea.Cmd {
	names, index := m.cachedSpawnToolSelection()
	if len(names) == 0 {
		m.errBar.text = "no CLIs enabled: open settings (s), then CLIs, to turn some on"
		return nil
	}
	input := textarea.New()
	input.CharLimit = 2000
	input.Placeholder = "type and press enter"
	input.ShowLineNumbers = false
	input.SetPromptFunc(2, func(lineIndex int) string {
		if lineIndex == 0 {
			return keyStyle.Render("❯ ")
		}
		return "  "
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	holdOpen(&input)
	input.Focus()
	m.errBar.text = ""
	m.forgetWorktreeCapability()
	m.quick = quickState{
		active:         true,
		composer:       composer{input: input, maxRows: quickBarMaxRows, gen: m.nextComposerGen()},
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.settings.cache.value(quickCloseSetting) == "close",
		worktree:       m.cachedSpawnWorktreeDefault(m.quickTargetGroup()),
		choice:         m.newChoice(names[index]),
	}
	catalog := m.ensureCatalog(names[index])
	if m.settings.pending > 0 {
		return tea.Batch(m.quickWorktreeProbeCmd(false), catalog)
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadQuick, generation: uint64(m.quick.gen), extra: m.choiceSettingKeys()}, reader), catalog)
}

// applyCachedQuickDefaults takes the loaded defaults. A tool the load moved
// to starts its choices over and asks its CLI.
func (m *Model) applyCachedQuickDefaults() tea.Cmd {
	before := m.quickTool()
	m.quick.toolNames, m.quick.toolIndex = m.cachedSpawnToolSelection()
	m.quick.closeAfterSend = m.settings.cache.value(quickCloseSetting) == "close"
	if !m.quick.worktreeTouched {
		m.quick.worktree = m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
	}
	toolName := m.quickTool()
	if toolName == before || toolName == "" {
		return nil
	}
	m.quick.choice = m.newChoice(toolName)
	return m.ensureCatalog(toolName)
}

// handleQuickKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor) unless the
// caret has a prompt row to move to, enter submits against whatever is
// selected, and every other key is typed text.
func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	msg = typedText(msg)
	if m.quick.picking != pickNone {
		// A click can move the target off the group the list was opened for.
		if m.quickSpawning() {
			return m.handleQuickPickKey(msg)
		}
		m.closeQuickPick()
	}
	switch msg.String() {
	case "esc":
		m.quick.active = false
		// Reopening the bar starts a fresh prompt, so the images this one
		// was holding have nowhere left to be referenced from.
		m.quick.release()
		return m, nil
	case "up":
		if cmd, stepped := m.quick.stepRow(msg); stepped {
			return m, cmd
		}
		return m, tea.Batch(m.moveCursor(-1), m.quickWorktreeProbeCmd(false))
	case "down":
		if cmd, stepped := m.quick.stepRow(msg); stepped {
			return m, cmd
		}
		return m, tea.Batch(m.moveCursor(1), m.quickWorktreeProbeCmd(false))
	case "tab", "alt+m":
		return m, m.cycleQuickTool(1)
	case "shift+tab":
		return m, m.cycleQuickTool(-1)
	case quickModelKey:
		m.openQuickPick(pickModel)
		return m, nil
	case quickEffortKey:
		m.stepQuickEffort()
		return m, nil
	case quickProfileKey:
		m.stepQuickProfile()
		return m, nil
	case "ctrl+t", "alt+w":
		return m, m.toggleQuickWorktree()
	case "enter":
		return m.submitQuick()
	}
	if cmd, handled := m.composerKey(composerQuick, msg); handled {
		return m, cmd
	}
	return m, m.quick.typeKey(msg)
}

// submitQuick answers the selected session, or spawns a new session with
// the prompt embedded when a group is selected. The bar stays active by
// default so consecutive prompts flow without re-arming; the "after quick
// send" setting closes it instead.
func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	entry, ok := m.selectedRow()
	if !ok {
		m.errBar.text = "nothing selected"
		return m, nil
	}
	if m.quick.pasting() {
		m.errBar.text = "still reading the pasted image - try again in a moment"
		return m, nil
	}
	text := m.quick.message()
	if text == "" {
		m.errBar.text = "prompt cannot be empty"
		return m, nil
	}
	if entry.isGroup {
		return m.quickSpawn(entry.group, text)
	}
	request := quickSendRequest{
		session:        entry.sess,
		composerGen:    m.quick.gen,
		draft:          m.quick.input.Value(),
		text:           text,
		closeAfterSend: m.quick.closeAfterSend,
		images:         m.quick.attachments,
	}
	if !m.dispatchQuickSend(request) {
		return m, nil
	}
	m.errBar.text = ""
	return m, m.nextEffectCmd()
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	return m.quickSpawnWithReader(group, prompt, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) quickSpawnWithReader(group, prompt string, reader directoryPreflight) (tea.Model, tea.Cmd) {
	if strings.HasPrefix(prompt, "-") {
		m.errBar.text = `prompt cannot start with "-": the tool would read it as a flag`
		return m, nil
	}
	toolName := m.quickTool()
	if toolName == "" {
		m.errBar.text = "no tools configured"
		return m, nil
	}
	picked, err := m.launchChoice(toolName, &m.quick.choice, "")
	if err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	name := toolName + "-" + newID()[:4]
	pickWorktree := m.cachedSpawnWorktreeDefault(group)
	if m.quick.worktreeTouched {
		pickWorktree = m.quick.worktree
	}
	paneW, paneH := m.paneTargetSize()
	request := spawnRequest{
		kind:         spawnQuick,
		toolName:     toolName,
		name:         name,
		group:        group,
		prompt:       prompt,
		autoNamed:    true,
		pickWorktree: pickWorktree,
		choice:       picked,
		base:         m.groupBase(group),
		pane:         sessioncmd.PaneSize{Width: paneW, Height: paneH},
		composerGen:  m.quick.gen,
		images:       m.quick.attachments,
		draft:        m.quick.input.Value(),
		rawDir:       m.workspace.groupPaths[group],
		dirFallbacks: m.groupDirCandidates(group),
		wantWorktree: pickWorktree,
		dirReader:    reader,
	}
	m.errBar.text = ""
	m.dispatchSpawn(request)
	return m, m.nextEffectCmd()
}

// clearQuickAfterSend empties the bar for the next prompt, and dismisses it
// entirely when the settings toggle asks for that.
func (m *Model) clearQuickAfterSend() {
	m.quick.input.SetValue("")
	m.quick.attachments = nil
	if m.quick.closeAfterSend {
		m.quick.active = false
	}
}

// toggleQuickWorktree flips the worktree choice, or probes the target first
// when its repo verdict is not cached.
func (m *Model) toggleQuickWorktree() tea.Cmd {
	dir := m.quickTargetDir()
	capable, known := m.cachedWorktreeCapability(dir)
	if !known {
		return m.quickWorktreeProbeCmd(true)
	}
	if !capable {
		m.errBar.text = "worktree sessions need a git repository: " + dir + " is not one"
		return nil
	}
	m.errBar.text = ""
	m.quick.worktree = !m.quick.worktree
	m.quick.worktreeTouched = true
	m.quick.defaultsTouched = true
	return nil
}

func (m *Model) quickWorktreeOn() bool {
	capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
	if !known || !capable {
		return false
	}
	if m.quick.worktreeTouched {
		return m.quick.worktree
	}
	return m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
}

// quickTargetGroup is the group a quick spawn would land in: the selected
// group, or the group holding the selected session.
func (m *Model) quickTargetGroup() string {
	entry, ok := m.selectedRow()
	if !ok {
		return ""
	}
	if entry.isGroup {
		return entry.group
	}
	return entry.sess.Group
}

// quickTargetDir is the directory a quick spawn would launch in, resolved
// the same way quickSpawn resolves it.
func (m *Model) quickTargetDir() string {
	group := m.quickTargetGroup()
	return m.capturedAbsolutePath(m.workspace.groupPaths[group], m.capturedGroupDefaultDir(group))
}

// quickTool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (m *Model) quickTool() string {
	if len(m.quick.toolNames) == 0 {
		return ""
	}
	return m.quick.toolNames[m.quick.toolIndex]
}

// The list open above the prompt, if any.
const (
	pickNone = iota
	pickModel
	pickEffort
)

// cycleQuickTool steps the spawn CLI by delta and starts its choices over.
func (m *Model) cycleQuickTool(delta int) tea.Cmd {
	count := len(m.quick.toolNames)
	if count == 0 {
		return nil
	}
	m.quick.toolIndex = (m.quick.toolIndex + delta + count) % count
	m.quick.defaultsTouched = true
	toolName := m.quickTool()
	m.quick.choice = m.newChoice(toolName)
	return m.ensureCatalog(toolName)
}

// Choices apply to a spawn only, never to an answer.
func (m *Model) quickSpawning() bool {
	entry, ok := m.selectedRow()
	return ok && entry.isGroup
}

// Control keys, since alt never arrives from many terminals, and ones the
// prompt's editor, the manager and the common multiplexers leave free.
const (
	quickModelKey   = "ctrl+l"
	quickEffortKey  = "ctrl+x"
	quickProfileKey = "ctrl+y"
)

const quickChoiceHint = "model, effort and profile apply to a new agent: select a group to spawn one"

func (m *Model) requireQuickSpawn() bool {
	if m.quickSpawning() {
		return true
	}
	m.errBar.text = quickChoiceHint
	return false
}

func (m *Model) openQuickPick(pick int) {
	if !m.requireQuickSpawn() {
		return
	}
	toolName, ch := m.quickTool(), &m.quick.choice
	if note, listed := m.modelRowNote(toolName); !listed {
		m.errBar.text = "model: " + ansi.Strip(note)
		return
	}
	m.errBar.text = ""
	m.quick.picking = pick
	switch pick {
	case pickModel:
		ch.filter.SetValue("")
		ch.filter.Focus()
		m.openModelList(toolName, ch)
	case pickEffort:
		ch.typedEffort.Focus()
	}
	m.quick.input.Blur()
}

func (m *Model) closeQuickPick() {
	m.quick.picking = pickNone
	m.quick.choice.filter.Blur()
	m.quick.choice.typedEffort.Blur()
	m.quick.choice.sugg = modelSuggest{}
	m.quick.input.Focus()
}

// stepQuickEffort opens the typed field for a CLI that lists no levels.
func (m *Model) stepQuickEffort() {
	if !m.requireQuickSpawn() {
		return
	}
	toolName, ch := m.quickTool(), &m.quick.choice
	if m.effortTyped(toolName, ch) {
		m.openQuickPick(pickEffort)
		return
	}
	if value, shown, active := m.effortRow(toolName, ch); !active {
		if !shown {
			value = "no levels for this model"
		}
		m.errBar.text = "effort: " + ansi.Strip(value)
		return
	}
	m.errBar.text = ""
	m.cycleChoiceEffort(toolName, ch, 1)
}

func (m *Model) stepQuickProfile() {
	if !m.requireQuickSpawn() {
		return
	}
	toolName := m.quickTool()
	if _, shown := m.profileRow(toolName, &m.quick.choice); !shown {
		m.errBar.text = "profile: " + toolName + " has none"
		return
	}
	m.errBar.text = ""
	m.cycleChoiceProfile(toolName, &m.quick.choice, 1)
}

func (m *Model) handleQuickPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	toolName, ch := m.quickTool(), &m.quick.choice
	if m.quick.picking == pickEffort {
		switch msg.String() {
		case "esc", "enter", "tab":
			m.closeQuickPick()
			return m, nil
		}
		var cmd tea.Cmd
		before := ch.typedEffort.Value()
		ch.typedEffort, cmd = ch.typedEffort.Update(msg)
		if ch.typedEffort.Value() != before {
			m.keepChoice(toolName, ch)
		}
		return m, cmd
	}
	list := m.modelSuggestions(toolName, ch, ch.query())
	switch msg.String() {
	case "esc", quickModelKey:
		m.closeQuickPick()
		return m, nil
	case quickEffortKey:
		m.stepQuickEffort()
		return m, nil
	case quickProfileKey:
		m.stepQuickProfile()
		return m, nil
	case "enter", "tab":
		if len(list) > 0 {
			m.pickModel(toolName, ch, list[ch.sugg.index].model.Key())
		}
		m.closeQuickPick()
		return m, nil
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		ch.sugg.move(len(list), delta)
		return m, nil
	}
	var cmd tea.Cmd
	ch.filter, cmd = ch.filter.Update(msg)
	ch.filtering = true
	ch.sugg = modelSuggest{open: true}
	return m, cmd
}

func (m *Model) quickHitAt(x, y int) (quickHit, bool) {
	if !m.quick.active {
		return quickHit{}, false
	}
	line, col := y-m.quick.originY, x-m.quick.originX
	for _, hit := range m.quick.hits {
		if hit.line == line && col >= hit.x0 && col < hit.x1 {
			return hit, true
		}
	}
	return quickHit{}, false
}

func (m *Model) handleQuickClick(hit quickHit) tea.Cmd {
	switch hit.action {
	case quickClickTool:
		m.closeQuickPick()
		return m.cycleQuickTool(1)
	case quickClickModel:
		if m.quick.picking == pickModel {
			m.closeQuickPick()
		} else {
			m.openQuickPick(pickModel)
		}
	case quickClickEffort:
		m.stepQuickEffort()
	case quickClickProfile:
		m.stepQuickProfile()
	case quickClickWorktree:
		return m.toggleQuickWorktree()
	case quickClickEntry:
		toolName, ch := m.quickTool(), &m.quick.choice
		if list := m.modelSuggestions(toolName, ch, ch.query()); hit.entry < len(list) {
			m.pickModel(toolName, ch, list[hit.entry].model.Key())
		}
		m.closeQuickPick()
	}
	return nil
}

func (m *Model) quickLegend() [][2]string {
	toolName, ch := m.quickTool(), &m.quick.choice
	_, _, effortActive := m.effortRow(toolName, ch)
	_, hasProfiles := m.profileRow(toolName, ch)
	switch m.quick.picking {
	case pickModel:
		pairs := [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵/tab", "choose"}}
		if effortActive {
			pairs = append(pairs, [2]string{quickEffortKey, "effort"})
		}
		if hasProfiles {
			pairs = append(pairs, [2]string{quickProfileKey, "profile"})
		}
		return append(pairs, [2]string{"esc", "back to the prompt"})
	case pickEffort:
		return [][2]string{{"type", "effort"}, {"↵", "done"}, {"esc", "back to the prompt"}}
	}
	pairs := [][2]string{{"↵", "send"}, {"↑↓", "target or caret"}, {"tab", "tool"}}
	if len(m.quick.toolNames) > 1 {
		pairs = append(pairs, [2]string{"shift+tab", "previous tool"})
	}
	if m.quickSpawning() {
		if _, listed := m.modelRowNote(toolName); listed {
			pairs = append(pairs, [2]string{quickModelKey, "model"})
		}
		if effortActive {
			pairs = append(pairs, [2]string{quickEffortKey, "effort"})
		}
		if hasProfiles {
			pairs = append(pairs, [2]string{quickProfileKey, "profile"})
		}
	}
	return append(pairs, [2]string{"ctrl+t", "worktree"}, [2]string{"esc", "close"})
}
