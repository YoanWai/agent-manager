package dialogs

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/YoanWai/agent-manager/internal/ui/surface"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LaunchHintSurface is the setup-step card a refused launch opens: the text,
// the command that unblocks it, the images the refused prompt named. It holds
// the copy of the fix it paints; the launch to retry, the images and the
// install outlive the card, so those stay on the root.
type LaunchHintSurface struct {
	fix    LaunchFix
	glyphs presentation.GlyphSet
	tone   lipgloss.Style
	muted  lipgloss.Style
	root   LaunchHintRoot
}

// LaunchFix is a refused launch's fix as the card paints it. Command is empty
// when the manager knows no recipe, which leaves the card read-only; Update
// marks a CLI that is installed but too old, where the same installer updates
// it.
type LaunchFix struct {
	Text    string
	Command string
	Update  bool
}

// LaunchHintRoot is what the card reaches on the root beyond surface.Host:
// the install a key starts and the images the fix was holding.
type LaunchHintRoot interface {
	// StartInstall admits the install for the fix on screen onto the root's
	// lane and says whether it took it.
	StartInstall() bool
	// CopyLaunchCommand copies the command on screen and reports the
	// outcome.
	CopyLaunchCommand() tea.Cmd
	CardWidth() int
	CardChromeRows(hint [][2]string) int
	ReleaseLaunchHint()
}

// NewLaunchHintSurface opens the card for a refused launch, its first
// paragraph in tone and the rest muted.
func NewLaunchHintSurface(root LaunchHintRoot, fix LaunchFix, glyphs presentation.GlyphSet, tone, muted lipgloss.Style) *LaunchHintSurface {
	return &LaunchHintSurface{fix: fix, glyphs: glyphs, tone: tone, muted: muted, root: root}
}

func (s *LaunchHintSurface) Fix() LaunchFix { return s.fix }

func (s *LaunchHintSurface) SetFix(fix LaunchFix) { s.fix = fix }

// prompt is the fix as the card shows it; its keys and layout are
// LaunchHint.
func (s *LaunchHintSurface) prompt() LaunchHint {
	return LaunchHint{Text: s.fix.Text, Command: s.fix.Command, Update: s.fix.Update}
}

func (s *LaunchHintSurface) card(width int) Card {
	return s.prompt().View(CardInnerWidth(width), s.tone, s.muted, s.glyphs)
}

func (s *LaunchHintSurface) Key(h surface.Host, msg tea.KeyMsg) (bool, tea.Cmd) {
	return true, s.run(h, s.prompt().Key(msg))
}

func (s *LaunchHintSurface) Click(h surface.Host, line int) tea.Cmd {
	width := s.root.CardWidth()
	card := s.card(width)
	return s.run(h, s.prompt().Click(line+s.droppedRows(h, card), CardInnerWidth(width)))
}

// Wheel does nothing: there is nothing to scroll.
func (s *LaunchHintSurface) Wheel(surface.Host, tea.KeyMsg) tea.Cmd { return nil }

func (s *LaunchHintSurface) View(h surface.Host, _ string) string {
	width := s.root.CardWidth()
	card := s.card(width)
	body := card.Body
	if dropped := s.droppedRows(h, card); dropped > 0 {
		body = strings.Join(strings.Split(body, "\n")[dropped:], "\n")
	}
	return h.CardSized(width, card.Title, body, card.Hints)
}

// droppedRows is how many leading body rows a short terminal clips away,
// which a click's row index has to skip as well.
func (s *LaunchHintSurface) droppedRows(h surface.Host, card Card) int {
	_, height := h.Size()
	if height <= 0 {
		return 0
	}
	limit := max(0, height-s.root.CardChromeRows(card.Hints))
	return max(0, strings.Count(card.Body, "\n")+1-limit)
}

func (s *LaunchHintSurface) Footer() [][2]string { return s.prompt().Hints() }

func (s *LaunchHintSurface) Owner() surface.Owner { return surface.Manager }

// Dismissible: there is no draft, so a click off the card and ctrl+c close
// it the way esc does.
func (s *LaunchHintSurface) Dismissible() bool { return true }

// Leave drops the images the refused prompt was holding: they stayed alive
// while an install could still spawn it.
func (s *LaunchHintSurface) Leave(surface.Host) {
	s.root.ReleaseLaunchHint()
	s.fix = LaunchFix{}
}

func (s *LaunchHintSurface) run(h surface.Host, action LaunchHintAction) tea.Cmd {
	switch action {
	case LaunchHintCopy:
		return s.root.CopyLaunchCommand()
	case LaunchHintInstall:
		s.startInstall()
	case LaunchHintClose:
		h.Pop()
	}
	return nil
}

// startInstall hands the fix on screen to the root, which queues its
// filesystem, store, lifecycle and tmux work. The card stays in front until
// that job says the command was typed into a durable shell row.
func (s *LaunchHintSurface) startInstall() { s.root.StartInstall() }
