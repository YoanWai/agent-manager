package help

import tea "github.com/charmbracelet/bubbletea"

// State owns the key map's search, scroll, and visible scope.
type State struct {
	scroll    int
	query     string
	searching bool
	scope     Scope
}

type Scope uint8

const (
	Global Scope = iota
	Review
)

type Action uint8

const (
	Stay Action = iota
	Close
	Quit
	OpenDocs
)

// Viewport is the catalog window currently available inside the dialog.
type Viewport struct {
	Rows  int
	Lines int
}

func New(scope Scope) State {
	return State{scope: scope}
}

func (s *State) close() {
	*s = State{}
}

func (s State) scrollLimit(viewport Viewport) int {
	return max(0, viewport.Lines-max(viewport.Rows, 1))
}

func (s *State) scrollBy(delta int, viewport Viewport) {
	s.scroll = min(max(s.scroll+delta, 0), s.scrollLimit(viewport))
}

// Searching reports whether typed keys go to the search.
func (s State) Searching() bool { return s.searching }

func (s State) page(viewport Viewport) int {
	return max(viewport.Rows-1, 1)
}

func (s *State) Update(msg tea.KeyMsg, viewport Viewport) Action {
	if msg.String() == "ctrl+c" {
		return Quit
	}
	if s.searching {
		s.updateSearch(msg, viewport)
		return Stay
	}
	switch msg.String() {
	case "esc":
		if s.query != "" {
			s.query = ""
			s.scroll = 0
			return Stay
		}
		s.close()
		return Close
	case "q", "?", "enter":
		s.close()
		return Close
	case "/":
		s.searching = true
	case "o":
		return OpenDocs
	case "up", "k":
		s.scrollBy(-1, viewport)
	case "down", "j":
		s.scrollBy(1, viewport)
	case "pgup", "ctrl+u":
		s.scrollBy(-s.page(viewport), viewport)
	case "pgdown", "ctrl+d":
		s.scrollBy(s.page(viewport), viewport)
	case "g", "home":
		s.scroll = 0
	case "G", "end":
		s.scroll = s.scrollLimit(viewport)
	}
	return Stay
}

// updateSearch types into the search. Scrolling stays live while it is up,
// so a query with more hits than the card can show remains readable.
func (s *State) updateSearch(msg tea.KeyMsg, viewport Viewport) {
	switch msg.String() {
	case "enter":
		s.searching = false
	case "esc":
		s.searching = false
		s.query = ""
		s.scroll = 0
	case "backspace":
		if runes := []rune(s.query); len(runes) > 0 {
			s.query = string(runes[:len(runes)-1])
			s.scroll = 0
		}
	case "up":
		s.scrollBy(-1, viewport)
	case "down":
		s.scrollBy(1, viewport)
	case "pgup":
		s.scrollBy(-s.page(viewport), viewport)
	case "pgdown":
		s.scrollBy(s.page(viewport), viewport)
	default:
		switch msg.Type {
		case tea.KeyRunes:
			s.query += string(msg.Runes)
			s.scroll = 0
		case tea.KeySpace:
			s.query += " "
			s.scroll = 0
		}
	}
}
