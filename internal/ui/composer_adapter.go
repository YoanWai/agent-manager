package ui

import (
	"github.com/YoanWai/agent-manager/internal/ui/composer"
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
	return composer.Paint{Input: textAreaView, Chip: imageChip, Pasting: imageChipPasting}
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

func (m *Model) handlePasteImageMsg(msg composer.PasteImageMsg) (tea.Model, tea.Cmd) {
	return m, m.composerFor(msg.Target).AcceptImage(m, msg, m.composerOpen(msg.Target))
}

func (m *Model) handlePasteTextMsg(msg composer.PasteTextMsg) (tea.Model, tea.Cmd) {
	return m, m.composerFor(msg.Target).AcceptText(msg, m.composerOpen(msg.Target))
}

func (m *Model) routePasteMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pasteSweepMsg:
		if msg.err != nil {
			m.reportErr("clearing old pasted images: " + msg.err.Error())
		}
		return routed(m, nil)

	case pasteSweepTickMsg:
		return routed(m, tea.Batch(m.sweepPastes, m.pasteSweepTick()))

	case composer.PasteImageMsg:
		return routed(m.handlePasteImageMsg(msg))

	case composer.PasteTextMsg:
		return routed(m.handlePasteTextMsg(msg))
	}
	return nil, nil, false
}
