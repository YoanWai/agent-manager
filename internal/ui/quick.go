package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// quickBar is the docked prompt bar: its prompt, tool and choice, the list
// open above it, its keys, clicks and view, and the send or spawn request
// enter builds. The root opens it, moves its target and runs what its
// quickRequest asks for.
type quickBar struct{ quickState }

// quickHost is what the bar reads from the root: the choice facts, the
// status bar, the selected row it answers or spawns into, and the launch
// inputs of a spawn into a group.
type quickHost interface {
	choiceHost
	clearErr()
	selectedRow() (treeRow, bool)
	spawnDefaults(group string) spawnDefaults
}

// quickRequest is root work a bar key or click asks for once the bar has
// taken its own share. At most one is set.
type quickRequest struct {
	// move steps the list cursor, which retargets the bar.
	move int
	// toggle flips the worktree choice once the target is known to host one.
	toggle bool
	// catalog names the CLI the bar moved to, whose choices are new.
	catalog string
	send    *quickSendRequest
	spawn   *spawnRequest
}

func (m *Model) openQuickMode() tea.Cmd {
	return m.openQuickModeWithReader(storeSettingWriter{st: m.services.store})
}

func (m *Model) openQuickModeWithReader(reader settingsValueReader) tea.Cmd {
	names, index := m.cachedSpawnToolSelection()
	if len(names) == 0 {
		m.reportErr("no CLIs enabled: open settings (s), then CLIs, to turn some on")
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
	m.clearErr()
	m.forgetWorktreeCapability()
	aim, _ := m.rail.Selected()
	m.quick = quickBar{quickState{
		aim:            aim,
		active:         true,
		composer:       composer{input: input, maxRows: quickBarMaxRows, gen: m.nextComposerGen()},
		toolNames:      names,
		toolIndex:      index,
		closeAfterSend: m.settings.cache.value(quickCloseSetting) == "close",
		worktree:       m.cachedSpawnWorktreeDefault(m.quickTargetGroup()),
		choice:         newChoice(m, names[index]),
	}}
	catalog := m.ensureCatalog(names[index])
	if m.settings.pending > 0 {
		return tea.Batch(m.quickWorktreeProbeCmd(false), catalog)
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadQuick, generation: uint64(m.quick.gen), extra: m.choiceSettingKeys()}, reader), catalog)
}

// applyCachedQuickDefaults takes the loaded defaults. A tool the load moved
// to starts its choices over and asks its CLI.
func (m *Model) applyCachedQuickDefaults() tea.Cmd {
	before := m.quick.tool()
	m.quick.toolNames, m.quick.toolIndex = m.cachedSpawnToolSelection()
	m.quick.closeAfterSend = m.settings.cache.value(quickCloseSetting) == "close"
	if !m.quick.worktreeTouched {
		m.quick.worktree = m.cachedSpawnWorktreeDefault(m.quickTargetGroup())
	}
	toolName := m.quick.tool()
	if toolName == before || toolName == "" {
		return nil
	}
	m.quick.choice = newChoice(m, toolName)
	return m.ensureCatalog(toolName)
}

func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if cmd, taken := m.remoteQuickKey(msg); taken {
		return m, cmd
	}
	cmd, request := m.quick.handleKey(m, msg)
	return m, tea.Batch(cmd, m.runQuickRequest(request))
}

// runQuickRequest executes what a bar key or click asked of the root.
func (m *Model) runQuickRequest(request quickRequest) tea.Cmd {
	switch {
	case request.move != 0:
		return tea.Batch(m.moveCursor(request.move), m.quickWorktreeProbeCmd(false))
	case request.toggle:
		return m.toggleQuickWorktree()
	case request.catalog != "":
		return m.ensureCatalog(request.catalog)
	case request.send != nil:
		if !m.dispatchQuickSend(*request.send) {
			return nil
		}
		m.clearErr()
		return m.nextEffectCmd()
	case request.spawn != nil:
		return m.dispatchQuickSpawn(*request.spawn, systemDirectoryPreflight{git: m.services.gitDrv})
	}
	return nil
}

// handleKey runs while the quick bar is docked in the sidebar: arrows
// keep moving the selection (the target follows the cursor) unless the
// caret has a prompt row to move to, enter submits against whatever is
// selected, and every other key is typed text.
func (q *quickBar) handleKey(h quickHost, msg tea.KeyMsg) (tea.Cmd, quickRequest) {
	msg = typedText(msg)
	if q.picking != pickNone {
		// A click can move the target off the group the list was opened for.
		if q.spawning(h) {
			return q.handlePickKey(h, msg), quickRequest{}
		}
		q.closePick()
	}
	switch msg.String() {
	case "esc":
		q.active = false
		// Reopening the bar starts a fresh prompt, so the images this one
		// was holding have nowhere left to be referenced from.
		q.release()
		return nil, quickRequest{}
	case "up":
		if cmd, stepped := q.stepRow(msg); stepped {
			return cmd, quickRequest{}
		}
		return nil, quickRequest{move: -1}
	case "down":
		if cmd, stepped := q.stepRow(msg); stepped {
			return cmd, quickRequest{}
		}
		return nil, quickRequest{move: 1}
	case "tab", "alt+m":
		return nil, q.cycleTool(h, 1)
	case "shift+tab":
		return nil, q.cycleTool(h, -1)
	case quickModelKey:
		q.openPick(h, pickModel)
		return nil, quickRequest{}
	case quickEffortKey:
		q.stepEffort(h)
		return nil, quickRequest{}
	case quickProfileKey:
		q.stepProfile(h)
		return nil, quickRequest{}
	case "ctrl+t", "alt+w":
		return nil, quickRequest{toggle: true}
	case "enter":
		return nil, q.submit(h)
	}
	if cmd, handled := q.handleChipKey(h, composerQuick, msg); handled {
		return cmd, quickRequest{}
	}
	return q.typeKey(msg), quickRequest{}
}

// submit answers the selected session, or spawns a new session with the
// prompt embedded when a group is selected. The bar stays active by
// default so consecutive prompts flow without re-arming; the "after quick
// send" setting closes it instead.
func (q *quickBar) submit(h quickHost) quickRequest {
	entry, ok := h.selectedRow()
	if !ok {
		h.reportErr("nothing selected")
		return quickRequest{}
	}
	if q.pasting() {
		h.reportErr("still reading the pasted image - try again in a moment")
		return quickRequest{}
	}
	text := q.message()
	if text == "" {
		h.reportErr("prompt cannot be empty")
		return quickRequest{}
	}
	if entry.isGroup {
		spawn, ok := q.spawnRequest(h, entry.group, text)
		if !ok {
			return quickRequest{}
		}
		return quickRequest{spawn: &spawn}
	}
	return quickRequest{send: &quickSendRequest{
		session:        entry.sess,
		composerGen:    q.gen,
		draft:          q.input.Value(),
		text:           text,
		closeAfterSend: q.closeAfterSend,
		images:         q.attachments,
	}}
}

func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	return m, m.runQuickRequest(m.quick.submit(m))
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	return m.quickSpawnWithReader(group, prompt, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) quickSpawnWithReader(group, prompt string, reader directoryPreflight) (tea.Model, tea.Cmd) {
	request, ok := m.quick.spawnRequest(m, group, prompt)
	if !ok {
		return m, nil
	}
	return m, m.dispatchQuickSpawn(request, reader)
}

func (m *Model) dispatchQuickSpawn(request spawnRequest, reader directoryPreflight) tea.Cmd {
	request.dirReader = reader
	m.clearErr()
	m.dispatchSpawn(request)
	return m.nextEffectCmd()
}

// spawnRequest validates a spawn of prompt into group and builds it; the
// root checks the directory on the effect lane.
func (q *quickBar) spawnRequest(h quickHost, group, prompt string) (spawnRequest, bool) {
	if strings.HasPrefix(prompt, "-") {
		h.reportErr(`prompt cannot start with "-": the tool would read it as a flag`)
		return spawnRequest{}, false
	}
	toolName := q.tool()
	if toolName == "" {
		h.reportErr("no tools configured")
		return spawnRequest{}, false
	}
	picked, err := q.choice.launch(h, toolName, "")
	if err != nil {
		h.reportErr(err.Error())
		return spawnRequest{}, false
	}
	name := toolName + "-" + newID()[:4]
	defaults := h.spawnDefaults(group)
	pickWorktree := defaults.worktree
	if q.worktreeTouched {
		pickWorktree = q.worktree
	}
	return spawnRequest{
		kind:         spawnQuick,
		toolName:     toolName,
		name:         name,
		group:        group,
		prompt:       prompt,
		autoNamed:    true,
		pickWorktree: pickWorktree,
		choice:       picked,
		base:         defaults.base,
		pane:         defaults.pane,
		composerGen:  q.gen,
		images:       q.attachments,
		draft:        q.input.Value(),
		rawDir:       defaults.groupDir,
		dirFallbacks: defaults.fallbacks,
		wantWorktree: pickWorktree,
	}, true
}

// clearAfterSend empties the bar for the next prompt, and dismisses it
// entirely when the settings toggle asks for that.
func (q *quickBar) clearAfterSend() {
	q.input.SetValue("")
	q.attachments = nil
	if q.closeAfterSend {
		q.active = false
	}
}

// toggleQuickWorktree flips the worktree choice, or probes the target first
// when its repo verdict is not cached.
func (m *Model) toggleQuickWorktree() tea.Cmd {
	if _, remote := m.remoteTarget(); remote {
		m.quick.setWorktree(!m.quick.worktreeTouched || !m.quick.worktree)
		return nil
	}
	dir := m.quickTargetDir()
	capable, known := m.cachedWorktreeCapability(dir)
	if !known {
		return m.quickWorktreeProbeCmd(true)
	}
	if !capable {
		m.reportErr("worktree sessions need a git repository: " + dir + " is not one")
		return nil
	}
	m.clearErr()
	m.quick.setWorktree(!m.quick.worktree)
	return nil
}

// setWorktree is an explicit worktree choice for this run, which the target
// group's default and later settings refreshes leave be.
func (q *quickBar) setWorktree(on bool) {
	q.worktree = on
	q.worktreeTouched = true
	q.defaultsTouched = true
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

func (m *Model) quickTool() string { return m.quick.tool() }

// tool is the spawn CLI for the current quick-mode run: the settings
// default until tab cycles it.
func (q *quickBar) tool() string {
	if len(q.toolNames) == 0 {
		return ""
	}
	return q.toolNames[q.toolIndex]
}

// The list open above the prompt, if any.
const (
	pickNone = iota
	pickModel
	pickEffort
)

// cycleTool steps the spawn CLI by delta and starts its choices over; the
// root asks the new CLI what it offers.
func (q *quickBar) cycleTool(h choiceHost, delta int) quickRequest {
	count := len(q.toolNames)
	if count == 0 {
		return quickRequest{}
	}
	q.toolIndex = (q.toolIndex + delta + count) % count
	q.defaultsTouched = true
	toolName := q.tool()
	q.choice = newChoice(h, toolName)
	return quickRequest{catalog: toolName}
}

func (m *Model) quickSpawning() bool { return m.quick.spawning(m) }

// Choices apply to a spawn only, never to an answer.
func (q *quickBar) spawning(h quickHost) bool {
	entry, ok := h.selectedRow()
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

func (q *quickBar) requireSpawn(h quickHost) bool {
	if q.spawning(h) {
		return true
	}
	h.reportErr(quickChoiceHint)
	return false
}

func (q *quickBar) openPick(h quickHost, pick int) {
	if !q.requireSpawn(h) {
		return
	}
	toolName, ch := q.tool(), &q.choice
	if note, listed := modelRowNote(h, toolName); !listed {
		h.reportErr("model: " + ansi.Strip(note))
		return
	}
	h.clearErr()
	q.picking = pick
	switch pick {
	case pickModel:
		ch.filter.SetValue("")
		ch.filter.Focus()
		ch.openModelList(h, toolName)
	case pickEffort:
		ch.typedEffort.Focus()
	}
	q.input.Blur()
}

func (q *quickBar) closePick() {
	q.picking = pickNone
	q.choice.filter.Blur()
	q.choice.typedEffort.Blur()
	q.choice.sugg = modelSuggest{}
	q.input.Focus()
}

// stepEffort opens the typed field for a CLI that lists no levels.
func (q *quickBar) stepEffort(h quickHost) {
	if !q.requireSpawn(h) {
		return
	}
	toolName, ch := q.tool(), &q.choice
	if ch.effortTyped(h, toolName) {
		q.openPick(h, pickEffort)
		return
	}
	if value, shown, active := ch.effortRow(h, toolName); !active {
		if !shown {
			value = "no levels for this model"
		}
		h.reportErr("effort: " + ansi.Strip(value))
		return
	}
	h.clearErr()
	ch.cycleEffort(h, toolName, 1)
}

func (q *quickBar) stepProfile(h quickHost) {
	if !q.requireSpawn(h) {
		return
	}
	toolName := q.tool()
	if _, shown := q.choice.profileRow(h, toolName); !shown {
		h.reportErr("profile: " + toolName + " has none")
		return
	}
	h.clearErr()
	q.choice.cycleProfile(h, toolName, 1)
}

func (q *quickBar) handlePickKey(h quickHost, msg tea.KeyMsg) tea.Cmd {
	toolName, ch := q.tool(), &q.choice
	if q.picking == pickEffort {
		switch msg.String() {
		case "esc", "enter", "tab":
			q.closePick()
			return nil
		}
		var cmd tea.Cmd
		before := ch.typedEffort.Value()
		ch.typedEffort, cmd = ch.typedEffort.Update(msg)
		if ch.typedEffort.Value() != before {
			ch.keep(h, toolName)
		}
		return cmd
	}
	list := ch.suggestions(h, toolName, ch.query())
	switch msg.String() {
	case "esc", quickModelKey:
		q.closePick()
		return nil
	case quickEffortKey:
		q.stepEffort(h)
		return nil
	case quickProfileKey:
		q.stepProfile(h)
		return nil
	case "enter", "tab":
		if len(list) > 0 {
			ch.pickModel(h, toolName, list[ch.sugg.index].model.Key())
		}
		q.closePick()
		return nil
	case "up", "down":
		delta := 1
		if msg.String() == "up" {
			delta = -1
		}
		ch.sugg.move(len(list), delta)
		return nil
	}
	var cmd tea.Cmd
	ch.filter, cmd = ch.filter.Update(msg)
	ch.filtering = true
	ch.sugg = modelSuggest{open: true}
	return cmd
}

func (q *quickBar) hitAt(x, y int) (quickHit, bool) {
	if !q.active {
		return quickHit{}, false
	}
	line, col := y-q.originY, x-q.originX
	for _, hit := range q.hits {
		if hit.line == line && col >= hit.x0 && col < hit.x1 {
			return hit, true
		}
	}
	return quickHit{}, false
}

func (m *Model) handleQuickClick(hit quickHit) tea.Cmd {
	return m.runQuickRequest(m.quick.click(m, hit))
}

func (q *quickBar) click(h quickHost, hit quickHit) quickRequest {
	switch hit.action {
	case quickClickTool:
		q.closePick()
		return q.cycleTool(h, 1)
	case quickClickModel:
		if q.picking == pickModel {
			q.closePick()
		} else {
			q.openPick(h, pickModel)
		}
	case quickClickEffort:
		q.stepEffort(h)
	case quickClickProfile:
		q.stepProfile(h)
	case quickClickWorktree:
		return quickRequest{toggle: true}
	case quickClickEntry:
		toolName, ch := q.tool(), &q.choice
		if list := ch.suggestions(h, toolName, ch.query()); hit.entry < len(list) {
			ch.pickModel(h, toolName, list[hit.entry].model.Key())
		}
		q.closePick()
	}
	return quickRequest{}
}

func (q *quickBar) legend(h quickHost) [][2]string {
	toolName, ch := q.tool(), &q.choice
	_, _, effortActive := ch.effortRow(h, toolName)
	_, hasProfiles := ch.profileRow(h, toolName)
	switch q.picking {
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
	if len(q.toolNames) > 1 {
		pairs = append(pairs, [2]string{"shift+tab", "previous tool"})
	}
	if q.spawning(h) {
		if _, listed := modelRowNote(h, toolName); listed {
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
