package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	"github.com/YoanWai/agent-manager/internal/config"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// captureClipboardImage is the seam the quick bar uses to save a pasted
// image to a temp file; tests swap it for a fake.
var captureClipboardImage = clipboard.SaveImage

// quickBar is the quick bar (internal/ui/quick) with what the root holds
// for it: whether it is open, its prompt box, the after-send setting, and
// where the frame painted it. A pasted image lands at the caret as an
// "[Image #N]" token that renders as a chip and steps, deletes, and wraps
// as one unit; on submit each token becomes its path.
type quickBar struct {
	uiquick.Bar
	composer
	active         bool
	closeAfterSend bool
	// aim is the row the user last pointed the bar at. A refresh that
	// drops a connection's row moves the cursor on its own, and enter must
	// not follow it to another agent or host.
	aim uirail.Selection
	// originX and originY place the bar's first painted line.
	originX, originY int
}

type quickHit = uiquick.Hit

const (
	quickBarMaxRows = uiquick.BarMaxRows
	quickModelKey   = uiquick.ModelKey
	quickEffortKey  = uiquick.EffortKey
	quickProfileKey = uiquick.ProfileKey
)

// quickBarHost is the root as the quick bar sees it.
type quickBarHost struct{ spawnHost }

func (m *Model) quickBarHost() quickBarHost { return quickBarHost{spawnHost{m: m}} }

func (h quickBarHost) Prompt() spawn.Prompt {
	return promptBox{c: &h.m.quick.composer, target: composerQuick, host: h.m}
}

func (h quickBarHost) Target() (uiquick.Target, bool) {
	if row, ok := h.m.remoteQuickTarget(); ok {
		return uiquick.Target{Group: row.Group, IsGroup: row.Kind != uiconnections.SessionRow}, true
	}
	entry, ok := h.m.selectedRow()
	if !ok {
		return uiquick.Target{}, false
	}
	return uiquick.Target{Group: entry.group, IsGroup: entry.isGroup}, true
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
			return keyStyle().Render(m.glyphs().PromptGlyph + " ")
		}
		return "  "
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	holdOpen(&input)
	input.Focus()
	m.clearErr()
	m.forgetWorktreeCapability()
	aim, _ := m.rail.Selected()
	m.quick = quickBar{
		aim:            aim,
		active:         true,
		composer:       composer{input: input, maxRows: quickBarMaxRows, gen: m.nextComposerGen()},
		closeAfterSend: m.settings.cache.value(uisettings.QuickCloseSetting) == "close",
	}
	// The bar is active before its choice starts, so a bar aimed at a
	// connection starts on the host's defaults.
	m.quick.Bar = uiquick.NewBar(m.spawnHost(), names, index, m.cachedSpawnWorktreeDefault(m.quickTargetGroup()))
	catalog := m.ensureCatalog(names[index])
	if m.settings.pending > 0 {
		return tea.Batch(m.quickWorktreeProbeCmd(false), catalog)
	}
	return tea.Batch(settingsLoadCmd(settingsLoadRequest{target: settingsLoadQuick, generation: uint64(m.quick.gen), extra: m.choiceSettingKeys()}, reader), catalog)
}

// applyCachedQuickDefaults takes the loaded defaults. A tool the load moved
// to starts its choices over and asks its CLI.
func (m *Model) applyCachedQuickDefaults() tea.Cmd {
	before := m.quick.Tool()
	m.quick.SetTools(m.cachedSpawnToolSelection())
	m.quick.closeAfterSend = m.settings.cache.value(uisettings.QuickCloseSetting) == "close"
	m.quick.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.quickTargetGroup()))
	toolName := m.quick.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	m.quick.RestartChoice(m.spawnHost())
	return m.ensureCatalog(toolName)
}

func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.quick.HandleKey(m.quickBarHost(), typedText(msg))
	return m, tea.Batch(cmd, m.runQuickRequest(request))
}

// runQuickRequest executes what a bar key or click asked of the root.
func (m *Model) runQuickRequest(request uiquick.Request) tea.Cmd {
	switch {
	case request.Move != 0:
		return tea.Batch(m.moveCursor(request.Move), m.quickWorktreeProbeCmd(false))
	case request.Toggle:
		return m.toggleQuickWorktree()
	case request.Catalog != "":
		return m.ensureCatalog(request.Catalog)
	case request.Close:
		m.quick.active = false
	case request.Send:
		if cmd, taken := m.remoteQuickSend(request.Text); taken {
			return cmd
		}
		entry, _ := m.selectedRow()
		if !m.dispatchQuickSend(quickSendRequest{
			session:        entry.sess,
			composerGen:    m.quick.gen,
			draft:          m.quick.input.Value(),
			text:           request.Text,
			closeAfterSend: m.quick.closeAfterSend,
			images:         m.quick.attachments,
		}) {
			return nil
		}
		m.clearErr()
		return m.nextEffectCmd()
	case request.Spawn != nil:
		if cmd, taken := m.remoteQuickSpawn(*request.Spawn); taken {
			return cmd
		}
		return m.dispatchQuickSpawn(m.quickSpawnRequest(*request.Spawn), systemDirectoryPreflight{git: m.services.gitDrv})
	case request.LoadSaved:
		return savedPromptsLoadCmd(m.services.store, savedForQuick, uint64(m.quick.gen), 0)
	case request.InsertSaved != nil:
		m.quick.insertSaved(*request.InsertSaved)
	}
	return nil
}

func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	return m, m.runQuickRequest(m.quick.Submit(m.quickBarHost()))
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	return m.quickSpawnWithReader(group, prompt, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) quickSpawnWithReader(group, prompt string, reader directoryPreflight) (tea.Model, tea.Cmd) {
	request, ok := m.quick.SpawnRequest(m.spawnHost(), group, prompt)
	if !ok {
		return m, nil
	}
	return m, m.dispatchQuickSpawn(m.quickSpawnRequest(request), reader)
}

func (m *Model) dispatchQuickSpawn(request spawnRequest, reader directoryPreflight) tea.Cmd {
	request.dirReader = reader
	m.clearErr()
	m.dispatchSpawn(request)
	return m.nextEffectCmd()
}

// quickSpawnRequest names the session and captures the group's launch
// inputs, the worktree default it keeps unless toggled, and the prompt's
// images.
func (m *Model) quickSpawnRequest(request spawn.Request) spawnRequest {
	name := request.Tool + "-" + newID()[:4]
	defaults := m.spawnDefaults(request.Group)
	pickWorktree := defaults.worktree
	if request.WorktreeSet {
		pickWorktree = request.Worktree
	}
	return spawnRequest{
		kind:         spawnQuick,
		toolName:     request.Tool,
		name:         name,
		group:        request.Group,
		prompt:       request.Prompt,
		autoNamed:    true,
		pickWorktree: pickWorktree,
		choice:       config.Choice(request.Pick),
		base:         defaults.base,
		pane:         defaults.pane,
		composerGen:  m.quick.gen,
		images:       m.quick.attachments,
		draft:        m.quick.input.Value(),
		rawDir:       defaults.groupDir,
		dirFallbacks: defaults.fallbacks,
		wantWorktree: pickWorktree,
	}
}

// clearAfterSend empties the bar for the next prompt, and dismisses it
// entirely when the settings toggle asks for that.
func (q *quickBar) clearAfterSend() {
	q.input.SetValue("")
	q.attachments = nil
	q.saved = nil
	if q.closeAfterSend {
		q.active = false
	}
}

// toggleQuickWorktree flips the worktree choice, or probes the target first
// when its repo verdict is not cached.
func (m *Model) toggleQuickWorktree() tea.Cmd {
	if _, remote := m.remoteTarget(); remote {
		m.quick.SetWorktree(!m.quick.WorktreeTouched() || !m.quick.Worktree())
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
	m.quick.SetWorktree(!m.quick.Worktree())
	return nil
}

func (m *Model) quickWorktreeOn() bool {
	if _, remote := m.remoteTarget(); remote {
		return false
	}
	capable, known := m.cachedWorktreeCapability(m.quickTargetDir())
	if !known || !capable {
		return false
	}
	if m.quick.WorktreeTouched() {
		return m.quick.Worktree()
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

func (m *Model) quickTool() string { return m.quick.Tool() }

func (m *Model) quickSpawning() bool { return m.quick.Spawning(m.quickBarHost()) }

// quickPicking reports a list open over the quick bar's prompt, which owns
// the wheel the way it owns ↑↓.
func (m *Model) quickPicking() bool {
	return m.quick.active && m.quick.Picking() != uiquick.PickNone
}

func (q *quickBar) hitAt(x, y int) (quickHit, bool) {
	if !q.active {
		return quickHit{}, false
	}
	return q.HitAt(x-q.originX, y-q.originY)
}

func (m *Model) handleQuickClick(hit quickHit) tea.Cmd {
	return m.runQuickRequest(m.quick.Click(m.quickBarHost(), hit))
}

const quickModeTitle = "Quick prompt mode"

func (m *Model) quickFooter() legend {
	edge := keyStyle().Render(quickEdge)
	gutter := strings.Repeat(" ", quickGutter-1)
	painted := paintLegend([]legendSection{{title: quickModeTitle, pairs: m.quick.Legend(m.quickBarHost())}}, max(m.layout.width-1, 1))
	keys := splitLines(painted.text)
	rows := quickBarMaxRows + 1
	if m.quick.Picking() != uiquick.PickNone {
		rows = max(m.layout.height/2, rows)
	}
	const minBody = 3
	room := m.layout.height - m.listChromeRows() - 1 - minBody - len(keys)
	rows = max(min(rows, room), 2)
	var lines []string
	for i, line := range splitLines(m.viewQuickBar(max(m.layout.width-2*quickGutter, 1), rows)) {
		tone := quickModeHex()
		if i > 0 && m.quick.Picking() == uiquick.PickNone {
			tone = blockHex()
		}
		lines = append(lines, paint(edge+gutter+line, m.layout.width, tone))
	}
	hits := painted.hits.moved(lipgloss.Width(quickEdge), len(lines))
	for _, line := range keys {
		lines = append(lines, paint(edge+line, m.layout.width, quickModeHex()))
	}
	m.quick.originX = quickGutter
	return legend{text: strings.Join(lines, "\n"), hits: hits}
}

const (
	quickEdge   = "▌"
	quickGutter = 3
)

func (m *Model) viewQuickBar(width, maxRows int) string {
	facts := uiquick.Facts{WorktreeOn: m.quickWorktreeOn(), LabelWidth: detailLabelWidth, Styles: m.spawnStyles()}
	facts.WorktreeCapable, facts.WorktreeKnown = m.cachedWorktreeCapability(m.quickTargetDir())
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			facts.Group = displayGroup(entry.group)
		} else {
			facts.Answer = m.quickAnswerRow(entry, m.displayName(entry.sess), width)
		}
	}
	m.remoteQuickFacts(&facts)
	return m.quick.View(m.quickBarHost(), facts, width, maxRows)
}

// quickAnswerRow names the session an answer goes to, with its state and
// tool where the width has room.
func (m *Model) quickAnswerRow(entry treeRow, name string, width int) string {
	label := labelStyle().Render(padRight("answer", detailLabelWidth)) + subtleStyle().Render("│ ")
	sess := entry.sess
	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(m.glyphs().StatusGlyph(sess.Status) + " " + statusLabel(sess.Status))
	return fitColumns(
		[]string{label + lipgloss.NewStyle().Foreground(colorBright()).Bold(true).Render(name)},
		[]string{state + " " + chipStyle().Render(sess.Tool), state, ""}, width)
}
