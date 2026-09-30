package ui

import (
	"cmp"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func (m *Model) openQuickMode() tea.Cmd {
	names, index := m.spawnToolSelection()
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
	input.SetHeight(1)
	input.Focus()
	m.errBar.text = ""
	m.forgetWorktreeCapability()
	m.quick = quickState{
		active:         true,
		composer:       composer{input: input, maxRows: quickBarMaxRows, gen: m.nextComposerGen()},
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.quickCloseAfterSend(),
		worktree:       m.spawnWorktreeDefault(m.quickTargetGroup()),
		choice:         m.newChoice(names[index]),
	}
	return m.ensureCatalog(names[index])
}

// defaultToolSelection returns enabled tool names with the index of
// the configured default, ready to seed a tool picker.
func (m *Model) defaultToolSelection() ([]string, int) {
	names := m.enabledToolNames()
	current := m.defaultTool()
	index := 0
	for i, name := range names {
		if name == current {
			index = i
		}
	}
	return names, index
}

func (m *Model) spawnToolSelection() ([]string, int) {
	names, index := m.defaultToolSelection()
	for i, name := range names {
		if name == m.lastSpawnTool {
			return names, i
		}
	}
	return names, index
}

// handleQuickKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor) unless the
// caret has a prompt row to move to, enter submits against whatever is
// selected, and every other key is typed text.
func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		return m, m.moveCursor(-1)
	case "down":
		if cmd, stepped := m.quick.stepRow(msg); stepped {
			return m, cmd
		}
		return m, m.moveCursor(1)
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
		m.toggleQuickWorktree()
		return m, nil
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
	if m.isShell(entry.sess.Tool) {
		m.errBar.text = shellPromptHint(entry.sess.Name)
		return m, nil
	}
	if !m.tmux.Exists(entry.sess.ID) {
		m.errBar.text = deadSessionHint
		return m, nil
	}
	if err := m.tmux.SendText(entry.sess.ID, text); err != nil {
		m.errBar.text = err.Error()
		return m, nil
	}
	// The prompt is delivered: clear the input before anything else can
	// fail, so a retry cannot send it twice.
	m.clearQuickAfterSend()
	m.errBar.text = ""
	// A queued answer means the user expects a fresh finished alert.
	if err := m.store.SetAcked(entry.sess.ID, false); err != nil {
		m.errBar.text = "prompt sent, but clearing the alert ack failed: " + err.Error()
	}
	if err := m.store.SetLastPrompt(entry.sess.ID, text); err != nil {
		m.errBar.text = "prompt sent, but recording it for the row failed: " + err.Error()
	}
	m.requestRefresh()
	return m, nil
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
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
	dir, ok := resolveExistingDir(m.groupPaths[group], m.groupDefaultDir(group))
	if !ok {
		m.errBar.text = "group has no valid default path: " + dir
		return m, nil
	}
	name := toolName + "-" + newID()[:4]
	worktree := m.quickWorktreeOn()
	pickWorktree := m.spawnWorktreeDefault(group)
	if m.quick.worktreeTouched {
		pickWorktree = m.quick.worktree
	}
	spawn := func() error {
		if err := m.spawnSession(toolName, name, dir, group, prompt, true, worktree, picked); err != nil {
			return err
		}
		m.rememberSpawnPick(toolName, pickWorktree)
		m.rememberModel(toolName, picked)
		return nil
	}
	if err := spawn(); err != nil {
		m.reportLaunchError(err, spawn)
		// A spawn the hint dialog refused leaves nothing to send, so the
		// bar closes instead of swallowing the list keys behind the dialog;
		// the dialog releases its images once no install can still spawn it.
		if m.mode == modeLaunchHint {
			m.quick.active = false
		}
		return m, nil
	}
	// Spawned sessions start outside the attention set; clear so the new row shows.
	m.statusFilter = statusFilterAll
	m.clearQuickAfterSend()
	m.errBar.text = ""
	return m, m.refreshCmd()
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

func (m *Model) toggleQuickWorktree() {
	dir := m.quickTargetDir()
	if !m.worktreeCapable(dir) {
		m.errBar.text = "worktree sessions need a git repository: " + dir + " is not one"
		return
	}
	m.errBar.text = ""
	m.quick.worktree = !m.quickWorktreeOn()
	m.quick.worktreeTouched = true
}

// quickWorktreeOn is the worktree state the quick bar shows and spawns
// with: the target group's default until ctrl+t overrides it, and off
// whenever the target directory cannot host a worktree.
func (m *Model) quickWorktreeOn() bool {
	if !m.worktreeCapable(m.quickTargetDir()) {
		return false
	}
	if m.quick.worktreeTouched {
		return m.quick.worktree
	}
	return m.spawnWorktreeDefault(m.quickTargetGroup())
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
	dir, _ := resolveExistingDir(m.groupPaths[group], m.groupDefaultDir(group))
	return dir
}

// quickTool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (m *Model) quickTool() string {
	if len(m.quick.toolNames) == 0 {
		return ""
	}
	return m.quick.toolNames[m.quick.toolIndex]
}

// quickCloseAfterSend reports whether the quick bar should dismiss itself
// once a prompt is delivered. Staying open is the default; a stored "close"
// choice opts in. A store error is surfaced but still yields the default.
func (m *Model) quickCloseAfterSend() bool {
	chosen, err := m.store.Setting(quickCloseSetting)
	if err != nil {
		m.errBar.text = "reading quick prompt setting: " + err.Error()
		return false
	}
	return chosen == "close"
}

// The open list above the prompt, if any: the model list, or the typed
// effort of a CLI that lists no levels.
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
	toolName := m.quickTool()
	m.quick.choice = m.newChoice(toolName)
	return m.ensureCatalog(toolName)
}

// quickSpawning reports whether enter would spawn a new agent, which is
// the only send the model, effort and profile apply to.
func (m *Model) quickSpawning() bool {
	entry, ok := m.selectedRow()
	return ok && entry.isGroup
}

// quickChoiceHint is what a choice key says while the bar answers a
// session instead of spawning one.
// The spawn choices take control keys, which every terminal passes on where
// alt often never arrives, picked from the ones the prompt's editor, the
// manager and the common multiplexers leave free.
const (
	quickModelKey   = "ctrl+l"
	quickEffortKey  = "ctrl+x"
	quickProfileKey = "ctrl+y"
)

const quickChoiceHint = "model, effort and profile apply to a new agent: select a group to spawn one"

// requireQuickSpawn reports whether the bar would spawn, and says why a
// choice key does nothing when it would answer a session instead.
func (m *Model) requireQuickSpawn() bool {
	if m.quickSpawning() {
		return true
	}
	m.errBar.text = quickChoiceHint
	return false
}

// openQuickPick opens the list a key asked for, or says why the CLI has
// none.
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
	ch.sugg = modelSuggest{open: true}
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

// stepQuickEffort steps the effort through the model's levels, or opens
// the typed field for a CLI that lists none.
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

// handleQuickPickKey runs the open list: typing narrows the models, the
// arrows walk them, and enter or tab picks one; esc closes the list and
// keeps the prompt.
func (m *Model) handleQuickPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	toolName, ch := m.quickTool(), &m.quick.choice
	if m.quick.picking == pickEffort {
		switch msg.String() {
		case "esc", "enter", "tab":
			m.closeQuickPick()
			return m, nil
		}
		var cmd tea.Cmd
		ch.typedEffort, cmd = ch.typedEffort.Update(msg)
		return m, cmd
	}
	list := m.modelSuggestions(toolName, ch, ch.query())
	switch msg.String() {
	case "esc", quickModelKey:
		m.closeQuickPick()
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

// quickHitAt is the stretch of the open bar a click at (x, y) lands on.
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

// handleQuickClick does what the key for a clicked stretch does.
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
		m.toggleQuickWorktree()
	case quickClickEntry:
		toolName, ch := m.quickTool(), &m.quick.choice
		if list := m.modelSuggestions(toolName, ch, ch.query()); hit.entry < len(list) {
			m.pickModel(toolName, ch, list[hit.entry].model.Key())
		}
		m.closeQuickPick()
	}
	return nil
}

// quickLegend names the bar's keys with what each is set to now, and the
// open list's own keys while one is up.
func (m *Model) quickLegend() [][2]string {
	switch m.quick.picking {
	case pickModel:
		return [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵/tab", "choose"}, {"esc", "close list"}}
	case pickEffort:
		return [][2]string{{"type", "effort"}, {"↵", "done"}, {"esc", "close"}}
	}
	toolName, ch := m.quickTool(), &m.quick.choice
	pairs := [][2]string{{"↵", "send"}, {"↑↓", "target or caret"}, {"tab", "tool: " + toolName}}
	if _, listed := m.modelRowNote(toolName); listed && m.quickSpawning() {
		pairs = append(pairs, [2]string{quickModelKey, "model: " + cmp.Or(ch.model, "default")})
		if _, _, active := m.effortRow(toolName, ch); active {
			pairs = append(pairs, [2]string{quickEffortKey, "effort: " + cmp.Or(m.choiceEffort(toolName, ch), "default")})
		}
	}
	if _, shown := m.profileRow(toolName, ch); shown && m.quickSpawning() {
		pairs = append(pairs, [2]string{quickProfileKey, "profile: " + cmp.Or(m.choiceProfileName(toolName, ch), "default")})
	}
	if len(m.quick.toolNames) > 1 {
		pairs = append(pairs, [2]string{"shift+tab", "previous tool"})
	}
	return append(pairs, [2]string{"ctrl+t", "worktree: " + m.quickWorktreeState()}, [2]string{"esc", "close"})
}

// quickWorktreeState is the worktree toggle's word on the bar and in the
// footer.
func (m *Model) quickWorktreeState() string {
	switch {
	case !m.worktreeCapable(m.quickTargetDir()):
		return worktreeUnavailable
	case m.quickWorktreeOn():
		return "on"
	}
	return "off"
}
