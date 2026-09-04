package ui

import (
	"fmt"
	"slices"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The key map is the one place every binding in the app is written down, so
// it is grouped the way the keys are learned - by what is under the cursor
// or which screen is up - rather than listed alphabetically. Long enough to
// outgrow a terminal, it scrolls, and a search narrows it to the one line
// the reader came for.

type helpState struct {
	scroll     int
	query      string
	searching  bool
	returnMode mode
}

// helpSection is one context's bindings, titled for the thing the keys act
// on.
type helpSection struct {
	title string
	rows  [][2]string
}

// helpKeyColumn is the width the key column is padded to: the widest key in
// the whole catalog plus a gap. Measured rather than fixed, so a binding
// added later cannot render clipped against its own description, and always
// over the whole catalog rather than a search's hits, so narrowing the map
// does not shift the column under the reader.
func helpKeyColumn() int {
	width := 0
	for _, section := range helpSections() {
		for _, row := range section.rows {
			if w := ansi.StringWidth(row[0]); w > width {
				width = w
			}
		}
	}
	return width + 2
}

// helpCardMaxWidth keeps the card readable on a wide terminal: past this the
// eye has to travel too far from a key to its description.
const helpCardMaxWidth = 92

func helpSections() []helpSection {
	return []helpSection{
		{title: "list", rows: [][2]string{
			{"", "Tell your agent to manage sessions and terminals in Agent Manager."},
			{"↑↓ / jk", "move the cursor"},
			{"→", "step in: focus the session, open the group"},
			{"←", "step out: close the group"},
			{"K / J", "reorder the row up / down (shift+↑↓ too)"},
			{"n", "new session"},
			{"T", "new terminal tab: a shell under the selected agent, or in the group"},
			{"g", "new group (name, parent, default path, worktree)"},
			{"/", "search the list by name"},
			{"w", "filter to what needs attention (waiting, finished, errored)"},
			{"t", "archived view"},
			{"e", "hide / show empty groups"},
			{"F", "fold / unfold every group"},
			{"|", "resize the split (←→ or hl, or drag; ↵ commits, esc cancels)"},
			{"s", "settings"},
			{"M", "messages"},
			{"?", "this key map"},
			{"q", "quit (sessions keep running)"},
		}},
		{title: "session under the cursor", rows: [][2]string{
			{"↵", "focus it: keys reach the agent, the list stays on screen"},
			{"A", "attach it: the pane takes the whole terminal"},
			{"", "settings swap what ↵ and A do"},
			{".", "mark it idle when it is finished, without entering it"},
			{"space", "quick prompt: answer the session without attaching"},
			{"ctrl+r", "review its diff"},
			{"f", "fork it into a new session in the same group"},
			{"r", "rename it, and re-pick its tool"},
			{"m", "move it to a group, or a terminal into a session"},
			{"o", "open its working directory in your editor"},
			{"R", "restart it on an empty context (same name, group, dir, tool)"},
			{"x / X", "kill it / kill every live session (frees their RAM)"},
			{"v / V", "revive it, its pane included / revive every dead session"},
			{"y", "copy its newest reply to the clipboard"},
			{"a / u", "archive / restore (archive kills, restore revives)"},
			{"d", "delete it"},
		}},
		{title: "the mark on a session row", rows: [][2]string{
			{"◐ working", "the agent is busy on a turn"},
			{"◆ waiting", "blocked on you: a dialog, a permission ask, a question"},
			{"● finished", "the turn ended; entering the session clears it to idle"},
			{"○ idle", "nothing running"},
			{"✕ errored", "the tool reported an error, or the session is dead"},
			{"◌ starting", "the pane is still launching"},
			{"", "w filters the list down to the marks that need you"},
		}},
		{title: "group under the cursor", rows: [][2]string{
			{"↵", "fold / unfold"},
			{"space", "quick prompt: spawn a new agent in the group"},
			{"r", "edit it: name, default path, worktree"},
			{"m", "move it, with its whole subtree, under another group"},
			{"o", "open its default path in your editor"},
			{"x / X", "kill every live session in it / everywhere"},
			{"v / V", "revive every dead session in it / everywhere"},
			{"a / u", "archive / restore the subtree (archive kills, restore revives)"},
			{"d", "delete the group and its subtree"},
		}},
		{title: "quick prompt (space)", rows: [][2]string{
			{"↵", "send"},
			{"↑↓", "switch the target session"},
			{"tab", "switch the tool a spawn uses (alt+m too)"},
			{"shift+tab", "toggle worktree for the spawned agent (alt+w too)"},
			{"ctrl+v", "paste an image as a chip at the cursor"},
			{"⌫", "next to a chip, delete the whole chip"},
			{"←→", "step over a chip as one token"},
			{"esc", "close"},
		}},
		{title: "inside a session (attached or focused)", rows: [][2]string{
			{"typing", "goes straight to the agent, q included"},
			{"ctrl+q", "back to the manager (ctrl+\\ too)"},
			{"←", "focused: back to the manager, at the prompt's start"},
			{"ctrl+r", "review the session's diff, esc returns"},
			{"f3", "open its directory in an editor"},
			{"wheel", "focused: scroll the pane's history, type to catch up"},
			{"drag", "focused: select pane text and copy it"},
			{"double click", "focused: copy the word"},
			{"triple click", "focused: copy the line"},
			{"click", "focused: open the link under it, else a tracking agent gets it"},
			{"alt+drag", "focused: pass a whole drag to that agent UI"},
		}},
		reviewHelpSection(),
		{title: "messages (M)", rows: [][2]string{
			{"↑↓", "pick a message"},
			{"pgup / pgdn", "scroll its body"},
			{"↵", "open its link in the browser"},
			{"u", "on an update message: update and restart"},
			{"r", "refresh releases and messages"},
			{"x", "dismiss it for good"},
			{"esc", "close"},
		}},
		{title: "settings (s)", rows: [][2]string{
			{"↑↓", "pick a field"},
			{"←→", "change the value"},
			{"↵", "run the field's action (CLIs, report, suggest, update)"},
			{"esc", "save and close"},
		}},
		{title: "dialogs (n, g, r, f, m)", rows: [][2]string{
			{"tab / ↑↓", "next field"},
			{"ctrl+v", "in a prompt field, paste an image as a chip"},
			{"←→", "change a picker's value"},
			{"↵", "confirm"},
			{"esc", "cancel"},
		}},
	}
}

func reviewHelpSection() helpSection {
	return helpSection{title: "review (ctrl+r)", rows: [][2]string{
		{"", "Tell your agent what to review in Agent Manager; they set it up."},
		{"c", "comment on the line"},
		{"d", "remove a draft, or mark sent feedback handled / open"},
		{"C", "send drafts to the agent as the next review round"},
		{"space", "mark the file reviewed"},
		{"f", "code files only (hides images, assets, lock files)"},
		{"s", "cycle the scope (uncommitted / vs target / last commit / staged)"},
		{"r", "pick the repo when the session dir holds several"},
		{"b", "switch to another worktree, listed by branch"},
		{"B", "pick the target branch the branch diff compares against"},
		{"↑↓ / jk", "move a line"},
		{"ctrl+d / ctrl+u", "half a page"},
		{"pgup / pgdn", "page"},
		{"g / G", "top / bottom of the file"},
		{"tab / shift+tab", "next / previous file (J / K too)"},
		{"n / N", "next / previous change"},
		{"u", "unified / split layout"},
		{"o / f3", "open the current file in your editor"},
		{"?", "this key map"},
		{"esc / q", "close the review"},
	}}
}

func (m *Model) visibleHelpSections() []helpSection {
	if m.help.returnMode == modeDiff {
		return []helpSection{reviewHelpSection()}
	}
	sections := helpSections()
	if m.arrowStep {
		return sections
	}
	for i := range sections {
		sections[i].rows = slices.DeleteFunc(sections[i].rows, func(row [2]string) bool {
			return row[0] == "→" || row[0] == "←"
		})
	}
	return sections
}

// matchHelp narrows the catalog to the rows whose key or description
// contains the query, dropping the sections left with nothing. A section
// whose own title matches keeps all of its rows: "review" should answer
// with the review screen, not with the two rows spelling the word.
func matchHelp(sections []helpSection, query string) []helpSection {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return sections
	}
	var kept []helpSection
	for _, section := range sections {
		if strings.Contains(strings.ToLower(section.title), query) {
			kept = append(kept, section)
			continue
		}
		var rows [][2]string
		for _, row := range section.rows {
			if strings.Contains(strings.ToLower(row[0]), query) ||
				strings.Contains(strings.ToLower(row[1]), query) {
				rows = append(rows, row)
			}
		}
		if len(rows) > 0 {
			kept = append(kept, helpSection{title: section.title, rows: rows})
		}
	}
	return kept
}

func helpRowCount(sections []helpSection) int {
	count := 0
	for _, section := range sections {
		for _, row := range section.rows {
			if row[0] != "" {
				count++
			}
		}
	}
	return count
}

// helpBodyLines lays the catalog out as one scrollable column: a titled rule
// per section, then its bindings in two aligned columns.
func helpBodyLines(sections []helpSection, width int, query string) []string {
	keyColumn := helpKeyColumn()
	if room := width / 3; keyColumn > room {
		keyColumn = max(room, 4)
	}
	var lines []string
	for i, section := range sections {
		if i > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, divider(section.title, width))
		for _, row := range section.rows {
			// A row with no key is a note about the one above it, so it
			// recedes into the description column instead of claiming a
			// binding of its own.
			if row[0] == "" {
				lines = append(lines, strings.Repeat(" ", keyColumn)+
					subtleStyle.Render(ansi.Truncate(row[1], max(width-keyColumn, 1), "…")))
				continue
			}
			key := padRight(keyStyle.Render(row[0]), keyColumn)
			lines = append(lines, key+highlightMatch(row[1], query, width-keyColumn))
		}
	}
	return lines
}

// highlightMatch renders a description with the matched run picked out, so
// a search lands the eye on the word it found instead of on the row.
func highlightMatch(text, query string, width int) string {
	text = ansi.Truncate(text, max(width, 1), "…")
	query = strings.TrimSpace(query)
	if query == "" {
		return mutedStyle.Render(text)
	}
	// Case folding can change a string's byte length, so the run to paint is
	// as long as the folded query, and a fold that moved the offsets past the
	// original drops the highlight instead of slicing out of range.
	folded := strings.ToLower(query)
	at := strings.Index(strings.ToLower(text), folded)
	if at < 0 || at+len(folded) > len(text) {
		return mutedStyle.Render(text)
	}
	hit := lipgloss.NewStyle().Foreground(colorBright).Bold(true)
	return mutedStyle.Render(text[:at]) + hit.Render(text[at:at+len(folded)]) +
		mutedStyle.Render(text[at+len(folded):])
}

func helpCardWidth(terminalWidth int) int {
	width := helpCardMaxWidth
	if terminalWidth >= 28 && width > terminalWidth-4 {
		width = terminalWidth - 4
	}
	return width
}

// helpBodyRoom is the rows of catalog the card can show: the frame's own
// chrome, the search line when one is up, and the error row come off the
// terminal's height first.
func (m *Model) helpBodyRoom() int {
	inner := cardInnerWidth(helpCardWidth(m.width))
	// Title, the blank under it, the blank above the rule, the rule itself,
	// the hint - which wraps on a narrow card - and the bottom rule.
	room := m.height - 5 - lipgloss.Height(legendInline(m.helpHint(), inner))
	if m.helpSearchActive() {
		room -= 2
	}
	if m.errBar.text != "" {
		room -= 2
	}
	return max(room, 1)
}

func (m *Model) helpSearchActive() bool {
	return m.help.searching || m.help.query != ""
}

func (m *Model) helpScrollLimit() int {
	sections := matchHelp(m.visibleHelpSections(), m.help.query)
	body := helpBodyLines(sections, cardInnerWidth(helpCardWidth(m.width)), m.help.query)
	return max(0, len(body)-m.helpBodyRoom())
}

func (m *Model) viewHelp() string {
	width := helpCardWidth(m.width)
	inner := cardInnerWidth(width)
	sections := matchHelp(m.visibleHelpSections(), m.help.query)

	var head []string
	if m.helpSearchActive() {
		head = append(head, m.helpSearchLine(sections), "")
	}

	body := helpBodyLines(sections, inner, m.help.query)
	if len(body) == 0 {
		body = []string{subtleStyle.Render("no key matches that")}
	}
	lines := append(head, fitBody(body, m.helpBodyRoom(), m.help.scroll)...)

	title := "? Keys"
	if m.help.returnMode == modeDiff {
		title = "? Review keys"
	}
	return m.cardSized(width, title, strings.Join(lines, "\n"), m.helpHint())
}

// helpSearchLine is the search's own row: what was typed, and how much of
// the map still answers to it.
func (m *Model) helpSearchLine(sections []helpSection) string {
	line := keyStyle.Render("search ") + valueStyle.Render(m.help.query)
	if m.help.searching {
		line += lipgloss.NewStyle().Foreground(colorAccent).Render("▏")
	}
	count := helpRowCount(sections)
	label := " keys"
	if count == 1 {
		label = " key"
	}
	return line + subtleStyle.Render(fmt.Sprintf("   %d%s", count, label))
}

func (m *Model) helpHint() [][2]string {
	if m.help.searching {
		return [][2]string{{"type", "search"}, {"↵", "done"}, {"↑↓", "scroll"}, {"esc", "clear"}}
	}
	if m.help.query != "" {
		return [][2]string{{"↑↓/jk", "scroll"}, {"/", "search"}, {"esc", "clear search"}, {"q", "close"}}
	}
	return [][2]string{
		{"↑↓/jk", "scroll"}, {"pgup/pgdn", "page"}, {"g/G", "top/bottom"},
		{"/", "search"}, {"esc/q", "close"},
	}
}

func (m *Model) openHelp() {
	m.help = helpState{returnMode: m.mode}
	m.mode = modeHelp
}

func (m *Model) closeHelp() {
	back := m.help.returnMode
	m.help = helpState{}
	m.mode = back
}

func (m *Model) scrollHelp(delta int) {
	m.help.scroll = min(max(m.help.scroll+delta, 0), m.helpScrollLimit())
}

func (m *Model) helpPage() int {
	return max(m.helpBodyRoom()-1, 1)
}

func (m *Model) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m, tea.Quit
	}
	if m.help.searching {
		return m.handleHelpSearchKey(msg)
	}
	switch msg.String() {
	case "esc":
		if m.help.query != "" {
			m.help.query = ""
			m.help.scroll = 0
			return m, nil
		}
		m.closeHelp()
		return m, m.startStartupTick()
	case "q", "?", "enter":
		m.closeHelp()
		return m, m.startStartupTick()
	case "/":
		m.help.searching = true
	case "up", "k":
		m.scrollHelp(-1)
	case "down", "j":
		m.scrollHelp(1)
	case "pgup", "ctrl+u":
		m.scrollHelp(-m.helpPage())
	case "pgdown", "ctrl+d":
		m.scrollHelp(m.helpPage())
	case "g", "home":
		m.help.scroll = 0
	case "G", "end":
		m.help.scroll = m.helpScrollLimit()
	}
	return m, nil
}

// handleHelpSearchKey types into the search. Scrolling stays live while it
// is up, so a query with more hits than the card can show is still readable
// without leaving the field.
func (m *Model) handleHelpSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.help.searching = false
	case "esc":
		m.help.searching = false
		m.help.query = ""
		m.help.scroll = 0
	case "backspace":
		if runes := []rune(m.help.query); len(runes) > 0 {
			m.help.query = string(runes[:len(runes)-1])
			m.help.scroll = 0
		}
	case "up":
		m.scrollHelp(-1)
	case "down":
		m.scrollHelp(1)
	case "pgup":
		m.scrollHelp(-m.helpPage())
	case "pgdown":
		m.scrollHelp(m.helpPage())
	default:
		switch msg.Type {
		case tea.KeyRunes:
			m.help.query += string(msg.Runes)
			m.help.scroll = 0
		case tea.KeySpace:
			m.help.query += " "
			m.help.scroll = 0
		}
	}
	return m, nil
}
