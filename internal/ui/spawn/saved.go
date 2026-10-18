package spawn

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Saved is a named prompt from the catalog: the text goes in front of a task.
type Saved struct {
	Name string
	Text string
}

// SavedList is the filterable list of saved prompts the form, the quick
// bar and a focused session open. The form's list offers none first.
type SavedList struct {
	open    bool
	none    bool
	filter  textinput.Model
	cursor  int
	rows    []Saved
	loading bool
	err     string
	// hits maps each painted line to the match a click there picks, or -1.
	hits []int
}

// SavedPick is what a list key or click did. Index is the picked row in
// the list's rows, or -1 for none.
type SavedPick struct {
	Closed bool
	Picked bool
	Index  int
	Saved  Saved
}

type savedMatch struct {
	index int
	saved Saved
}

// OpenSavedList opens on rows, or on a read still running when loading.
func OpenSavedList(rows []Saved, loading, none bool) SavedList {
	filter := textinput.New()
	filter.Placeholder = "filter"
	filter.CharLimit = 60
	filter.Focus()
	return SavedList{open: true, none: none, filter: filter, rows: rows, loading: loading}
}

// Open says the list is showing.
func (l *SavedList) Open() bool { return l.open }

// Loading says the list waits on its read.
func (l *SavedList) Loading() bool { return l.loading }

// Close hides the list and forgets its filter.
func (l *SavedList) Close() { *l = SavedList{} }

// Load fills a list that opened while its read was running.
func (l *SavedList) Load(rows []Saved, err error) {
	l.loading = false
	if err != nil {
		l.err = err.Error()
		return
	}
	l.rows, l.err = rows, ""
}

func (l *SavedList) matches() []savedMatch {
	query := strings.ToLower(strings.TrimSpace(l.filter.Value()))
	var matches []savedMatch
	if l.none && (query == "" || strings.Contains("none", query)) {
		matches = append(matches, savedMatch{index: -1, saved: Saved{Name: "none"}})
	}
	for i, row := range l.rows {
		if query != "" && !strings.Contains(strings.ToLower(row.Name), query) {
			continue
		}
		matches = append(matches, savedMatch{index: i, saved: row})
	}
	return matches
}

// Key filters, moves and picks. Every key the list does not use types into
// its filter.
func (l *SavedList) Key(msg tea.KeyMsg) (tea.Cmd, SavedPick) {
	switch msg.String() {
	case "esc", "ctrl+o":
		l.Close()
		return nil, SavedPick{Closed: true}
	case "up":
		l.cursor = max(0, l.cursor-1)
		return nil, SavedPick{}
	case "down":
		if matches := l.matches(); len(matches) > 0 {
			l.cursor = min(l.cursor+1, len(matches)-1)
		}
		return nil, SavedPick{}
	case "enter":
		return nil, l.pick()
	}
	var cmd tea.Cmd
	l.filter, cmd = l.filter.Update(msg)
	l.cursor = 0
	return cmd, SavedPick{}
}

func (l *SavedList) pick() SavedPick {
	matches := l.matches()
	if l.loading || l.err != "" || len(matches) == 0 {
		return SavedPick{}
	}
	match := matches[min(l.cursor, len(matches)-1)]
	l.Close()
	return SavedPick{Closed: true, Picked: true, Index: match.index, Saved: match.saved}
}

// Click picks the entry on a painted line.
func (l *SavedList) Click(line int) SavedPick {
	if line < 0 || line >= len(l.hits) || l.hits[line] < 0 {
		return SavedPick{}
	}
	l.cursor = l.hits[line]
	return l.pick()
}

// Lines paints the filter and up to visible-1 entries around the cursor,
// or the state the read is in.
func (l *SavedList) Lines(width, visible int, st Styles) []string {
	l.hits = l.hits[:0]
	note := func(text string) []string {
		l.hits = append(l.hits, -1)
		return []string{st.Muted.Render(ansi.Truncate(text, max(1, width), "…"))}
	}
	switch {
	case l.loading:
		return note("loading saved prompts…")
	case l.err != "":
		return note(l.err)
	}
	matches := l.matches()
	if len(matches) == 0 {
		return note("no saved prompts match")
	}
	l.cursor = max(0, min(l.cursor, len(matches)-1))
	lines := []string{st.Input(l.filter)}
	l.hits = append(l.hits, -1)
	visible = max(1, visible-1)
	first := max(0, l.cursor-visible+1)
	for i := first; i < min(len(matches), first+visible); i++ {
		marker, style := "  ", st.Muted
		if i == l.cursor {
			marker, style = st.Value.Render("❯ "), st.Value
		}
		lines = append(lines, ansi.Truncate(marker+style.Render(matches[i].saved.Name), max(1, width), "…"))
		l.hits = append(l.hits, i)
	}
	return lines
}

// Hint is the list's key hint, with what enter does to the pick.
func (l *SavedList) Hint(enter, esc string) [][2]string {
	return [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵", enter}, {"esc", esc}}
}

// SavedPreview is the text on one line, cut to width.
func SavedPreview(text string, width int) string {
	if width <= 0 {
		return ""
	}
	return ansi.Truncate(strings.Join(strings.Fields(text), " "), width, "…")
}
