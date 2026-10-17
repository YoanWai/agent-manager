package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/diff"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/charmbracelet/lipgloss"
)

// reviewRenderContext copies the active theme into the values Review paints
// with. It is built per render so a theme change reaches the next frame.
func (m *Model) reviewRenderContext() uireview.RenderContext {
	return uireview.RenderContext{
		Styles: uireview.Styles{
			Muted: mutedStyle(), Subtle: subtleStyle(), Value: valueStyle(), Err: errStyle(), Done: doneStyle(),
			Annotation: annotationStyle(), Section: sectionStyle(), Rule: lipgloss.NewStyle().Foreground(colorBorder()),
			Chip: chipStyle(), Selected: selectedRowStyle(),
			Accent: colorAccent(), Accent2: colorAccent2(), Finished: colorFinished(), Errored: colorErrored(),
			SelectedReapply: "\x1b[0m" + bgSeq(current().Surface) + fgSeq(current().Bright),
			AnnotationBg:    annotationBg(), HandledAnnotationBg: handledAnnotationBg(),
		},
		CursorMarker: cursorAnchorMarker,
	}
}

func (m *Model) currentFileDiff() *diff.FileDiff {
	fd, ok := m.review.CurrentFile()
	if !ok {
		return nil
	}
	return &fd
}

func (m *Model) diffFileHidden(fd *diff.FileDiff) bool { return m.review.FileHidden(fd) }

func (m *Model) diffCodeHeight() int {
	height := m.layout.height - 6 - lipgloss.Height(m.viewDiffFooter())
	if m.review.Snapshot().Annotating {
		height -= m.diffAnnBarRows() + 1
	}
	height -= 2
	if height < 1 {
		height = 1
	}
	return height
}

func (m *Model) diffAnnBarRows() int {
	if !m.review.Snapshot().Annotating {
		return 0
	}
	_, codeWidth := m.diffPaneWidths()
	return 1 + m.review.AnnotationInputHeight(codeWidth-2*contentGutter)
}

func (m *Model) diffPaneWidths() (fileWidth, codeWidth int) {
	return uireview.PaneWidths(m.layout.width)
}

// prepareReviewLayout performs the only Review layout mutations before View.
func (m *Model) prepareReviewLayout() {
	if m.mode != modeDiff || m.layout.width <= 0 || m.layout.height <= 0 {
		return
	}
	footer := m.viewDiffFooter()
	bodyHeight := m.layout.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 5 {
		bodyHeight = 5
	}
	_, codeWidth := m.diffPaneWidths()
	width, height := codeWidth-2*contentGutter, bodyHeight-2
	if width < 1 {
		width = 1
	}
	m.review.PrepareLayout(m.reviewRenderContext(), width, height)
}

func (m *Model) reviewRing(label string, width, height int) string {
	return strings.Join(ringLoader(m.glyphs(), width, height, label, m.startup.startupPhase), "\n")
}

func (m *Model) reviewSpinnerLine(label string) string {
	frame := m.glyphs().StartupFrame(m.startup.startupPhase)
	return mutedStyle().Render(frame + " " + label)
}

// viewDiffFull is the full-screen review mode: file list on the left,
// whole-file code on the right, annotation bar docked when typing.
func (m *Model) viewDiffFull() string {
	sess, _ := m.diffSession()
	footer := m.viewDiffFooter()
	bodyHeight := m.layout.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	// Two columns go to the seam and the fill's bleed edge between the
	// two surfaces.
	fileWidth, codeWidth := m.diffPaneWidths()

	// Files sit on the rail surface, the code on the backdrop: the same
	// two-surface split the session list uses, so review reads as the same
	// application rather than a second one.
	fileLines := append([]string{"", strings.Repeat(" ", railInset) + subtleStyle().Render("files")},
		indentLines(splitLines(m.viewDiffFileList(fileWidth-2*railGutter, bodyHeight-2)), railInset)...)
	codeLines := append([]string{"", "  " + subtleStyle().Render(escapeControlsInline(m.review.CodeTitle()))},
		indentLines(splitLines(m.viewDiffCode(codeWidth-2*contentGutter, bodyHeight-2)), contentGutter)...)

	// The column seam tees into the rules that open and close the body,
	// same as the list view, so the two screens share one frame language.
	frame := []string{
		paint(m.viewDiffHeader(sess.Name), m.layout.width, backdropHex()),
		m.boundedRuleRow(fileWidth+1, m.layout.width, "▀"),
	}
	edge := make([]string, bodyHeight)
	for i := range edge {
		edge[i] = railEdgeCell(panelHex())
	}
	frame = append(frame, joinColumns(
		edge,
		paintRows(fileLines, fileWidth-1, bodyHeight, panelHex()),
		m.vruleColumn(bodyHeight),
		m.bleedColumn(bodyHeight),
		paintRows(codeLines, codeWidth, bodyHeight, backdropHex()),
	)...)
	frame = append(frame,
		m.boundedRuleRow(fileWidth+1, m.layout.width, "▄"),
		paint(m.viewDiffStatus(), m.layout.width, backdropHex()),
	)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.layout.width, backdropHex()))
	}
	return strings.Join(frame, "\n")
}

func (m *Model) viewDiffHeader(sessName string) string {
	return m.review.Header(m.reviewRenderContext(), sessName, m.layout.width)
}

func (m *Model) viewDiffFileList(width, height int) string {
	pane := m.review.FileList(m.reviewRenderContext(), width, height)
	if pane.Loading != "" {
		return m.reviewSpinnerLine(pane.Loading)
	}
	return pane.Text
}

func (m *Model) viewDiffCode(width, height int) string {
	pane := m.review.Code(m.reviewRenderContext(), width, height)
	if pane.Loading != "" {
		return m.reviewRing(pane.Loading, width, height)
	}
	return pane.Text
}

func (m *Model) viewDiffStatus() string {
	if m.errBar.text != "" {
		return padRight(m.statusMessage(" ✖", " ✔", " ▲"), m.layout.width)
	}
	return m.review.Status(m.reviewRenderContext(), m.layout.width)
}

func (m *Model) viewDiffFooter() string {
	var sections []legendSection
	for _, section := range m.review.Legend() {
		sections = append(sections, legendSection{title: section.Title, quiet: section.Quiet, pairs: section.Pairs})
	}
	return legendBar(sections, m.layout.width)
}
