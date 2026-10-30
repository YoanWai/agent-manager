package onboarding

import (
	"fmt"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Keys are the bindings the guide names, as the key tables hold them now.
// An empty one is an action turned off, and its line is left out.
type Keys struct {
	Prompt, Focus, Back, Review, Settings, Messages, Help string
	// The other ways in, named under a card's main flow: the New Session
	// and New Group forms, and the focus and back keys besides the arrows.
	NewSession, NewGroup, OtherFocus, OtherBack string
}

// ViewContext is what a frame reads from the root. Height leaves out the
// footer the root paints under the setup.
type ViewContext struct {
	Width, Height int
	GlyphMode     presentation.GlyphMode
	Theme         uitheme.Theme
	Wordmark      string
	Keys          Keys
	// Status is the status bar's message, already styled.
	Status string
	// Preview draws the live window beside the step, nil to leave it out.
	Preview func(Preview) []string
}

// Preview is the window a step shows: the workspace as the choices on
// screen would draw it.
type Preview struct {
	Step          Step
	Choices       Choices
	Agents        []string
	Width, Height int
	// Scene is the guide line an animated step is acting out, and Frame how
	// many beats into it the animation is.
	Scene, Frame int
	// Import is the import as picked so far.
	Import ImportRequest
}

// Frame is the setup's rows and the column its content starts at, which
// the footer lines up with.
type Frame struct {
	Lines []string
	Left  int
}

const (
	margin = 4
	gutter = 6
	// bandRows are two blank rows, the wordmark with the progress, and the
	// blank row under them.
	bandRows       = 4
	previewMinRows = 12
	previewMaxRows = 26
)

type palette struct {
	mode                                            presentation.GlyphMode
	theme                                           uitheme.Theme
	heading, text, bright, dim, subtle, accent, cap lipgloss.Style
	found, warn                                     lipgloss.Style
}

func newPalette(t uitheme.Theme, mode presentation.GlyphMode) palette {
	color := func(hex string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)) }
	return palette{
		mode:    mode,
		theme:   t,
		heading: color(t.Bright).Bold(true),
		text:    color(t.Text),
		bright:  color(t.Bright).Bold(true),
		dim:     color(t.Dim),
		subtle:  color(t.Subtle),
		accent:  color(t.Accent).Bold(true),
		cap:     color(t.Accent).Background(lipgloss.Color(t.Surface)).Bold(true).Padding(0, 1),
		found:   color(t.Finished),
		warn:    color(t.Working).Bold(true),
	}
}

// View paints the setup in ctx.Height rows and records the option each row
// shows, for clicks.
func (m *Model) View(ctx ViewContext) Frame {
	p := newPalette(ctx.Theme, ctx.GlyphMode)
	bodyRows := max(ctx.Height-bandRows-1, 1)
	columnWidth, previewWidth := columns(ctx.Width, ctx.Preview != nil && bodyRows >= previewMinRows)
	total := columnWidth
	if previewWidth > 0 {
		total += gutter + previewWidth
	}
	left := max((ctx.Width-total)/2, 0)
	m.options = columnSpan{left: left, width: columnWidth}
	column, items := m.column(ctx, p, columnWidth, bodyRows)
	var preview []string
	if previewWidth > 0 {
		scene, frame := m.scene()
		pending, _ := m.pendingImport()
		preview = ctx.Preview(Preview{Step: m.Step(), Choices: m.chosen.clone(), Agents: m.Enabled(),
			Width: previewWidth, Height: min(bodyRows, previewMaxRows), Scene: scene, Frame: frame, Import: pending})
	}
	block := max(len(column), len(preview))
	top := bandRows + max((bodyRows-block)/2, 0)
	columnTop, previewTop := top+(block-len(column))/2, top+(block-len(preview))/2

	lines := make([]string, ctx.Height)
	m.lines.Reset()
	indent := strings.Repeat(" ", left)
	for y := range lines {
		if y == bandRows-2 {
			lines[y] = indent + m.band(p, ctx.Wordmark, total)
		}
		if y < bandRows {
			m.lines.Skip(1)
			continue
		}
		cell, item := "", -1
		if i := y - columnTop; i >= 0 && i < len(column) {
			cell, item = column[i], items[i]
		}
		line := indent + presentation.PadRight(cell, columnWidth)
		if i := y - previewTop; previewWidth > 0 && i >= 0 && i < len(preview) {
			line += strings.Repeat(" ", gutter) + preview[i]
		}
		lines[y] = line
		if item >= 0 {
			m.lines.Show(item, 1)
		} else {
			m.lines.Skip(1)
		}
	}
	return Frame{Lines: lines, Left: left}
}

type columnSpan struct{ left, width int }

func (s columnSpan) holds(col int) bool { return col >= s.left && col < s.left+s.width }

// columns sizes the step's column and the preview beside it. A terminal
// too narrow for both keeps the column alone.
func columns(width int, preview bool) (column, window int) {
	room := width - 2*margin
	column = min(max(width*3/8, 38), 52)
	window = min(room-column-gutter, 96)
	if !preview || window < 46 {
		return min(max(room, 1), 64), 0
	}
	return column, window
}

// band is the wordmark and, against the right edge, how far along the
// setup is.
func (m Model) band(p palette, wordmark string, width int) string {
	progress := m.progress(p)
	gap := width - ansi.StringWidth(wordmark) - ansi.StringWidth(progress)
	if gap < 2 {
		return wordmark
	}
	return wordmark + strings.Repeat(" ", gap) + progress
}

func (m Model) progress(p palette) string {
	current, done, ahead := "●", "●", "○"
	if p.mode.Effective() == presentation.GlyphModeBasic {
		current, done, ahead = "*", "*", "."
	}
	var b strings.Builder
	for i, step := range m.steps {
		if i > 0 {
			b.WriteString(" ")
			if m.steps[i-1].choosing() && !step.choosing() || step == Import {
				b.WriteString("  ")
			}
		}
		switch {
		case i == m.at:
			b.WriteString(p.accent.Render(current))
		case i < m.at:
			b.WriteString(p.dim.Render(done))
		default:
			b.WriteString(p.subtle.Render(ahead))
		}
	}
	return p.dim.Render("quick setup   ") + b.String() + p.subtle.Render(fmt.Sprintf("   %d of %d", m.at+1, len(m.steps)))
}

// column is the step's own text and options, with the option each line
// shows or -1.
func (m *Model) column(ctx ViewContext, p palette, width, rows int) ([]string, []int) {
	var lines []string
	var items []int
	add := func(line string, item int) {
		lines = append(lines, line)
		items = append(items, item)
	}
	step := m.Step()
	title, lead := copyFor(step)
	add(p.accent.Render(presentation.ChromeFor(p.mode).SectionGlyph)+" "+p.heading.Render(title), -1)
	add("", -1)
	for _, line := range strings.Split(ansi.Wordwrap(lead, width-2, ""), "\n") {
		add("  "+p.dim.Render(line), -1)
	}
	add("", -1)
	switch step {
	case Agents:
		visible := min(len(m.agents), max(rows-len(lines)-2, 3))
		m.agentFirst = presentation.Follow(m.agentFirst, m.agentCursor, len(m.agents), visible)
		for i := m.agentFirst; i < m.agentFirst+visible; i++ {
			add(m.agentRow(p, m.agents[i], i == m.agentCursor, width), i)
		}
	case Layout:
		add(radioRow(p, !m.chosen.FullLayout, "Split", "list beside a live view", width), 0)
		add(radioRow(p, m.chosen.FullLayout, "Full screen", "the list takes it all", width), 1)
	case Density:
		add(radioRow(p, !m.chosen.Comfortable, "Compact", "one line per agent", width), 0)
		add(radioRow(p, m.chosen.Comfortable, "Comfortable", "status on its own line", width), 1)
	case Theme:
		add(m.followRow(p), 0)
		add("", -1)
		room := max(rows-len(lines)-2, 3)
		visible := min(len(uitheme.Palettes), room)
		m.themeFirst = presentation.Follow(m.themeFirst, m.themeCursor-1, len(uitheme.Palettes), visible)
		for i := m.themeFirst; i < m.themeFirst+visible; i++ {
			add(m.themeRow(p, i, width), i+1)
		}
	case Import:
		m.importColumn(p, width, rows-len(lines), add)
	default:
		active := -1
		if m.Animating() {
			active = m.line()
		}
		for _, line := range guideRows(p, step, ctx.Keys, active) {
			add(line, -1)
		}
		for _, line := range alsoLines(p, step, ctx.Keys) {
			add(line, -1)
		}
	}
	if m.notice != "" {
		add("", -1)
		add("  "+p.warn.Render(m.notice), -1)
	}
	if ctx.Status != "" {
		add("", -1)
		add("  "+ansi.Truncate(ctx.Status, width-2, "…"), -1)
	}
	return lines, items
}

// copyFor is a step's title and the line under it.
func copyFor(step Step) (string, string) {
	switch step {
	case Welcome:
		return "Welcome to agent-manager", "Every coding agent you run, in one list. A few quick choices and a short tour, about a minute. Everything can change later."
	case Agents:
		return "Which agents do you use?", "Checked ones show up when you start a session."
	case Layout:
		return "Pick a layout", "Keep a live view of the agent beside the list, or give the list the whole screen."
	case Density:
		return "How much per row?", "Compact fits more agents. Comfortable gives each one room to breathe."
	case Theme:
		return "Pick a theme", "Try a few. The whole screen follows along."
	case QuickPrompt:
		return "Start an agent in one line", "Pick a group, type the task, done."
	case Sessions:
		return "Talk to your agents", "Answer from the list, or step inside."
	case Review:
		return "Review what changed", "Read an agent's diff, comment on a line, and hand the notes back."
	case Orchestrate:
		return "Let one agent lead the rest", "Agents here can open more agents, sort them into groups, and keep them in step. Ask one to lead."
	case Import:
		return "Bring in your past chats", "Optional. An agent finds the conversations your CLIs saved on this machine and adds each one to the list, ready to pick up where you left off."
	}
	return "Change anything later", "Every choice here lives in Settings, and so does this setup."
}

// selectable draws a row the cursor can rest on: a marker, then a band
// under the whole row while the cursor is on it.
func selectable(p palette, on bool, body string, width int) string {
	if !on {
		return "  " + body
	}
	row := p.accent.Render(presentation.GlyphsFor(p.mode).SelectionGlyph) + " " + body
	return presentation.Fill(presentation.PadRight(row, width), p.theme.Surface)
}

func checkbox(p palette, on bool) string {
	mark := "✓"
	if p.mode.Effective() == presentation.GlyphModeBasic {
		mark = "x"
	}
	if !on {
		mark = " "
	}
	return p.subtle.Render("[") + p.accent.Render(mark) + p.subtle.Render("]")
}

func (m Model) agentRow(p palette, agent Agent, cursor bool, width int) string {
	name := p.text
	if cursor {
		name = p.bright
	}
	status := p.subtle.Render("…")
	switch {
	case m.probed && agent.Installed:
		status = p.found.Render("installed")
	case m.probed:
		status = p.subtle.Render("not found")
	}
	body := checkbox(p, !m.chosen.Hidden[agent.Name]) + " " + presentation.PadRight(name.Render(agent.Name), 16) + status
	return selectable(p, cursor, body, width)
}

func radioRow(p palette, on bool, name, note string, width int) string {
	mark, nameStyle := p.subtle.Render("○"), p.text
	if on {
		mark, nameStyle = p.accent.Render("●"), p.bright
	}
	if p.mode.Effective() == presentation.GlyphModeBasic {
		mark = p.subtle.Render("( )")
		if on {
			mark = p.accent.Render("(*)")
		}
	}
	return selectable(p, on, mark+" "+presentation.PadRight(nameStyle.Render(name), 14)+p.dim.Render(note), width)
}

func (m Model) followRow(p palette) string {
	return "  " + checkbox(p, m.chosen.ThemeAuto) + " " + p.text.Render("Follow the OS") + p.dim.Render("  its light or dark side")
}

func (m Model) themeRow(p palette, index, width int) string {
	t := uitheme.Palettes[index]
	on := m.themeCursor == index+1
	name := p.text
	if on {
		name = p.bright
	}
	var swatch strings.Builder
	for _, hex := range []string{t.Accent, t.Accent2, t.Working, t.Waiting, t.Finished, t.Errored} {
		swatch.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Render("█"))
	}
	chip := presentation.Fill(" "+swatch.String()+" ", t.Bg)
	tag := ""
	if t.LightBackdrop() {
		tag = p.subtle.Render("  light")
	}
	return selectable(p, on, presentation.PadRight(name.Render(t.Name), 20)+chip+tag, width)
}

// guideRows are a guide step's lines, each a cap beside what it does. A key
// the tables leave unbound drops its line. A typed row names no key, and a
// spoken row is something to say to an agent. active is the line the
// preview is acting out, or -1.
func guideRows(p palette, step Step, keys Keys, active int) []string {
	type row struct {
		key, does     string
		typed, spoken bool
		// outcome is what an agent does in answer to the spoken row.
		outcome bool
	}
	var rows []row
	switch step {
	case QuickPrompt:
		rows = []row{{key: keys.Prompt, does: "on a group"}, {key: "type", does: "the task", typed: true}, {key: "↵", does: "the agent starts there"}}
	case Sessions:
		rows = []row{{key: keys.Prompt, does: "answer the selected agent"}, {key: keys.Focus, does: "focus it and type right in"},
			{key: keys.Back, does: "come back to the list"}}
	case Review:
		rows = []row{{key: keys.Review, does: "open the agent's diff"}, {key: "c", does: "comment on a line"}, {key: "C", does: "send the comments to the agent"}}
	case Orchestrate:
		rows = []row{{does: OrchestrateAsk, spoken: true}, {does: "it opens the agents, sorted into groups", outcome: true},
			{does: "and keeps them in step to the finish", outcome: true}}
	case Places:
		rows = []row{{key: keys.Settings, does: "Settings: every choice, and this setup"},
			{key: keys.Messages, does: "Messages: news and tips"}, {key: keys.Help, does: "every key, searchable"}}
	}
	typed := p.cap.Foreground(lipgloss.Color(p.theme.Dim)).Bold(false)
	lit := p.cap.Foreground(lipgloss.Color(p.theme.Bg)).Background(lipgloss.Color(p.theme.Accent))
	caps := make([]string, len(rows))
	capWidth := 0
	for i, r := range rows {
		label := presentation.KeyLabel(p.mode, r.key)
		switch {
		case r.spoken:
			caps[i] = p.accent.Render(presentation.GlyphsFor(p.mode).PromptGlyph)
		case r.outcome:
			caps[i] = p.accent.Render(presentation.GlyphsFor(p.mode).ReplyGlyph)
		case i == active && r.key != "":
			caps[i] = lit.Render(label)
		case r.typed:
			caps[i] = typed.Render(label)
		case r.key != "":
			caps[i] = p.cap.Render(label)
		}
		capWidth = max(capWidth, ansi.StringWidth(caps[i]))
	}
	var lines []string
	for i, r := range rows {
		if caps[i] == "" {
			continue
		}
		does := p.text
		switch {
		case i == active:
			does = p.bright
		case active >= 0:
			does = p.dim
		}
		text := r.does
		if r.spoken {
			text = "“" + text + "”"
		}
		lines = append(lines, "  "+presentation.PadRight(caps[i], capWidth)+"  "+does.Render(text), "")
	}
	return lines
}

// alsoLines name the other keys that do what a card's main flow does,
// quieter than the flow itself, one to a line. An unbound key is left out.
func alsoLines(p palette, step Step, keys Keys) []string {
	var pairs [][2]string
	switch step {
	case QuickPrompt:
		pairs = [][2]string{{keys.NewSession, "the full new-session form"}, {keys.NewGroup, "a new group"}}
	case Sessions:
		pairs = [][2]string{{keys.OtherFocus, "focuses too"}, {keys.OtherBack, "comes back from anywhere"}}
	}
	labels := make([]string, 0, len(pairs))
	width := 0
	for _, pair := range pairs {
		label := presentation.KeyLabel(p.mode, pair[0])
		labels = append(labels, label)
		width = max(width, ansi.StringWidth(label))
	}
	var lines []string
	for i, pair := range pairs {
		if pair[0] != "" {
			lines = append(lines, "  "+p.subtle.Render("or ")+p.accent.Render(presentation.PadRight(labels[i], width))+"  "+p.dim.Render(pair[1]))
		}
	}
	return lines
}

// OrchestrateAsk is what the lead agent is asked. The preview acts out the
// same words.
const OrchestrateAsk = "rebuild checkout, split it across agents"
