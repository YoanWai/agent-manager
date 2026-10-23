package ui

import (
	"strings"

	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// launchHintSurface is the setup-step card a refused launch opens: the text,
// the command that unblocks it, the images the refused prompt named. It
// holds the fix; the install it starts outlives the card (it closes once
// the command is typed into a shell row), so that lives on the ledger.
type launchHintSurface struct {
	fix    launchFix
	glyphs presentation.GlyphSet
}

// launchHintHost is what the card reaches on the root beyond the stack.
type launchHintHost interface {
	Pop()
	reportErr(text string)
	startInstall(request installStartRequest) bool
	cardWidth() int
	cardChromeRows(hint [][2]string) int
	CardSized(width int, title, body string, hint [][2]string) string
	Size() (width, height int)
	pendingInstall() *pendingInstall
}

func newLaunchHintSurface(fix launchFix, glyphs presentation.GlyphSet) *launchHintSurface {
	return &launchHintSurface{fix: fix, glyphs: glyphs}
}

// launchHintFix is the fix the open card holds, for a completion that
// attaches the launch to retry to it.
func (m *Model) launchHintFix() (*launchFix, bool) {
	s, ok := surfaceOf[*launchHintSurface](m)
	if !ok {
		return nil, false
	}
	return &s.fix, true
}

// closeLaunchHint takes the card down once its install has the screen,
// without dropping the images the install now owns.
func (m *Model) closeLaunchHint() {
	if fix, ok := m.launchHintFix(); ok {
		*fix = launchFix{}
	}
	m.clearSurfaces()
	m.base = baseList
}

// prompt is the fix as the card shows it; its keys and card are
// uidialogs.LaunchHint.
func (s *launchHintSurface) prompt() uidialogs.LaunchHint {
	return uidialogs.LaunchHint{Text: s.fix.text, Command: s.fix.command, Update: s.fix.update}
}

func (s *launchHintSurface) card(width int) uidialogs.Card {
	tone := lipgloss.NewStyle().Foreground(colorAccent()).Bold(true)
	return s.prompt().View(cardInnerWidth(width), tone, mutedStyle(), s.glyphs)
}

func (s *launchHintSurface) Key(h surfaceHost, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.run(h, s.prompt().Key(msg))
}

func (s *launchHintSurface) Click(h surfaceHost, line int) tea.Cmd {
	host := h.(launchHintHost)
	width := host.cardWidth()
	card := s.card(width)
	return s.run(h, s.prompt().Click(line+s.droppedRows(host, card), cardInnerWidth(width)))
}

// Wheel does nothing: there is nothing to scroll.
func (s *launchHintSurface) Wheel(surfaceHost, tea.KeyMsg) tea.Cmd { return nil }

func (s *launchHintSurface) View(h surfaceHost, _ string) string {
	host := h.(launchHintHost)
	width := host.cardWidth()
	card := s.card(width)
	body := card.Body
	if dropped := s.droppedRows(host, card); dropped > 0 {
		body = strings.Join(strings.Split(body, "\n")[dropped:], "\n")
	}
	return host.CardSized(width, card.Title, body, card.Hints)
}

// droppedRows is how many leading body rows a short terminal clips away,
// which a click's row index has to skip as well.
func (s *launchHintSurface) droppedRows(h launchHintHost, card uidialogs.Card) int {
	_, height := h.Size()
	if height <= 0 {
		return 0
	}
	limit := max(0, height-h.cardChromeRows(card.Hints))
	return max(0, strings.Count(card.Body, "\n")+1-limit)
}

func (s *launchHintSurface) Footer() [][2]string { return s.card(64).Hints }

func (s *launchHintSurface) Owner() inputOwner { return ownerManager }

// Dismissible: there is no draft, so a click off the card and ctrl+c close
// it the way esc does.
func (s *launchHintSurface) Dismissible() bool { return true }

// Leave drops the images the refused prompt was holding: they stayed alive
// while an install could still spawn it.
func (s *launchHintSurface) Leave(surfaceHost) {
	dropImages(s.fix.images)
	s.fix = launchFix{}
}

func (s *launchHintSurface) run(h surfaceHost, action uidialogs.LaunchHintAction) tea.Cmd {
	switch action {
	case uidialogs.LaunchHintCopy:
		command := s.fix.command
		return func() tea.Msg {
			return launchCommandCopiedMsg{err: copyLaunchCommand(command)}
		}
	case uidialogs.LaunchHintInstall:
		s.startInstall(h.(launchHintHost))
	case uidialogs.LaunchHintClose:
		h.Pop()
	}
	return nil
}

// startInstall captures the setup dialog's install and hands it to the
// root, which queues its filesystem, store, lifecycle, and tmux work. The
// card stays in front until that job says the command was typed into a
// durable shell row; the images move to the request once it is admitted.
func (s *launchHintSurface) startInstall(h launchHintHost) {
	if install := h.pendingInstall(); install != nil {
		h.reportErr("an install is already running in " + install.name)
		return
	}
	fix := s.fix
	retry := fix.effectRetry
	if spawn, ok := retry.(spawnRequest); ok && len(spawn.images) == 0 {
		spawn.images = append([]imageAttachment(nil), fix.images...)
		retry = spawn
	}
	if h.startInstall(installStartRequest{
		command:     fix.script,
		binary:      fix.binary,
		effectRetry: retry,
		images:      fix.images,
	}) {
		s.fix.images = nil
	}
}
