package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	uionboarding "github.com/YoanWai/agent-manager/internal/ui/onboarding"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// previewScene is one frame of the window beside a step: the sample agents
// and the selected row, the live view beside the rail, rows under the list
// such as the quick bar, or a body that takes the whole window, such as a
// focused agent in the full layout or Review.
type previewScene struct {
	sessions []uirail.Session
	groups   []string
	selected uirail.Selection
	content  []contentLine
	foot     []string
	body     []string
}

// setupPreview draws the workspace the setup's choices give: the real rail
// over a few sample agents, in a window frame, acting out what a guide step
// teaches.
func (m *Model) setupPreview(p uionboarding.Preview) []string {
	inner := p.Width - 2
	rows := p.Height - 2
	scene := m.setupScene(p, inner, rows-1)
	out := []string{m.previewHeader(scene.sessions, inner)}
	if scene.body != nil {
		out = append(out, scene.body...)
	} else {
		bodyRows := max(rows-3-len(scene.foot), 4)
		out = append(out, m.previewBody(p.Choices, scene, inner, bodyRows)...)
		for _, row := range scene.foot {
			out = append(out, paint(row, inner, backdropHex()))
		}
	}
	for len(out) < rows {
		out = append(out, paint("", inner, backdropHex()))
	}
	return m.previewWindow(out[:rows], p.Width)
}

func (m *Model) previewWindow(rows []string, width int) []string {
	border := cardBorderStyle()
	chrome := m.chrome()
	edge := border.Render("│")
	out := []string{border.Render(chrome.TopLeftGlyph + strings.Repeat("─", width-2) + chrome.TopRightGlyph)}
	for _, row := range rows {
		out = append(out, edge+row+edge)
	}
	return append(out, border.Render(chrome.BottomLeftGlyph+strings.Repeat("─", width-2)+chrome.BottomRightGlyph))
}

const (
	previewLogin = "preview-login"
	previewDark  = "preview-dark"
)

// previewSessions are the sample agents, spread over the agents left checked.
func previewSessions(agents []string) []uirail.Session {
	now := time.Now()
	tool := func(i int) string { return agents[i%len(agents)] }
	return []uirail.Session{
		{ID: previewLogin, Name: "login redirect", Group: "web", Tool: tool(0), Status: status.Working,
			Prompt: "fix the login redirect loop", PaneLine: "Reading src/auth/redirect.ts", CreatedAt: now.Add(-9 * time.Minute), LastStatusAt: now.Add(-time.Minute)},
		{ID: previewDark, Name: "dark mode", Group: "web", Tool: tool(1), Status: status.Waiting,
			Prompt: "add a dark mode toggle", PaneLine: "Allow edits to theme.css?", CreatedAt: now.Add(-20 * time.Minute), LastStatusAt: now.Add(-4 * time.Minute)},
		{ID: "preview-limits", Name: "rate limits", Group: "api", Tool: tool(2), Status: status.Finished,
			Prompt: "add tests for the rate limiter", PaneLine: "All 12 tests pass.", CreatedAt: now.Add(-40 * time.Minute), LastStatusAt: now.Add(-12 * time.Minute)},
		{ID: "preview-pg", Name: "pg 17 upgrade", Group: "api", Tool: tool(3), Status: status.Idle,
			Prompt: "plan the postgres 17 upgrade", PaneLine: "The plan is in docs/pg17.md.", CreatedAt: now.Add(-3 * time.Hour), LastStatusAt: now.Add(-time.Hour)},
		{ID: "preview-triage", Name: "triage issues", Tool: tool(4), Status: status.Finished,
			Prompt: "sort this week's issues", PaneLine: "Sorted 14 issues into 3 labels.", CreatedAt: now.Add(-5 * time.Hour), LastStatusAt: now.Add(-2 * time.Hour)},
	}
}

// previewHeader is the window's own wordmark and its agents by state.
func (m *Model) previewHeader(sessions []uirail.Session, width int) string {
	left := " " + lipgloss.NewStyle().Foreground(colorAccent()).Bold(true).Render("agent-manager")
	tally := map[string]int{}
	for _, session := range sessions {
		tally[session.Status]++
	}
	var counts []string
	for _, state := range []string{status.Working, status.Waiting, status.Finished, status.Idle} {
		mark := lipgloss.NewStyle().Foreground(statusColor(state)).Render(m.glyphs().StatusGlyph(state))
		counts = append(counts, mark+" "+valueStyle().Render(fmt.Sprint(tally[state])))
	}
	right := strings.Join(counts, "  ") + " "
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return paint(left, width, backdropHex())
	}
	return paint(left+strings.Repeat(" ", gap)+right, width, backdropHex())
}

// previewBody is the list as the layout and density choices draw it, the
// rail rendered by the rail itself.
func (m *Model) previewBody(choices uionboarding.Choices, scene previewScene, width, height int) []string {
	rail := uirail.New(nil)
	rail.Reconcile(uirail.Snapshot{Sessions: scene.sessions, Groups: scene.groups})
	rail.Focus(scene.selected)
	railWidth := width - 1
	if !choices.FullLayout {
		railWidth = width/2 - 1
	}
	frame := rail.Render(uirail.RenderContext{
		GlyphMode:       m.prefs.glyphMode,
		Width:           railWidth,
		Height:          height,
		TerminalWidth:   width,
		TerminalHeight:  height,
		Theme:           current(),
		ListKeys:        m.services.listKeys,
		ComfortableRows: choices.Comfortable,
		MouseDisabled:   true,
		EnterFocuses:    m.enterFocuses(),
	}, uirail.Frame{})
	railRows := railContentLines(frame.Lines)
	edge := make([]string, height)
	seam := make([]string, height)
	for i := range edge {
		tone := panelHex()
		if i < len(railRows) && railRows[i].tone != "" {
			tone = railRows[i].tone
		}
		edge[i] = railEdgeCell(tone)
		seam[i] = m.seamCell(i < len(railRows) && railRows[i].rule)
	}
	if choices.FullLayout {
		out := []string{m.railTopRow(railWidth, width)}
		out = append(out, joinColumns(edge, paintContent(railRows, railWidth, height, panelHex()))...)
		return append(out, m.boundedRuleRow(railWidth, width, "▄"))
	}
	contentWidth := width - railWidth - 3
	out := []string{m.railTopRow(railWidth+2, width)}
	out = append(out, joinColumns(edge, paintContent(railRows, railWidth, height, panelHex()), seam,
		m.bleedColumn(height), paintContent(m.fitTranscript(scene.content, contentWidth, height), contentWidth, height, backdropHex()))...)
	return append(out, m.boundedRuleRow(railWidth+2, width, "▄"))
}

// fitTranscript keeps the newest lines of a live view that outgrew its
// height, the way a pane scrolls, and cuts each line to the width.
func (m *Model) fitTranscript(lines []contentLine, width, height int) []contentLine {
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	out := make([]contentLine, len(lines))
	for i, line := range lines {
		out[i] = contentLine{text: ansi.Truncate(line.text, max(width, 1), "…")}
	}
	return out
}

// previewBar is the quick prompt: the target on one row, what it launches
// with against the right edge, and the prompt typed so far under it.
func (m *Model) previewBar(width int, label, target, launch, typed string) []string {
	head := subtleStyle().Render(fmt.Sprintf(" %-7s", label)) + subtleStyle().Render("│ ") + target
	gap := max(width-ansi.StringWidth(head)-ansi.StringWidth(launch), 1)
	prompt := " " + lipgloss.NewStyle().Foreground(colorAccent()).Render(m.glyphs().PromptGlyph) + " " +
		lipgloss.NewStyle().Foreground(colorBright()).Render(typed) +
		lipgloss.NewStyle().Foreground(colorAccent()).Render(m.chrome().CursorGlyph)
	return []string{paint(head+strings.Repeat(" ", gap)+launch, width, quickModeHex()), paint(prompt, width, quickModeHex())}
}

// previewLegend names where Settings, Messages and the key map open.
func (m *Model) previewLegend() string {
	keys := m.setupKeys()
	var parts []string
	for _, pair := range [][2]string{{keys.Settings, "settings"}, {keys.Messages, "messages"}, {keys.Help, "keys"}} {
		if pair[0] != "" {
			parts = append(parts, hintCap(pair[0], pair[1]))
		}
	}
	return " " + strings.Join(parts, "   ")
}

// typedOut is text as typed so far, frame beats after typing began.
func typedOut(text string, frame int) string {
	runes := []rune(text)
	return string(runes[:min(max(frame, 0), len(runes))])
}
