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

// setupPreview draws the workspace the setup's choices give: the real rail
// over a few sample agents, in a window frame, with the part a guide step
// teaches added under it.
func (m *Model) setupPreview(p uionboarding.Preview) []string {
	inner := p.Width - 2
	sessions := previewSessions(p.Agents)
	var extra []string
	switch p.Step {
	case uionboarding.QuickPrompt:
		extra = m.previewQuickBar(inner, p.Agents[0])
	case uionboarding.Places:
		extra = []string{"", m.previewLegend()}
	}
	bodyRows := max(p.Height-5-len(extra), 4)
	rows := append([]string{m.previewHeader(sessions, inner)}, m.previewBody(p, sessions, inner, bodyRows)...)
	for _, row := range extra {
		rows = append(rows, paint(row, inner, backdropHex()))
	}
	return m.previewWindow(rows, p.Width)
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

// previewSessions are the sample agents, spread over the agents left checked.
func previewSessions(agents []string) []uirail.Session {
	now := time.Now()
	tool := func(i int) string { return agents[i%len(agents)] }
	return []uirail.Session{
		{ID: "preview-login", Name: "login redirect", Group: "web", Tool: tool(0), Status: status.Working,
			Prompt: "fix the login redirect loop", PaneLine: "Reading src/auth/redirect.ts", CreatedAt: now.Add(-9 * time.Minute), LastStatusAt: now.Add(-time.Minute)},
		{ID: "preview-dark", Name: "dark mode", Group: "web", Tool: tool(1), Status: status.Waiting,
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
func (m *Model) previewBody(p uionboarding.Preview, sessions []uirail.Session, width, height int) []string {
	rail := uirail.New(nil)
	rail.Reconcile(uirail.Snapshot{Sessions: sessions, Groups: []string{"web", "api"}})
	selected := uirail.Selection{Kind: uirail.SessionRow, SessionID: sessions[0].ID, Group: sessions[0].Group}
	if p.Step == uionboarding.QuickPrompt {
		selected = uirail.Selection{Kind: uirail.GroupRow, Group: "web"}
	}
	rail.Focus(selected)
	railWidth := width - 1
	if !p.Choices.FullLayout {
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
		ComfortableRows: p.Choices.Comfortable,
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
	if p.Choices.FullLayout {
		out := []string{m.railTopRow(railWidth, width)}
		out = append(out, joinColumns(edge, paintContent(railRows, railWidth, height, panelHex()))...)
		return append(out, m.boundedRuleRow(railWidth, width, "▄"))
	}
	contentWidth := width - railWidth - 3
	content := m.previewTranscript(p.Step == uionboarding.QuickPrompt, contentWidth)
	out := []string{m.railTopRow(railWidth+2, width)}
	out = append(out, joinColumns(edge, paintContent(railRows, railWidth, height, panelHex()), seam,
		m.bleedColumn(height), paintContent(content, contentWidth, height, backdropHex()))...)
	return append(out, m.boundedRuleRow(railWidth+2, width, "▄"))
}

// previewTranscript is the live view beside the rail: the selected agent's
// work, or the selected group's summary.
func (m *Model) previewTranscript(group bool, width int) []contentLine {
	g := m.glyphs()
	pad := strings.Repeat(" ", contentGutter)
	line := func(text string) contentLine {
		return contentLine{text: pad + ansi.Truncate(text, max(width-contentGutter, 1), "…")}
	}
	if group {
		return []contentLine{
			{},
			line(lipgloss.NewStyle().Foreground(colorAccent2()).Bold(true).Render("web")),
			line(mutedStyle().Render("2 agents · 1 working · 1 waiting")),
		}
	}
	step := func(text string) contentLine {
		return line(lipgloss.NewStyle().Foreground(colorFinished()).Render(g.FinishedGlyph) + " " + valueStyle().Render(text))
	}
	return []contentLine{
		{},
		line(lipgloss.NewStyle().Foreground(colorAccent()).Render(g.PromptGlyph) + " " + lipgloss.NewStyle().Foreground(colorBright()).Render("fix the login redirect loop")),
		{},
		step("Reading src/auth/redirect.ts"),
		step("The callback adds returnTo twice."),
		line("  " + valueStyle().Render("Patching it, then the tests.")),
		{},
		line(lipgloss.NewStyle().Foreground(colorWorking()).Render(g.WorkingGlyph + " Running tests…")),
	}
}

// previewQuickBar is the quick prompt open on a group, a task half typed.
func (m *Model) previewQuickBar(width int, tool string) []string {
	g := m.glyphs()
	label := subtleStyle().Render(fmt.Sprintf(" %-6s", "new")) + subtleStyle().Render("│ ") +
		lipgloss.NewStyle().Foreground(colorAccent2()).Render("web")
	launch := valueStyle().Render(tool) + subtleStyle().Render(" · "+g.WorktreeGlyph+" off ")
	gap := max(width-ansi.StringWidth(label)-ansi.StringWidth(launch), 1)
	prompt := " " + lipgloss.NewStyle().Foreground(colorAccent()).Render(g.PromptGlyph) + " " +
		lipgloss.NewStyle().Foreground(colorBright()).Render("fix the flaky checkout test") +
		lipgloss.NewStyle().Foreground(colorAccent()).Render(m.chrome().CursorGlyph)
	return []string{paint(label+strings.Repeat(" ", gap)+launch, width, quickModeHex()), paint(prompt, width, quickModeHex())}
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
