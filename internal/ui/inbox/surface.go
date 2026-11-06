package inbox

import (
	"strings"
	"time"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// Root queues the loads the card asks for. Each completion comes back
// through Loaded or BodyRead with the generation it was asked under.
type Root interface {
	LoadInbox(sessionID string, gen uint64)
	ReadInboxMessage(messageID int64, gen uint64)
}

// Surface is one session's inbox on the root's stack: its queue, then its
// latest messages, and one of them read in full.
type Surface struct {
	root      Root
	sessionID string
	name      string
	mode      presentation.GlyphMode
	now       func() time.Time

	loadGen uint64
	loading bool
	loaded  bool
	failed  string
	log     Log
	cursor  int
	first   int

	reading  bool
	readGen  uint64
	readOf   Entry
	body     string
	bodyDone bool
	offset   int
}

const cardMaxWidth = 96

// NewSurface opens the inbox of one session and asks for its first load.
func NewSurface(root Root, sessionID, name string, mode presentation.GlyphMode) *Surface {
	s := &Surface{root: root, sessionID: sessionID, name: name, mode: mode, now: time.Now}
	s.Reload()
	return s
}

func (s *Surface) SessionID() string { return s.sessionID }

// Reload asks for the log again, keeping what shows until it lands.
func (s *Surface) Reload() {
	s.loadGen++
	s.loading = true
	s.root.LoadInbox(s.sessionID, s.loadGen)
}

// Refresh reloads unless a load is already on its way, so a card left open
// across many polls never stacks them up.
func (s *Surface) Refresh() {
	if !s.loading {
		s.Reload()
	}
}

// Loaded takes a load the card asked for; a stale one is dropped. The
// cursor stays on the message it was on, which a delivery moves from the
// queue to the recent list.
func (s *Surface) Loaded(gen uint64, log Log, err error) {
	if gen != s.loadGen {
		return
	}
	s.loading, s.loaded = false, true
	if err != nil {
		s.failed = err.Error()
		return
	}
	picked := s.log.entries()
	s.failed, s.log = "", log
	entries := log.entries()
	if s.cursor < len(picked) {
		for index, entry := range entries {
			if entry.ID == picked[s.cursor].ID {
				s.cursor = index
				return
			}
		}
	}
	s.cursor = min(s.cursor, max(len(entries)-1, 0))
}

// BodyRead takes the text of the message being read.
func (s *Surface) BodyRead(gen uint64, body string, err error) {
	if !s.reading || gen != s.readGen {
		return
	}
	s.bodyDone = true
	s.body = body
	if err != nil {
		s.body = uitheme.ErrStyle().Render(err.Error())
	}
}

func cardWidth(cols int) int { return max(min(cardMaxWidth, cols-4), min(cols, 24)) }

func innerWidth(cols int) int { return max(uiframe.CardInnerWidth(cardWidth(cols)), 1) }

func bodyRows(rows int) int { return max(rows-8, 3) }

func (s *Surface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	_, rows := h.Size()
	if s.reading {
		s.readingKey(h, msg)
		return true, nil
	}
	entries := s.log.entries()
	switch msg.String() {
	case "esc":
		h.Pop()
	case "up", "k":
		s.cursor = max(s.cursor-1, 0)
	case "down", "j":
		s.cursor = min(s.cursor+1, max(len(entries)-1, 0))
	case "enter":
		s.open(entries)
	}
	s.follow(h, rows)
	return true, nil
}

func (s *Surface) readingKey(h surface.Host, msg tea.KeyMsg) {
	_, rows := h.Size()
	page := bodyRows(rows)
	switch msg.String() {
	case "esc":
		s.reading = false
	case "up", "k":
		s.offset--
	case "down", "j":
		s.offset++
	case "pgup":
		s.offset -= page
	case "pgdown":
		s.offset += page
	case "home":
		s.offset = 0
	case "end":
		s.offset = len(s.readLines(h))
	}
	s.offset = min(max(s.offset, 0), max(len(s.readLines(h))-page, 0))
}

func (s *Surface) open(entries []Entry) {
	if s.cursor >= len(entries) {
		return
	}
	s.reading, s.bodyDone, s.body, s.offset = true, false, "", 0
	s.readOf = entries[s.cursor]
	s.readGen++
	s.root.ReadInboxMessage(s.readOf.ID, s.readGen)
}

// follow scrolls the list just far enough to keep the cursor's lines shown.
func (s *Surface) follow(h surface.Host, rows int) {
	lines, owners := s.list(h)
	top, bottom := -1, -1
	for line, owner := range owners {
		if owner == s.cursor {
			if top < 0 {
				top = line
			}
			bottom = line
		}
	}
	if top < 0 {
		return
	}
	visible := bodyRows(rows)
	s.first = presentation.Follow(s.first, top, len(lines), visible)
	s.first = presentation.Follow(s.first, bottom, len(lines), visible)
	s.first = min(s.first, top)
}

func (s *Surface) list(h surface.Host) ([]string, []int) {
	cols, _ := h.Size()
	return listLines(s.log, s.cursor, innerWidth(cols), s.now(), s.mode)
}

func (s *Surface) readLines(h surface.Host) []string {
	cols, _ := h.Size()
	body := uitheme.SubtleStyle().Render("Loading the message…")
	if s.bodyDone {
		body = s.body
	}
	return bodyLines(s.readOf, body, innerWidth(cols), s.now(), s.mode)
}

// Click picks the message a click landed on, and reads it on a click on
// the one already picked.
func (s *Surface) Click(h surface.Host, line int) tea.Cmd {
	if s.reading {
		return nil
	}
	_, rows := h.Size()
	_, owners := s.list(h)
	at := clampFirst(s.first, len(owners), bodyRows(rows)) + line
	if line < 0 || at >= len(owners) || owners[at] < 0 {
		return nil
	}
	if owners[at] == s.cursor {
		s.open(s.log.entries())
		return nil
	}
	s.cursor = owners[at]
	return nil
}

func (s *Surface) Wheel(h surface.Host, arrow tea.KeyMsg) tea.Cmd {
	_, cmd := s.Key(h, arrow)
	return cmd
}

func (s *Surface) View(h surface.Host, _ string) string {
	cols, rows := h.Size()
	visible := bodyRows(rows)
	title := presentation.GlyphsFor(s.mode).InboxGlyph + " Inbox: " + presentation.EscapeControlsInline(s.name)
	subtle := uitheme.SubtleStyle()
	var lines []string
	switch {
	case s.reading:
		lines = presentation.Window(s.readLines(h), visible, s.offset, subtle, s.mode)
	case !s.loaded:
		lines = []string{subtle.Render("Loading messages…")}
	case s.failed != "":
		lines = []string{uitheme.ErrStyle().Render(s.failed)}
	case len(s.log.entries()) == 0:
		lines = []string{uitheme.MutedStyle().Render("No messages to or from this session in the last day.")}
	default:
		list, _ := s.list(h)
		lines = windowFrom(list, visible, s.first)
	}
	return h.CardSized(cardWidth(cols), title, strings.Join(lines, "\n"), s.hints())
}

// windowFrom shows rows lines from first, the same rows Click counts from.
func windowFrom(lines []string, rows, first int) []string {
	first = clampFirst(first, len(lines), rows)
	return lines[first:min(first+rows, len(lines))]
}

func clampFirst(first, count, rows int) int { return min(max(first, 0), max(count-rows, 0)) }

func (s *Surface) hints() [][2]string {
	if s.reading {
		return [][2]string{{presentation.NavigationLabel(s.mode, "↑↓"), "scroll"}, {"esc", "back"}}
	}
	return [][2]string{{presentation.NavigationLabel(s.mode, "↑↓"), "pick"}, {presentation.KeyLabel(s.mode, "↵"), "read"}, {"esc", "close"}}
}

// Footer is empty: the card names its own keys on its bottom edge.
func (s *Surface) Footer() [][2]string { return nil }

func (s *Surface) Owner() surface.Owner { return surface.Manager }

func (s *Surface) Dismissible() bool { return true }

func (s *Surface) Leave(surface.Host) {}
