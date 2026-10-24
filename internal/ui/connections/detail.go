package connections

import (
	"fmt"
	"strings"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uipreview "github.com/YoanWai/agent-manager/internal/ui/preview"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Line is one row of the content column for a remote selection: painted
// text, or a seam that spans the column edge to edge.
type Line struct {
	Text string
	Rule bool
}

// Gutter is the blank columns between the frame and the text of a content
// row.
const Gutter = 1

// Selected is the row a rail selection names, as the last snapshot of its
// host has it.
func (c *Connections) Selected(selection uirail.Selection) (Row, bool) {
	if !selection.Remote() {
		return Row{}, false
	}
	kind := SessionRow
	switch selection.Kind {
	case uirail.ConnectionRow:
		kind = ConnectionRow
	case uirail.GroupRow:
		kind = GroupRow
	}
	return c.Row(Target{Kind: kind, Host: selection.Host, Group: selection.Group, ID: selection.SessionID})
}

// Look is what the rows paint with that lives outside this package: the
// glyph a status is marked with and the list key that starts an agent.
type Look struct {
	StatusGlyph func(status string) string
	Prompt      func() string
}

// Content fills the content column for a remote row: a connection's facts, a
// remote group's roster hint, or a remote screen, whose read the caller
// paints.
func (c *Connections) Content(row Row, width, height int, look Look) ([]Line, bool) {
	gutter := strings.Repeat(" ", Gutter)
	inner := width - 2*Gutter
	var head, body []string
	switch row.Kind {
	case ConnectionRow:
		head = c.connectionDetail(row.Host, inner)
	case GroupRow:
		head = []string{lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Bold(true).Render(presentation.EscapeControlsInline(uiframe.DisplayGroup(row.Group))) +
			uitheme.SubtleStyle().Render("  on ") + lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Render(row.Host)}
		body = []string{uitheme.MutedStyle().Render("press " + look.Prompt() + " to start an agent here")}
	default:
		head = c.sessionDetail(row, inner, look.StatusGlyph)
		body = c.Preview.Screen(inner, height-len(head)-1, ScreenView{Err: uitheme.ErrStyle(), Muted: uitheme.MutedStyle(), ExpandTabs: uipreview.ExpandTabs})
	}
	var lines []Line
	for _, line := range head {
		lines = append(lines, Line{Text: gutter + line})
	}
	if body != nil {
		lines = append(lines, Line{Rule: true})
		for _, line := range body {
			lines = append(lines, Line{Text: gutter + line})
		}
	}
	for len(lines) < height {
		lines = append(lines, Line{})
	}
	return lines[:max(height, 0)], true
}

func (c *Connections) connectionDetail(host string, width int) []string {
	conn, _ := c.Connection(host)
	state, seen := c.Host(host)
	title := lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Bold(true).Render(host) + "  " + uitheme.ChipStyle().Render("ssh")
	reading := uitheme.SubtleStyle().Render("connecting…")
	switch {
	case seen && state.OK:
		reading = lipgloss.NewStyle().Foreground(uitheme.ColorFinished()).Render("online")
	case seen:
		reading = uitheme.ErrStyle().Render(fmt.Sprintf("%s · %d failed", TroubleLabel(state.Trouble), state.Failures))
	}
	lines := []string{
		uipreview.FitColumns([]string{title}, []string{reading}, width),
		uipreview.FactRow(factLabel("ssh to"), uipreview.TrimmedValue(conn.Destination), "", width),
	}
	if conn.Binary != "" {
		lines = append(lines, uipreview.FactRow(factLabel("binary"), uipreview.TrimmedValue(conn.Binary), "", width))
	}
	if seen && state.OK {
		manager := "manager awake"
		if !state.ManagerAwake {
			manager = "no manager running"
		}
		lines = append(lines, uipreview.FactRow(factLabel("host"), uipreview.PlainValue(uitheme.MutedStyle().Render(CountNoun(len(state.Sessions), "session")+" · "+
			CountNoun(len(state.Terminals), "terminal")+" · "+manager)), "", width))
	}
	if seen && state.Err != "" {
		for _, line := range strings.Split(ansi.Wordwrap(presentation.EscapeControls(state.Err), max(width-uipreview.DetailLabelWidth, 8), " "), "\n") {
			lines = append(lines, uitheme.LabelStyle().Render(uipreview.FactLabel("error"))+uitheme.ErrStyle().Render(line))
		}
	}
	return lines
}

func (c *Connections) sessionDetail(row Row, width int, glyph func(string) string) []string {
	state := row.Status
	if row.Archived {
		state = "archived"
	}
	tint := lipgloss.NewStyle().Foreground(StatusColor(row.Status))
	name := lipgloss.NewStyle().Foreground(uitheme.ColorBright()).Bold(true).Render(presentation.EscapeControlsInline(row.Name))
	host := lipgloss.NewStyle().Foreground(uitheme.ColorRemote()).Render(row.Host)
	group := lipgloss.NewStyle().Foreground(uitheme.ColorAccent2()).Render(presentation.EscapeControlsInline(uiframe.DisplayGroup(row.Group)))
	return []string{
		uipreview.FitColumns([]string{name + uitheme.SubtleStyle().Render("  on ") + host, name},
			[]string{tint.Render(glyph(row.Status) + " " + presentation.EscapeControlsInline(StatusLabel(state)))}, width),
		uipreview.FactRow(factLabel("group"), uipreview.PlainValue(group), "", width),
	}
}

// StatusLabel is the human text for a status; the transient launch state
// reads better spelled out.
func StatusLabel(state string) string {
	if state == "starting" {
		return "starting up"
	}
	return state
}

func CountNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// AimLost refuses an enter whose target a refresh moved, when either end of
// the aim is on a connection: the row the bar was aimed at is gone, or the
// cursor slid to another row without the user moving it.
func (c *Connections) AimLost(aim, selection uirail.Selection) (string, bool) {
	if !aim.Remote() && !selection.Remote() {
		return "", false
	}
	if _, ok := c.Selected(aim); aim.Remote() && !ok {
		return "the agent this prompt was aimed at is gone from " + aim.Host + "; nothing was sent", true
	}
	if !SameRow(aim, selection) {
		return "the selection moved since this prompt was aimed; nothing was sent", true
	}
	return "", false
}

// SameRow compares rows by identity; a session that changed group is still
// the same session.
func SameRow(a, b uirail.Selection) bool {
	if a.Kind == uirail.SessionRow && b.Kind == uirail.SessionRow {
		return a.Host == b.Host && a.SessionID == b.SessionID
	}
	return a == b
}

func factLabel(label string) string {
	return uitheme.LabelStyle().Render(uipreview.FactLabel(label))
}
