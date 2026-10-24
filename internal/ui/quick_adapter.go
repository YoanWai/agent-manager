package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiconnections "github.com/YoanWai/agent-manager/internal/ui/connections"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uisettings "github.com/YoanWai/agent-manager/internal/ui/settings"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	"github.com/YoanWai/agent-manager/internal/ui/spawn"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) quickBarHost() quickBarHost { return quickBarHost{spawnHost{m: m}} }

// quickBarHost is the root as the quick bar sees it.
type quickBarHost struct{ spawnHost }

func (h quickBarHost) Prompt() spawn.Prompt {
	return promptBox{c: &h.m.quick().Composer, target: composer.Quick, host: h.m}
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
	return m.openQuickModeWithReader(uipersist.StoreWriter{Store: m.services.store})
}

func (m *Model) openQuickModeWithReader(reader uipersist.Reader) tea.Cmd {
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
			return uitheme.KeyStyle().Render(m.glyphs().PromptGlyph + " ")
		}
		return "  "
	})
	input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	composer.HoldOpen(&input)
	input.Focus()
	m.clearErr()
	m.forgetWorktreeCapability()
	aim, _ := m.rail.Selected()
	q := newQuickSurface(m)
	q.Composer = composer.Composer{Input: input, MaxRows: uiquick.BarMaxRows, Gen: m.nextComposerGen()}
	q.CloseAfterSend = m.settings.Cache.Value(uisettings.QuickCloseSetting) == "close"
	q.Aim = quickAim(aim)
	// The bar is on the stack before its choice starts, so a bar aimed at
	// a connection starts on the host's defaults.
	m.push(q)
	q.Bar = uiquick.NewBar(m.spawnHost(), names, index, m.cachedSpawnWorktreeDefault(m.quickTargetGroup()))
	catalog := m.ensureCatalog(names[index])
	if m.settings.Pending > 0 {
		return tea.Batch(m.quickWorktreeProbeCmd(false), catalog)
	}
	return tea.Batch(uipersist.LoadCmd(uipersist.LoadRequest{Target: uipersist.QuickTarget, Generation: uint64(q.Gen), Extra: m.choiceSettingKeys()}, reader), catalog)
}

// applyCachedQuickDefaults takes the loaded defaults. A tool the load moved
// to starts its choices over and asks its CLI.
func (m *Model) applyCachedQuickDefaults() tea.Cmd {
	q := m.quick()
	before := q.Tool()
	q.SetTools(m.cachedSpawnToolSelection())
	q.CloseAfterSend = m.settings.Cache.Value(uisettings.QuickCloseSetting) == "close"
	q.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.quickTargetGroup()))
	toolName := q.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	q.RestartChoice(m.spawnHost())
	return m.ensureCatalog(toolName)
}

func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	cmd, request := m.quick().HandleKey(m.quickBarHost(), composer.TypedText(msg))
	return m, tea.Batch(cmd, m.runQuickRequest(request))
}

// quickKey types a key into the open bar. A row menu opened on the rail
// under it by the mouse keeps the keys until it closes.
func (m *Model) quickKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if m.rail.Busy() {
		return nil, false
	}
	m.gens.foreground++
	_, cmd := m.handleQuickKey(msg)
	return cmd, true
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
		m.closeQuick()
	case request.Send:
		if cmd, taken := m.remoteQuickSend(request.Text); taken {
			return cmd
		}
		entry, _ := m.selectedRow()
		q := m.quick()
		if !m.dispatchQuickSend(uiquick.SendRequest{
			Session:        entry.sess,
			ComposerGen:    q.Gen,
			Draft:          q.Input.Value(),
			Text:           request.Text,
			CloseAfterSend: q.CloseAfterSend,
			Images:         q.Attachments,
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
		return uipersist.SavedCmd(m.services.store, uipersist.SavedForQuick, uint64(m.quick().Gen), 0)
	case request.InsertSaved != nil:
		m.quick().InsertSaved(*request.InsertSaved)
	}
	return nil
}

func (m *Model) submitQuick() (tea.Model, tea.Cmd) {
	return m, m.runQuickRequest(m.quick().Submit(m.quickBarHost()))
}

func (m *Model) quickSpawn(group, prompt string) (tea.Model, tea.Cmd) {
	return m.quickSpawnWithReader(group, prompt, systemDirectoryPreflight{git: m.services.gitDrv})
}

func (m *Model) quickSpawnWithReader(group, prompt string, reader directoryPreflight) (tea.Model, tea.Cmd) {
	request, ok := m.quick().SpawnRequest(m.spawnHost(), group, prompt)
	if !ok {
		return m, nil
	}
	return m, m.dispatchQuickSpawn(m.quickSpawnRequest(request), reader)
}

func (m *Model) dispatchQuickSpawn(request uilaunch.Request, reader directoryPreflight) tea.Cmd {
	request.DirReader = crossPreflight(reader)
	m.clearErr()
	m.dispatchSpawn(request)
	return m.nextEffectCmd()
}

// quickSpawnRequest names the session and captures the group's launch
// inputs, the worktree default it keeps unless toggled, and the prompt's
// images.
func (m *Model) quickSpawnRequest(request spawn.Request) uilaunch.Request {
	name := request.Tool + "-" + newID()[:4]
	defaults := m.spawnDefaults(request.Group)
	pickWorktree := defaults.worktree
	if request.WorktreeSet {
		pickWorktree = request.Worktree
	}
	q := m.quick()
	return uilaunch.Request{
		Kind:         uilaunch.Quick,
		ToolName:     request.Tool,
		Name:         name,
		Group:        request.Group,
		Prompt:       request.Prompt,
		AutoNamed:    true,
		PickWorktree: pickWorktree,
		Choice:       config.Choice(request.Pick),
		Base:         defaults.base,
		Pane:         defaults.pane,
		ComposerGen:  q.Gen,
		Images:       q.Attachments,
		Draft:        q.Input.Value(),
		RawDir:       defaults.groupDir,
		DirFallbacks: defaults.fallbacks,
		WantWorktree: pickWorktree,
	}
}

// toggleQuickWorktree flips the worktree choice, or probes the target first
// when its repo verdict is not cached.
func (m *Model) toggleQuickWorktree() tea.Cmd {
	q := m.quick()
	if _, remote := m.remoteTarget(); remote {
		q.SetWorktree(!q.WorktreeTouched() || !q.Worktree())
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
	q.SetWorktree(!q.Worktree())
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
	if q := m.quick(); q.WorktreeTouched() {
		return q.Worktree()
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

func (m *Model) quickTool() string { return m.quick().Tool() }

func (m *Model) quickSpawning() bool { return m.quick().Spawning(m.quickBarHost()) }

// quickPicking reports a list open over the quick bar's prompt, which owns
// the wheel the way it owns ↑↓.
func (m *Model) quickPicking() bool {
	q := m.quick()
	return q != nil && q.Picking() != uiquick.PickNone
}

func (m *Model) quickHitAt(x, y int) (uiquick.Hit, bool) {
	q := m.quick()
	if q == nil {
		return uiquick.Hit{}, false
	}
	return q.HitAt(x-q.OriginX, y-q.OriginY)
}

func (m *Model) handleQuickClick(hit uiquick.Hit) tea.Cmd {
	return m.runQuickRequest(m.quick().Bar.Click(m.quickBarHost(), hit))
}

const quickModeTitle = "Quick prompt mode"

func (m *Model) quickFooter(q *uiquick.Surface) legend {
	edge := uitheme.KeyStyle().Render(quickEdge)
	gutter := strings.Repeat(" ", quickGutter-1)
	painted := paintLegend([]legendSection{{title: quickModeTitle, pairs: q.Legend(m.quickBarHost())}}, max(m.layout.width-1, 1))
	keys := uitheme.SplitLines(painted.text)
	rows := uiquick.BarMaxRows + 1
	if q.Picking() != uiquick.PickNone {
		rows = max(m.layout.height/2, rows)
	}
	const minBody = 3
	room := m.layout.height - m.listChromeRows() - 1 - minBody - len(keys)
	rows = max(min(rows, room), 2)
	var lines []string
	for i, line := range uitheme.SplitLines(m.viewQuickBar(max(m.layout.width-2*quickGutter, 1), rows)) {
		tone := uitheme.QuickModeHex()
		if i > 0 && q.Picking() == uiquick.PickNone {
			tone = uitheme.BlockHex()
		}
		lines = append(lines, uitheme.Paint(edge+gutter+line, m.layout.width, tone))
	}
	hits := painted.hits.moved(lipgloss.Width(quickEdge), len(lines))
	for _, line := range keys {
		lines = append(lines, uitheme.Paint(edge+line, m.layout.width, uitheme.QuickModeHex()))
	}
	q.OriginX = quickGutter
	return legend{text: strings.Join(lines, "\n"), hits: hits}
}

const (
	quickEdge   = "▌"
	quickGutter = 3
)

func (m *Model) viewQuickBar(width, maxRows int) string {
	facts := uiquick.Facts{WorktreeOn: m.quickWorktreeOn(), LabelWidth: uipreview.DetailLabelWidth, Styles: m.spawnStyles()}
	facts.WorktreeCapable, facts.WorktreeKnown = m.cachedWorktreeCapability(m.quickTargetDir())
	if entry, ok := m.selectedRow(); ok {
		if entry.isGroup {
			facts.Group = uiframe.DisplayGroup(entry.group)
		} else {
			facts.Answer = m.quickAnswerRow(entry, m.displayName(entry.sess), width)
		}
	}
	m.remoteQuickFacts(&facts)
	return m.quick().Bar.View(m.quickBarHost(), facts, width, maxRows)
}

// quickAnswerRow names the session an answer goes to, with its state and
// tool where the width has room.
func (m *Model) quickAnswerRow(entry treeRow, name string, width int) string {
	label := uitheme.LabelStyle().Render(padRight("answer", uipreview.DetailLabelWidth)) + uitheme.SubtleStyle().Render("│ ")
	sess := entry.sess
	state := lipgloss.NewStyle().Foreground(statusColor(sess.Status)).
		Render(m.glyphs().StatusGlyph(sess.Status) + " " + statusLabel(sess.Status))
	return uipreview.FitColumns(
		[]string{label + lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render(name)},
		[]string{state + " " + uitheme.ChipStyle().Render(sess.Tool), state, ""}, width)
}
