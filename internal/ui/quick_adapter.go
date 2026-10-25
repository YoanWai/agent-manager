package ui

import (
	"slices"
	"strings"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uieffects "github.com/YoanWai/agent-manager/internal/ui/effects"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uiinstall "github.com/YoanWai/agent-manager/internal/ui/launchhint/install"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uiquicksend "github.com/YoanWai/agent-manager/internal/ui/quick/send"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uipersist "github.com/YoanWai/agent-manager/internal/ui/settings/persist"
	uilaunch "github.com/YoanWai/agent-manager/internal/ui/spawn/launch"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	q.CloseAfterSend = m.closeAfterSend()
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
	q.CloseAfterSend = m.closeAfterSend()
	q.DefaultWorktree(m.cachedSpawnWorktreeDefault(m.quickTargetGroup()))
	toolName := q.Tool()
	if toolName == before || toolName == "" {
		return nil
	}
	q.RestartChoice(m.spawnHost())
	return m.ensureCatalog(toolName)
}

func (m *Model) handleQuickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	q := m.quick()
	// The move and toggle effects change the bar's own aim and toggle, so
	// they land before a later repaint reads them.
	cmd := uiquick.Decide(m.quickBarHost(), q, composer.TypedText(msg))
	return m, cmd
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

// runQuickRequest runs what a bar key or click asked of the root.
func (m *Model) runQuickRequest(request uiquick.Request) tea.Cmd {
	return uiquick.Execute(m.quickBarHost(), m.quick(), request, nil)
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
	painted := paintLegend([]legendSection{{Title: quickModeTitle, Pairs: q.Legend(m.quickBarHost())}}, max(m.layout.width-1, 1), m.prefs.glyphMode)
	keys := uitheme.SplitLines(painted.Text)
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
	hits := painted.Hits.Moved(lipgloss.Width(quickEdge), len(lines))
	for _, line := range keys {
		lines = append(lines, uitheme.Paint(edge+line, m.layout.width, uitheme.QuickModeHex()))
	}
	q.OriginX = quickGutter
	return legend{Text: strings.Join(lines, "\n"), Hits: hits}
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

// quickSendRoot is the root as a finished send reaches it.
type quickSendRoot struct{ m *Model }

func (r quickSendRoot) ClearAccepted(composerGen int, draft string, closeAfterSend bool) {
	r.m.clearAccepted(composerGen, draft, closeAfterSend)
}

func (r quickSendRoot) RestoreImages(request uiquicksend.SendRequest) tea.Cmd {
	return r.m.returnQuickSendImages(request)
}

func (r quickSendRoot) ReportErr(text string) { r.m.reportErr(text) }

func (r quickSendRoot) ClearErr() { r.m.clearErr() }

func (r quickSendRoot) MarkSent(session store.Session, text string) {
	for i := range r.m.workspace.sessions {
		if uiquicksend.SameTarget(r.m.workspace.sessions[i], session) {
			r.m.workspace.sessions[i].Acked = false
			r.m.workspace.sessions[i].LastPrompt = text
		}
	}
}

var _ uiquicksend.Root = quickSendRoot{}

func (m *Model) dispatchQuickSend(request uiquicksend.SendRequest) bool {
	jobs := append([]*uieffects.Job{m.effects.Main.Active}, m.effects.Main.Pending...)
	for _, job := range jobs {
		if job == nil {
			continue
		}
		pending, ok := job.Request.(uiquicksend.SendRequest)
		if ok && pending.ComposerGen == request.ComposerGen {
			m.reportErr("this prompt is already being sent")
			return false
		}
	}
	request.Images = slices.Clone(request.Images)
	m.quick().Attachments = nil
	m.enqueueEffect(request, 0, false)
	return true
}

// clearAccepted empties the bar a confirmed send came from, unless its
// prompt has changed since, and closes it when the setting asks.
func (m *Model) clearAccepted(composerGen int, draft string, closeAfterSend bool) {
	if !m.quickHolds(composerGen, draft) {
		return
	}
	q := m.quick()
	q.Input.SetValue("")
	q.Saved = nil
	if closeAfterSend {
		m.closeQuick()
	}
}

func (m *Model) returnQuickSendImages(request uiquicksend.SendRequest) tea.Cmd {
	if q, ok := m.quickFor(request.ComposerGen); ok {
		q.Attachments = append(request.Images, q.Attachments...)
		return nil
	}
	if len(request.Images) == 0 {
		return nil
	}
	images := slices.Clone(request.Images)
	return func() tea.Msg {
		uiinstall.DropImages(images)
		return nil
	}
}

// quickServices is the effect services as a quick send reaches them.
func (s effectServices) quickServices() uiquicksend.Services {
	exists, sendText := s.quickSessionExists, s.quickSendText
	if exists == nil {
		exists = func(driver *tmux.Driver, id string) (bool, error) { return driver.SessionExists(id) }
	}
	if sendText == nil {
		sendText = func(driver *tmux.Driver, id, text string) (tmux.SendResult, error) {
			return driver.SendTextResult(id, text)
		}
	}
	return uiquicksend.Services{Store: s.store, Config: s.cfg, Driver: s.driver, SessionExists: exists, SendText: sendText}
}

// quickRoot is the root as the bar's surface reaches it: the key the bar
// types into the list, and the mouse the list keeps under it.
type quickRoot struct{ *Model }

func (r quickRoot) Key(msg tea.KeyMsg) (tea.Cmd, bool) { return r.quickKey(msg) }

func (r quickRoot) Mouse(msg tea.MouseMsg) tea.Cmd {
	_, cmd := r.listMouse(msg)
	return cmd
}

var _ uiquick.SurfaceRoot = quickRoot{}

// newQuickSurface builds a bar behind the root; the caller fills in its
// Bar, prompt, aim and origin before putting it on the stack.
func newQuickSurface(m *Model) *uiquick.Surface {
	return uiquick.NewSurface(quickRoot{m})
}

// quick is the open quick bar, nil when none is on the stack.
func (m *Model) quick() *uiquick.Surface {
	q, _ := surfaceOf[*uiquick.Surface](m)
	return q
}

// quickFor is the open bar when it is the one composerGen came from: a
// completion lands on the bar that asked or is dropped.
func (m *Model) quickFor(composerGen int) (*uiquick.Surface, bool) {
	q := m.quick()
	return q, q != nil && q.Gen == composerGen
}

// quickHolds is whether the bar composerGen came from still shows draft
// and no image: a send or spawn of exactly that prompt may clear it.
func (m *Model) quickHolds(composerGen int, draft string) bool {
	q, ok := m.quickFor(composerGen)
	return ok && q.Input.Value() == draft && len(q.Attachments) == 0
}

// closeQuick takes the bar off the stack wherever it sits: a send can
// complete under a card opened since.
func (m *Model) closeQuick() {
	for i := len(m.surfaces) - 1; i >= 0; i-- {
		if q, ok := m.surfaces[i].(*uiquick.Surface); ok {
			m.surfaces = append(m.surfaces[:i:i], m.surfaces[i+1:]...)
			q.Leave(m)
			return
		}
	}
}

// quickAim and railSelection convert the bar's aim: it keeps only the
// fields that tell a row it was pointed at from the one a refresh moved the
// cursor to, so quick never names the rail's row type.
func quickAim(selection uirail.Selection) uiquick.Aim {
	return uiquick.Aim{
		Kind:      uint8(selection.Kind),
		SessionID: selection.SessionID,
		Group:     selection.Group,
		Host:      selection.Host,
	}
}

func railSelection(aim uiquick.Aim) uirail.Selection {
	return uirail.Selection{
		Kind:      uirail.RowKind(aim.Kind),
		SessionID: aim.SessionID,
		Group:     aim.Group,
		Host:      aim.Host,
	}
}
