package ui

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/remote"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// remotePreview is the selected remote row's screen as its last read
// returned it. gen fences reads against the selection they were made for.
type remotePreview struct {
	ref      remote.Ref
	terminal bool
	text     string
	err      string
	gen      uint64
	reading  bool
}

type remotePreviewMsg struct {
	ref  remote.Ref
	gen  uint64
	text string
	err  error
}

// syncRemotePreview follows the selection: a remote session or terminal it
// has not read yet gets a fresh read, anything else clears it.
func (m *Model) syncRemotePreview() tea.Cmd {
	p := &m.ssh.preview
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok || row.kind != uirail.SessionRow {
		*p = remotePreview{gen: p.gen + 1}
		return nil
	}
	if p.ref == row.ref {
		return nil
	}
	*p = remotePreview{ref: row.ref, terminal: row.terminal, gen: p.gen + 1}
	return m.readRemotePreview()
}

// readRemotePreview reads the tracked row's screen once, unless a read is
// already out or its host is known to be unreachable.
func (m *Model) readRemotePreview() tea.Cmd {
	p := &m.ssh.preview
	if p.ref.ID == "" || p.reading {
		return nil
	}
	if _, offline := m.ssh.offline(p.ref.Host); offline {
		return nil
	}
	p.reading = true
	client, ref, terminal, gen := m.ssh.client, p.ref, p.terminal, p.gen
	return func() tea.Msg {
		text, err := client.Read(context.Background(), ref, terminal)
		return remotePreviewMsg{ref: ref, gen: gen, text: text, err: err}
	}
}

func (m *Model) applyRemotePreview(msg remotePreviewMsg) {
	p := &m.ssh.preview
	if msg.gen != p.gen || msg.ref != p.ref {
		return
	}
	p.reading = false
	if msg.err != nil {
		p.err = msg.err.Error()
		return
	}
	p.text, p.err = msg.text, ""
}

// remoteContentLines fills the content column for a remote selection: a
// connection's facts, a remote group's roster, or a remote screen.
func (m *Model) remoteContentLines(width, height int) ([]contentLine, bool) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return nil, false
	}
	gutter := strings.Repeat(" ", contentGutter)
	inner := width - 2*contentGutter
	var head []string
	var body []string
	switch row.kind {
	case uirail.ConnectionRow:
		head = m.connectionDetail(row.host, inner)
	case uirail.GroupRow:
		head = []string{lipgloss.NewStyle().Foreground(colorAccent2).Bold(true).Render(escapeControlsInline(displayGroup(row.group))) +
			subtleStyle.Render("  on ") + lipgloss.NewStyle().Foreground(colorRemote).Render(row.host)}
		body = []string{mutedStyle.Render("press " + m.listGlyph(keybind.Prompt) + " to start an agent here")}
	default:
		head = m.remoteSessionDetail(row, inner)
		body = m.remoteScreen(inner, height-len(head)-1)
	}
	var lines []contentLine
	for _, line := range head {
		lines = append(lines, contentLine{text: gutter + line})
	}
	if body != nil {
		lines = append(lines, contentLine{rule: true})
		for _, line := range body {
			lines = append(lines, contentLine{text: gutter + line})
		}
	}
	for len(lines) < height {
		lines = append(lines, contentLine{})
	}
	return lines[:max(height, 0)], true
}

func (m *Model) connectionDetail(host string, width int) []string {
	conn, _ := m.ssh.connection(host)
	state, seen := m.ssh.hosts[host]
	title := lipgloss.NewStyle().Foreground(colorRemote).Bold(true).Render(host) + "  " + chipStyle.Render("ssh")
	reading := subtleStyle.Render("connecting…")
	switch {
	case seen && state.OK:
		reading = lipgloss.NewStyle().Foreground(colorFinished).Render("online")
	case seen:
		reading = errStyle.Render(fmt.Sprintf("offline · %d failed", state.Failures))
	}
	lines := []string{
		fitColumns([]string{title}, []string{reading}, width),
		factRow("ssh to", trimmedValue(conn.Destination), "", width),
	}
	if seen && state.OK {
		manager := "manager awake"
		if !state.Snapshot.ManagerAwake {
			manager = "no manager running"
		}
		lines = append(lines, factRow("host", plainValue(mutedStyle.Render(countNoun(len(state.Snapshot.Sessions), "session")+" · "+
			countNoun(len(state.Snapshot.Terminals), "terminal")+" · "+manager)), "", width))
	}
	if seen && state.Err != nil {
		for _, line := range strings.Split(ansi.Wordwrap(escapeControls(state.Err.Error()), max(width-detailLabelWidth, 8), " "), "\n") {
			lines = append(lines, labelStyle.Render(padRight("error", detailLabelWidth))+errStyle.Render(line))
		}
	}
	return lines
}

func (m *Model) remoteSessionDetail(row remoteRow, width int) []string {
	status := row.status
	if row.archived {
		status = "archived"
	}
	state := lipgloss.NewStyle().Foreground(statusColor(row.status)).Render(statusGlyph(row.status) + " " + escapeControlsInline(statusLabel(status)))
	name := lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(escapeControlsInline(row.name))
	host := lipgloss.NewStyle().Foreground(colorRemote).Render(row.host)
	group := lipgloss.NewStyle().Foreground(colorAccent2).Render(escapeControlsInline(displayGroup(row.group)))
	return []string{
		fitColumns([]string{name + subtleStyle.Render("  on ") + host, name}, []string{state}, width),
		factRow("group", plainValue(group), "", width),
	}
}

// remoteScreen is the last read of a remote pane as plain text, its bottom
// rows kept the way a local preview keeps them.
func (m *Model) remoteScreen(width, height int) []string {
	p := m.ssh.preview
	if height <= 0 {
		return []string{}
	}
	if p.err != "" {
		return []string{errStyle.Render(ansi.Truncate(escapeControlsInline(p.err), width, "…"))}
	}
	text := strings.TrimRight(p.text, "\n ")
	if text == "" {
		if p.reading {
			return []string{mutedStyle.Render("reading the screen over SSH…")}
		}
		return []string{mutedStyle.Render("(no output yet)")}
	}
	lines := strings.Split(screenText(text), "\n")
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(expandPaneTabs(line, width), width, "")
	}
	return lines
}

// screenText is a remote screen as plain text: escape sequences stripped,
// and every control but a newline or tab dropped, since a bare \r or \b
// moves the cursor of the terminal the frame paints into.
func screenText(text string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if (r < utf8.RuneSelf && presentation.IsControlByte(byte(r))) || presentation.IsEscapedRune(r) {
			return -1
		}
		return r
	}, ansi.Strip(text))
}

// remoteRowLegend is the footer tier for a connection's rows: only the keys
// the connection carries out.
func (m *Model) remoteRowLegend() legendSection {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return legendSection{}
	}
	k := m.listGlyph
	fold := "fold"
	if m.rail.SelectionCollapsed(selection) {
		fold = "unfold"
	}
	leads := [][2]string{m.quickModeLead()}
	switch row.kind {
	case uirail.ConnectionRow:
		return legendSection{title: "SSH connection", leads: leads, pairs: legendPairsBound([][2]string{
			{k(keybind.Open), fold}, {k(keybind.Terminal), "terminal"}, {k(keybind.Rename), "edit"}, {k(keybind.Delete), "remove"},
		})}
	case uirail.GroupRow:
		return legendSection{title: "Group", leads: leads, pairs: legendPairsBound([][2]string{
			{k(keybind.Open), fold}, {k(keybind.Terminal), "terminal"},
		})}
	}
	title, kill := "Session", "kill"
	if row.terminal {
		title, kill, leads = "Shell", "close", nil
	}
	pairs := [][2]string{{k(keybind.Open, keybind.Attach), "attach over SSH"}, {k(keybind.Kill), kill}}
	if row.status == "dead" {
		pairs = append(pairs, [2]string{k(keybind.Revive), "revive"})
	}
	pairs = append(pairs, m.archiveRestoreLegend())
	return legendSection{title: title + " on " + row.host, leads: leads, pairs: legendPairsBound(pairs)}
}

// remoteQuickFacts aims the quick bar's target row at a connection: a
// remote session it answers, or the connection or remote group it spawns
// into, with the worktree left to the host until picked.
func (m *Model) remoteQuickFacts(facts *quickFacts) {
	selection, _ := m.rail.Selected()
	row, ok := m.ssh.row(selection)
	if !ok {
		return
	}
	host := subtleStyle.Render(" on ") + lipgloss.NewStyle().Foreground(colorRemote).Render(row.host)
	if row.kind == uirail.SessionRow {
		facts.remote = lipgloss.NewStyle().Foreground(colorBright).Bold(true).Render(escapeControlsInline(row.name)) + host
		return
	}
	facts.remote = lipgloss.NewStyle().Foreground(colorAccent2).Render(escapeControlsInline(displayGroup(row.group))) + host
	facts.remoteSpawn = true
	facts.worktreeKnown, facts.worktreeCapable = true, true
	facts.worktreeOn = m.quick.worktreeTouched && m.quick.worktree
	facts.worktreeInherit = !m.quick.worktreeTouched
}

func countNoun(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
