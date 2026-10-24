package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	tea "github.com/charmbracelet/bubbletea"
)

// The prompt box itself lives in ui/composer; what stays here is the root
// state it needs: which box a clipboard read belongs to, the generation that
// tells a reopened box from its predecessor, the painters the current theme
// gives it, and the pastes directory sweep.

// The root is the status bar a prompt box reports its paste and chip edits
// on, through composer.Host.
func (m *Model) ReportErr(text string) { m.reportErr(text) }

func (m *Model) ClearErr() { m.clearErr() }

// composerPaint is the prompt as the current theme paints it: the caret
// marker on its textarea, and the color of a filled chip and of one whose
// clipboard read is still running.
func composerPaint() composer.Paint {
	return composer.Paint{Input: uiframe.TextAreaView, Chip: uitheme.ImageChip, Pasting: uitheme.ImageChipPasting}
}

// nextComposerGen numbers a freshly opened prompt box, so a clipboard read
// still in flight can tell the box it was started in from its successor.
func (m *Model) nextComposerGen() int {
	m.ledger.composerSeq++
	return m.ledger.composerSeq
}

// composerFor is the prompt box a target names. A quick bar since closed
// is an empty stand-in whose generation no read carries, so a read that
// lands on it only drops its file.
func (m *Model) composerFor(target composer.ID) *composer.Composer {
	if target == composer.Form {
		if prompt := m.formPromptFor(); prompt != nil {
			return prompt
		}
	}
	if q := m.quick(); target == composer.Quick && q != nil {
		return &q.Composer
	}
	return &composer.Composer{}
}

// composerOpen reports whether that prompt box is still on screen, which
// is what says an image still has somewhere to land.
func (m *Model) composerOpen(target composer.ID) bool {
	if target == composer.Form {
		return m.form() != nil
	}
	return m.quick() != nil
}

// pasteRoot is the root as the prompt boxes reach it.
type pasteRoot struct{ *Model }

func (r pasteRoot) ComposerFor(target composer.ID) *composer.Composer { return r.m.composerFor(target) }

func (r pasteRoot) ComposerOpen(target composer.ID) bool { return r.m.composerOpen(target) }

func (r pasteRoot) ReportErr(text string) { r.m.reportErr(text) }

func (r pasteRoot) ClearErr() { r.m.clearErr() }

func (m *Model) routePasteMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	cmd, handled := composer.Update(pasteRoot{m}, msg)
	if !handled {
		return nil, nil, false
	}
	return routed(m, cmd)
}
