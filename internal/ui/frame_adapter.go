package ui

import (
	"fmt"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"io"
	"os"
	"slices"
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
	m.layout.keyHits = m.layout.keyHits.Above(m.layout.height)
	return m.syncCursorAnchor(uiframe.Clamp(frame, m.layout.height, func() string { return uitheme.Paint("", 1, uitheme.BackdropHex()) }))
}

// paintLegend renders a legend as the app's footer, one tier per line where
// the terminal allows it and the tail marked when it does not.
func paintLegend(sections []legendSection, width int, mode presentation.GlyphMode) legend {
	return uiframe.PaintLive(sections, width, mode)
}

// placeKeys records a legend's keys painted with its first row at y and
// its first column at x.
func (m *Model) placeKeys(hits keyHits, x, y int) {
	m.layout.keyHits = append(m.layout.keyHits, hits.Moved(x, y)...)
}

// placePainted records keys a feature painted on row y, from column x.
func (m *Model) placePainted(keys []presentation.PaintedKey, x, y int) {
	var hits keyHits
	for _, key := range keys {
		hits = hits.Add(key.X0, key.X1, key.Y, key.Label)
	}
	m.placeKeys(hits, x, y)
}

type (
	legendSection = uiframe.Section
	legend        = uiframe.Legend
	keyHit        = uiframe.KeyHit
	keyHits       = uiframe.KeyHits
)

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

// cardRect is a painted card's cells, and its body's: the first body line,
// the column it starts at and how many columns it spans.
type cardRect struct {
	top, left, right, bottom     int
	bodyTop, bodyLeft, bodyWidth int
}

func (r cardRect) holds(x, y int) bool {
	return x >= r.left && x < r.right && y >= r.top && y < r.bottom
}

// bodyAt is the body line and column a point lands on, when it lands on
// the body's columns.
func (r cardRect) bodyAt(x, y int) (col, line int, ok bool) {
	col, line = x-r.bodyLeft, y-r.bodyTop
	return col, line, r.holds(x, y) && col >= 0 && col < r.bodyWidth && line >= 0
}

func (m *Model) cardSizing(hint [][2]string) uiframe.CardSizing {
	sizing := uiframe.CardSizing{Mode: m.prefs.glyphMode, Cols: m.layout.width, Hint: hint}
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
	box, hits := uiframe.Box(m.dialogRenderContext(), uiframe.LiveChrome(m.prefs.glyphMode), width, title, body, hint, uitheme.Paint)
	frame := m.centerOnBackdrop(box)
	card := &m.layout.card
	card.bodyTop, card.bodyLeft, card.bodyWidth = card.top+uiframe.CardBodyRow, card.left+uiframe.CardBodyColumn, uiframe.CardInnerWidth(width)
	m.placeKeys(hits, card.left, card.top)
	return frame
}

// centerOnBackdrop floats a block of pre-painted lines in the middle of
// the app frame, filling the rest with the backdrop. The backdrop covers
// any key painted before it, so those keys stop answering clicks.
func (m *Model) centerOnBackdrop(box []string) string {
	ctx := m.dialogRenderContext()
	top, left, right := uiframe.Placement(ctx, box)
	m.layout.card = cardRect{top: top, left: left, right: right, bottom: top + len(box)}
	m.layout.keyHits = nil
	return uiframe.Centered(ctx, box, uitheme.BackdropHex(), uitheme.Paint)
}

func (m *Model) dialogRenderContext() uiframe.Context {
	return uiframe.Context{Width: m.layout.width, Height: m.layout.height, Status: m.cardSizing(nil).Status}
}

func (m *Model) viewFooter() string { return m.footer().Text }

// footer is the app's legend: a tier of keys for whatever the cursor is on,
// then a quieter tier for the keys that always apply. A transient mode (quick
// prompt, rename, resize) owns the legend alone while it is up. The layout
// rules live in the package; this routes a mode to the tier it paints.
func (m *Model) footer() legend {
	footer := m.footerOf()
	switch {
	case m.rail.Reordering():
		return m.transientFooter(legendOf(m.rail.ReorderLegend(m.prefs.glyphMode)))
	case m.quick() != nil:
		return m.quickFooter(m.quick())
	}
	if s, ok := surfaceOf[*uigeometry.Surface](m); ok {
		return m.transientFooter(legendSection{Title: "Resize", Pairs: s.FooterFor(presentation.NavigationLabel(m.prefs.glyphMode, "←→"), strings.TrimPrefix(m.listGlyph(keybind.Resize)+" / release", " / "))})
	}
	if s, ok := surfaceOf[*uidialogs.RenameSurface](m); ok {
		return m.transientFooter(legendSection{Title: "Rename", Pairs: s.Footer()})
	}
	if m.focused() {
		return footer.Transient(footer.Focused(m.focusFooter()))
	}
	return m.listLegend()
}

// footerOf is the footer's height rule, over the base screen's tiers.
func (m *Model) footerOf() uiframe.Footer {
	return uiframe.Footer{
		Mode:        m.prefs.glyphMode,
		List:        m.listTiers(),
		Measurement: m.layout.footer.Measure(m.legendReference(), m.layout.width, max(uiframe.MaxRows, (m.layout.height-m.listChromeRows()-1)/2), m.prefs.glyphMode),
		Width:       m.layout.width,
		FullLayout:  m.prefs.fullLayout,
		ArrowStep:   m.prefs.arrowStep,
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

func (m *Model) listFooter() string { return m.listLegend().Text }

func (m *Model) listLegend() legend {
	return m.footerOf().ListLegend()
}

// transientFooter renders one tier at the list footer's height, so a mode that
// owns the legend never resizes every session's pane.
func (m *Model) transientFooter(section legendSection) legend {
	return m.footerOf().Transient(section)
}

func (m *Model) listGlyph(actions ...string) string {
	joined := ""
	for _, action := range actions {
		glyph := m.services.listKeys.Binding(action).Text("/", m.prefs.glyphMode.Effective() == presentation.GlyphModeAdvanced)
		if glyph == "" {
			continue
		}
		if joined != "" {
			joined += "/"
		}
		joined += glyph
	}
	return presentation.KeyLabel(m.prefs.glyphMode, joined)
}

// listKeyHint fills format with the key bound to action, and is empty while
// nothing is bound, so a sentence never names a key that does nothing.
func (m *Model) listKeyHint(action, format string) string {
	glyph := m.listGlyph(action)
	if glyph == "" {
		return ""
	}
	return fmt.Sprintf(format, glyph)
}

func (m *Model) messagesAlert() (glyph, label string) {
	glyph = m.listGlyph(keybind.Messages)
	if glyph == "" {
		return "", ""
	}
	if m.release.FeedPending {
		return glyph, uinotices.FeedPendingText
	}
	notices := m.activeNotices()
	if len(notices) == 0 {
		return "", ""
	}
	if slices.ContainsFunc(notices, func(n uinotices.Notice) bool { return n.Unread }) {
		return glyph, "unread"
	}
	return glyph, messagesReadLabel
}

// messagesReadLabel is the chip once every active notice is read: dim, and
// still a click into the panel.
const messagesReadLabel = "messages"

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
	railRows := m.layout.displayedRail.Lines
	contentRows := m.contentLines(bleedWidth, bodyHeight)
	seam := make([]string, bodyHeight)
	edge := make([]string, bodyHeight)
	for i := range seam {
		seam[i] = m.seamCell(i < len(railRows) && railRows[i].Rule)
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].Tone != "" {
			tone = railRows[i].Tone
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
	for _, line := range uitheme.SplitLines(footer.Text) {
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
	railRows := m.layout.displayedRail.Lines
	edge := make([]string, bodyHeight)
	for i := range edge {
		tone := uitheme.PanelHex()
		if i < len(railRows) && railRows[i].Tone != "" {
			tone = railRows[i].Tone
		}
		edge[i] = railEdgeCell(tone)
	}
	frame = append(frame, m.railTopRow(railWidth, m.layout.width))
	frame = append(frame, uitheme.JoinColumns(edge, paintContent(railRows, railWidth, bodyHeight, uitheme.PanelHex()))...)
	frame = append(frame, m.boundedRuleRow(railWidth, m.layout.width, "▄"))
	m.placeListFooter(footer, len(frame))
	for _, line := range uitheme.SplitLines(footer.Text) {
		frame = append(frame, uitheme.Paint(line, m.layout.width, uitheme.BackdropHex()))
	}
	return m.overlayTopRight(strings.Join(frame, "\n"), m.statusToast(), m.listChromeRows()+1)
}

// placeListFooter records where the list footer's keys and the quick bar
// it holds landed, with its first row at y.
func (m *Model) placeListFooter(footer legend, y int) {
	m.placeKeys(footer.Hits, 0, y)
	if q := m.quick(); q != nil {
		q.OriginY = y
	}
}

// syncCursorAnchor publishes the input coordinates in a finished frame and
// removes the private marker before Bubble Tea writes the frame. Focus mode
// uses its recorded pane box as a fallback because its caret is painted over
// captured terminal output rather than by a Bubbles input widget.
func (m *Model) syncCursorAnchor(frame string) string {
	if m.focus.imeCursor == nil {
		return uiframe.StripCursorMarker(frame)
	}
	if m.agentOwnsKeyboard() {
		col, row, ok := m.focusCursorAnchor()
		m.focus.imeCursor.Set(col, row, ok)
		return uiframe.StripCursorMarker(frame)
	}
	frame, col, row, ok := uiframe.CursorMarkerPosition(frame)
	m.focus.imeCursor.Set(col, row, ok)
	return frame
}

func (m *Model) focusCursorAnchor() (col, row int, ok bool) {
	if !m.focused() {
		return 0, 0, false
	}
	sess, selected := m.selected()
	if !selected {
		return 0, 0, false
	}
	return m.focus.pane.CursorAnchor(sess.ID)
}

// CursorOutput keeps native TTY detection and terminal sizing while placing
// the host cursor at the active input caret after each rendered frame.
func (m *Model) CursorOutput(output *os.File) io.Writer {
	if m.focus.imeCursor == nil {
		m.focus.imeCursor = &uiframe.CursorAnchor{}
	}
	return uiframe.NewCursorTTYOutput(output, m.focus.imeCursor)
}
