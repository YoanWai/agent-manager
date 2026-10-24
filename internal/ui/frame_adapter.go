package ui

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	uifocus "github.com/YoanWai/agent-manager/internal/ui/focus"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uigeometry "github.com/YoanWai/agent-manager/internal/ui/geometry"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uistartup "github.com/YoanWai/agent-manager/internal/ui/startup"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func (m *Model) View() string {
	if m.layout.frame == "" {
		return "loading..."
	}
	return m.layout.frame
}

func (m *Model) prepareFrame() {
	m.prepareReviewLayout()
	frame := m.renderFrame()
	// A colorless profile renders the backdrop as a bare reset, which would
	// strip the bold and reverse cells it lands beside.
	if !m.prefs.terminalBackground && lipgloss.ColorProfile() != termenv.Ascii {
		frame = uitheme.FillBackdrop(frame, m.layout.width, uitheme.Current().Bg, uitheme.Current().Text)
	}
	m.layout.frame = frame
}

func (m *Model) renderFrame() string {
	m.layout.keyHits, m.layout.card = nil, cardRect{}
	if m.layout.width == 0 {
		return m.syncCursorAnchor("loading...")
	}
	if m.startup.Booting {
		var lines []string
		for _, line := range uistartup.Ring(m.glyphs(), m.layout.width, m.layout.height, "loading", m.startup.LoadPhase, uitheme.Current()) {
			lines = append(lines, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
		}
		frame := m.overlayTopRight(strings.Join(lines, "\n"), m.statusToast(), 0)
		// Pinned to exactly height rows so the outer terminal cannot scroll
		// the TUI away when a layout overshoots.
		return m.syncCursorAnchor(uiframe.Clamp(frame, m.layout.height, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) }))
	}
	var frame string
	if m.base == baseReview {
		frame = m.viewDiffFull()
	} else {
		frame = m.overlayRowMenu(m.titleTopRowWithUpdate(m.viewListFrame()))
	}
	frame = m.surfaceView(frame)
	m.layout.keyHits = m.layout.keyHits.above(m.layout.height)
	return m.syncCursorAnchor(uiframe.Clamp(frame, m.layout.height, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) }))
}

// paintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func paintLegend(sections []legendSection, width int) legend {
	return rootLegend(uiframe.PaintLive(frameSections(sections), width))
}

// placeKeys records a legend's keys painted with its first row at y and
// its first column at x.
func (m *Model) placeKeys(hits keyHits, x, y int) {
	m.layout.keyHits = append(m.layout.keyHits, hits.moved(x, y)...)
}

// placePainted records keys a feature painted on row y, from column x.
func (m *Model) placePainted(keys []presentation.PaintedKey, x, y int) {
	var hits keyHits
	for _, key := range keys {
		hits = hits.add(key.X0, key.X1, 0, key.Label)
	}
	m.placeKeys(hits, x, y)
}

// The root keeps the legend's wire types under their own field names, because
// every prefix writes section and pair literals; ui/frame paints them.

// legendSection is one tier: its title, its bindings, and whether it is a
// secondary tier that recedes behind the tier above it.
type legendSection struct {
	title  string
	leads  [][2]string
	alerts [][2]string
	hint   string
	pairs  [][2]string
	quiet  bool
}

// legend is a footer as painted: its text, and where each key it names
// landed, from its own first row and column.
type legend struct {
	text string
	hits keyHits
}

// keyHit is a key pair as painted: a left press inside it presses key.
type keyHit struct {
	x0, x1, y int
	key       tea.KeyMsg
}

type keyHits []keyHit

// add records a part painted across columns [x0, x1) of row y, when its
// label names a key a click can press.
func (hits keyHits) add(x0, x1, y int, label string) keyHits {
	key, ok := uiframe.LegendKey(label)
	if !ok {
		return hits
	}
	return append(hits, keyHit{x0: x0, x1: x1, y: y, key: key})
}

func (hits keyHits) at(x, y int) (keyHit, bool) {
	for _, hit := range hits {
		if y == hit.y && x >= hit.x0 && x < hit.x1 {
			return hit, true
		}
	}
	return keyHit{}, false
}

// above keeps the hits on the rows a frame clamped to height still shows.
func (hits keyHits) above(height int) keyHits {
	kept := hits[:0]
	for _, hit := range hits {
		if hit.y < height {
			kept = append(kept, hit)
		}
	}
	return kept
}

// moved is hits painted with their first row at y and first column at x.
func (hits keyHits) moved(x, y int) keyHits {
	out := make(keyHits, len(hits))
	for i, hit := range hits {
		out[i] = keyHit{x0: hit.x0 + x, x1: hit.x1 + x, y: hit.y + y, key: hit.key}
	}
	return out
}

// pkg is a tier as the package paints it.
func (s legendSection) pkg() uiframe.Section {
	return uiframe.Section{Title: s.title, Leads: s.leads, Alerts: s.alerts, Hint: s.hint, Pairs: s.pairs, Quiet: s.quiet}
}

// frameSections copies the root's tiers into the package's, which paints them.
func frameSections(sections []legendSection) []uiframe.Section {
	out := make([]uiframe.Section, len(sections))
	for i, section := range sections {
		out[i] = section.pkg()
	}
	return out
}

// rootHits copies a box's painted keys back to the root's own type.
func rootHits(hits uiframe.KeyHits) keyHits {
	out := make(keyHits, len(hits))
	for i, hit := range hits {
		out[i] = keyHit{x0: hit.X0, x1: hit.X1, y: hit.Y, key: hit.Key}
	}
	return out
}

// rootLegend copies a painted legend back to the root's own types.
func rootLegend(painted uiframe.Legend) legend {
	return legend{text: painted.Text, hits: rootHits(painted.Hits)}
}

// viewHeaderRows is the full-width band over both columns.
func (m *Model) viewHeaderRows() []string {
	if m.headerRows() == 0 {
		return nil
	}
	header := uiframe.Header{
		Left:   m.viewBanner()[0],
		Scope:  m.headerScope(),
		Agents: m.headerAgents(),
		Rollup: []string{m.viewStatusCounts(false), m.viewStatusCounts(true)},
		Sep:    uitheme.SubtleStyle().Render("   "),
		Width:  m.layout.width,
		Gutter: uiframe.RailGutter,
	}
	if tag := m.updateTag(); tag != "" {
		header.Left += uitheme.SubtleStyle().Render("  ") + tag
	}
	return []string{header.Row()}
}

func (m *Model) updateTag() string {
	if m.release.Latest == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(uitheme.ColorAccent()).Render(m.glyphs().UpdateGlyph + " " + m.release.Latest + " available")
}

// titleTopRowWithUpdate keeps the update tag on the top edge when the header
// that would carry it is hidden.
func (m *Model) titleTopRowWithUpdate(frame string) string {
	tag := m.updateTag()
	if !m.prefs.hideHeader || tag == "" {
		return frame
	}
	title := uitheme.Paint(" "+tag+" ", ansi.StringWidth(tag)+2, uitheme.BackdropHex())
	left := m.layout.width - ansi.StringWidth(title) - uiframe.RailGutter
	if left < uiframe.RailGutter {
		return frame
	}
	rows := strings.SplitN(frame, "\n", 2)
	rows[0] = spliceAtColumn(rows[0], title, left)
	return strings.Join(rows, "\n")
}

// headerScope names what the list is showing. The count is of the same
// agents the rollup beside it breaks down, so the two lines always add up;
// counting painted rows instead would drop everything folded inside a
// collapsed group. Shells are left to their own block.
func (m *Model) headerScope() string {
	count := len(m.listedAgents())
	label := " agents"
	if count == 1 {
		label = " agent"
	}
	scope := uitheme.SubtleStyle().Render(" · active")
	if m.rail.ShowArchived() {
		scope = uitheme.SubtleStyle().Render(" · archived")
	}
	return uitheme.ValueStyle().Render(fmt.Sprintf("%d", count)) + uitheme.SubtleStyle().Render(label) + scope
}

// headerAgents is the fleet's process cost as shares of this machine,
// empty when nothing is running. RAM shows both percent and absolute size.
func (m *Model) headerAgents() string {
	if m.workspace.agents.count == 0 {
		return ""
	}
	title := lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render("agents total usage:")
	return title + " " +
		uitheme.LabelStyle().Render("cpu ") + uitheme.ValueStyle().Render(fmt.Sprintf("%.0f%%", m.workspace.agents.cpu)) +
		uitheme.SubtleStyle().Render(" · ") + uitheme.LabelStyle().Render("ram ") +
		uitheme.ValueStyle().Render(fmt.Sprintf("%.0f%%", m.workspace.agents.ram)) +
		uitheme.SubtleStyle().Render(" · ") + uitheme.ValueStyle().Render(uiframe.HumanBytes(m.workspace.agents.rss))
}

// elsewhereNote marks a session this manager does not speak for: its pane
// runs on a tmux server this one does not talk to, or no server has claimed
// it and the store belongs to another manager. Its status is the last one
// that manager wrote, and nothing here refreshes or drives it.
func (m *Model) elsewhereNote(sess store.Session) string {
	if m.workspace.tmuxSocket == "" {
		return ""
	}
	if sess.TmuxSocket == "" && !m.workspace.leadingManager {
		return " · elsewhere"
	}
	if sess.TmuxSocket == "" || sess.TmuxSocket == m.workspace.tmuxSocket {
		return ""
	}
	return " · elsewhere"
}

// viewStatusCounts is the fleet-at-a-glance strip: a tinted dot and count
// per state present among the listed agents.
func (m *Model) viewStatusCounts(compact bool) string {
	counts := map[string]int{}
	for _, sess := range m.listedAgents() {
		counts[sess.Status]++
	}
	var parts []string
	for _, st := range []string{status.Waiting, status.Working, status.Finished, status.Idle, status.Errored, status.Dead} {
		if counts[st] == 0 {
			continue
		}
		dot := lipgloss.NewStyle().Foreground(statusColor(st)).Render(m.glyphs().StatusGlyph(st))
		label := fmt.Sprintf(" %d %s", counts[st], st)
		if compact {
			label = fmt.Sprintf(" %d", counts[st])
		}
		parts = append(parts, dot+uitheme.SubtleStyle().Render(label))
	}
	return strings.Join(parts, uitheme.SubtleStyle().Render("  "))
}

// cardRect is the package's painted-card geometry, under the root's own name.
type cardRect = uiframe.Rect

func (m *Model) cardSizing(hint [][2]string) uiframe.CardSizing {
	sizing := uiframe.CardSizing{Cols: m.layout.width, Hint: hint}
	if m.errBar.text != "" {
		sizing.Status = m.statusMessage(m.glyphs().CardErrorGlyph, m.glyphs().FinishedGlyph, m.glyphs().WarningGlyph)
	}
	return sizing
}

func (m *Model) cardWidth() int { return m.cardSizing(nil).Width() }

// cardChromeRows are the rows a default-width card spends around its body:
// the title edge and the blank under it, the status, the blank above the
// hint, the hint's rule and rows, and the bottom edge.
func (m *Model) cardChromeRows(hint [][2]string) int {
	return m.cardSizing(hint).DefaultChromeRows()
}

// flexChromeRows are the rows a flex card spends around its body. A flex
// card is at least as wide as its hint on one row, so the hint wraps the
// way it would at that width whatever the body holds.
func (m *Model) flexChromeRows(hint [][2]string) int {
	return m.cardSizing(hint).FlexChromeRows()
}

// flexCardWidth picks a width that fits every content line, never under the
// default card width and never past the terminal edge.
func (m *Model) flexCardWidth(title, body string, hint [][2]string) int {
	return m.cardSizing(hint).FlexWidth(title, body)
}

// CardSized floats a modal at an explicit width, for the key map, whose
// lines are too long to read inside the default column.
func (m *Model) CardSized(width int, title, body string, hint [][2]string) string {
	box, hits := uiframe.Box(m.dialogRenderContext(), uiframe.LiveChrome(), width, title, body, hint, uitheme.Paint)
	frame := m.centerOnBackdrop(box)
	card := &m.layout.card
	card.BodyTop, card.BodyLeft, card.BodyWidth = card.Top+uiframe.CardBodyRow, card.Left+uiframe.CardBodyColumn, uiframe.CardInnerWidth(width)
	m.placeKeys(rootHits(hits), card.Left, card.Top)
	return frame
}

// centerOnBackdrop floats a block of pre-painted lines in the middle of
// the app frame, filling the rest with the backdrop. The backdrop covers
// any key painted before it, so those keys stop answering clicks.
func (m *Model) centerOnBackdrop(box []string) string {
	ctx := m.dialogRenderContext()
	top, left, right := uiframe.Placement(ctx, box)
	m.layout.card = cardRect{Top: top, Left: left, Right: right, Bottom: top + len(box)}
	m.layout.keyHits = nil
	return uiframe.Centered(ctx, box, uitheme.BackdropHex(), uitheme.Paint)
}

func (m *Model) dialogRenderContext() uiframe.Context {
	return uiframe.Context{Width: m.layout.width, Height: m.layout.height, Status: m.cardSizing(nil).Status}
}

func (m *Model) viewFooter() string { return m.footer().text }

// footer is the app's legend: a tier of keys for whatever the cursor is on,
// then a quieter tier for the keys that always apply. A transient mode (quick
// prompt, rename, resize) owns the legend alone while it is up. The layout
// rules live in the package; this routes a mode to the tier it paints.
func (m *Model) footer() legend {
	footer := m.footerOf()
	switch {
	case m.rail.Reordering():
		return m.transientFooter(legendOf(m.rail.ReorderLegend()))
	case m.quick() != nil:
		return m.quickFooter(m.quick())
	}
	if s, ok := surfaceOf[*uigeometry.Surface](m); ok {
		return m.transientFooter(legendSection{title: "Resize", pairs: s.Footer()})
	}
	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok {
		return m.transientFooter(legendSection{title: "Rename", pairs: s.Footer()})
	}
	if m.focused() {
		return rootLegend(footer.Transient(footer.Focused(m.focusFooter())))
	}
	return m.listLegend()
}

// footerOf is the footer's height rule, over the base screen's tiers.
func (m *Model) footerOf() uiframe.Footer {
	return uiframe.Footer{
		List:       frameSections(m.listTiers()),
		Width:      m.layout.width,
		FullLayout: m.prefs.fullLayout,
		ArrowStep:  m.prefs.arrowStep,
	}
}

// listTiers is the base screen's tiers: the cursor's row, then the keys that
// always apply.
func (m *Model) listTiers() []legendSection {
	return []legendSection{m.rowLegend(), m.viewLegend()}
}

// focusFooter is the keys the manager keeps while the agent has the rest.
func (m *Model) focusFooter() uiframe.Focus {
	focus := uiframe.Focus{
		Keys: uiframe.FocusKeys{
			Detach:      m.services.keys.Binding(keybind.Detach).Label(),
			Review:      m.services.keys.Binding(keybind.Review).Label(),
			Editor:      m.services.keys.Binding(keybind.Editor).Label(),
			SavedPrompt: m.services.keys.Binding(keybind.SavedPrompt).Label(),
		},
		Mouse: m.focus.pane.Pane().Mouse,
	}
	if sess, selected := m.selected(); selected {
		focus.PagesScrollback = m.focus.pane.PagesScrollback(sess.ID)
	}
	// A bare surface keeps its own keys and never names the saved prompts key
	// the pane below answers.
	if top, ok := m.top(); ok {
		if _, bare := top.(*uifocus.Surface); bare {
			focus.Bare = true
		} else {
			focus.Surface = top.Footer()
		}
	}
	return focus
}

func (m *Model) listFooter() string { return m.listLegend().text }

func (m *Model) listLegend() legend {
	return rootLegend(m.footerOf().ListLegend())
}

// transientFooter renders one tier at the list footer's height, so a mode that
// owns the legend never resizes every session's pane.
func (m *Model) transientFooter(section legendSection) legend {
	return rootLegend(m.footerOf().Transient(section.pkg()))
}

func (m *Model) listGlyph(actions ...string) string {
	joined := ""
	for _, action := range actions {
		glyph := m.services.listKeys.Binding(action).Glyph("/")
		if glyph == "" {
			continue
		}
		if joined != "" {
			joined += "/"
		}
		joined += glyph
	}
	return joined
}

func (m *Model) messagesAlert() (glyph, label string) {
	if len(m.activeNotices()) == 0 {
		return "", ""
	}
	glyph = m.listGlyph(keybind.Messages)
	if glyph == "" {
		return "", ""
	}
	return glyph, "unread"
}

const (
	contentGutter = 2
	railInset     = uiframe.RailGutter - 1
)

func (m *Model) viewListFrame() string {
	if m.fullFocus() {
		m.layout.displayedRail = uirail.Frame{}
		return m.viewFullFocusFrame()
	}
	if m.fullRows() {
		return m.viewFullListFrame()
	}
	leftWidth, rightWidth := m.splitWidths()
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	contentWidth := rightWidth - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	bleedWidth := contentWidth - 1
	railWidth := leftWidth - 1
	m.prepareRailFrame(railWidth, bodyHeight)
	railRows := railContentLines(m.layout.displayedRail.Lines)
	contentRows := m.contentLines(bleedWidth, bodyHeight)
	seam := make([]string, bodyHeight)
	edge := make([]string, bodyHeight)
	for i := range seam {
		seam[i] = m.seamCell(i < len(railRows) && railRows[i].rule)
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(leftWidth+1, m.layout.width))
	frame = append(frame, uitheme.JoinColumns(
		edge,
		paintContent(railRows, railWidth, bodyHeight, uitheme.PanelHex()),
		seam,
		m.bleedColumn(bodyHeight),
		paintContent(contentRows, bleedWidth, bodyHeight, uitheme.BackdropHex()),
	)...)
	bottom := m.boundedRuleRow(leftWidth+1, m.layout.width, "▄")
	if m.focused() && m.focus.pane.FrameBox().Valid {
		bottom = m.focusBottomRule(leftWidth+1, m.layout.width)
	}
	frame = append(frame, bottom)
	m.placeListFooter(footer, len(frame))
	for _, line := range uitheme.SplitLines(footer.text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

func (m *Model) fullRows() bool { return m.prefs.fullLayout && !m.focused() }

func (m *Model) viewFullListFrame() string {
	footer := m.footer()
	bodyHeight := m.listBodyHeight()
	railWidth := m.layout.width - 1

	frame := []string{}
	for _, line := range m.viewHeaderRows() {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	m.prepareRailFrame(railWidth, bodyHeight)
	railRows := railContentLines(m.layout.displayedRail.Lines)
	edge := make([]string, bodyHeight)
	for i := range edge {
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(railWidth, m.layout.width))
	frame = append(frame, uitheme.JoinColumns(edge, paintContent(railRows, railWidth, bodyHeight, uitheme.PanelHex()))...)
	frame = append(frame, m.boundedRuleRow(railWidth, m.layout.width, "▄"))
	m.placeListFooter(footer, len(frame))
	for _, line := range uitheme.SplitLines(footer.text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// placeListFooter records where the list footer's keys and the quick bar
// it holds landed, with its first row at y.
func (m *Model) placeListFooter(footer legend, y int) {
	m.placeKeys(footer.hits, 0, y)
	if q := m.quick(); q != nil {
		q.OriginY = y
	}
}
