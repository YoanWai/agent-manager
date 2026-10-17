package review

import (
	"fmt"
	"strings"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const diffGutterSign = 2

func (m *Model) Code(ctx RenderContext, width, height int) Pane {
	r := m.renderer(ctx)
	if m.loading && len(m.set.Files) == 0 {
		return Pane{Loading: "loading diff"}
	}
	if empty := r.emptyText(); empty != "" {
		return Pane{Text: empty}
	}
	fd := m.currentFile()
	if !fd.Loaded() && !m.fileHidden(fd) {
		return Pane{Loading: "loading file"}
	}
	if body := r.bodyNote(fd); body != "" {
		return Pane{Text: body}
	}

	var bar string
	if m.annotationOpen {
		bar = r.annotationBar(fd, width, ctx.CursorMarker)
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}

	hl := m.CurrentHighlight()
	var b strings.Builder
	if m.sideBySide {
		r.renderSideBySide(&b, fd, hl, width, height)
	} else {
		b.WriteString(strings.Join(r.unifiedRows(fd, hl, width, height), "\n"))
	}

	body := strings.TrimRight(b.String(), "\n")
	if bar != "" {
		return Pane{Text: presentation.PadToHeight(body, height) + "\n" + bar}
	}
	return Pane{Text: body}
}

// PrepareLayout performs the only Review layout mutations before View: it
// sizes the comment box and keeps the cursor's painted rows on screen in a
// code pane of width by height.
func (m *Model) PrepareLayout(ctx RenderContext, width, height int) {
	fd := m.currentFile()
	if fd == nil || !fd.Loaded() || m.fileHidden(fd) {
		return
	}
	r := m.renderer(ctx)
	if m.annotationOpen {
		inputHeight := m.AnnotationInputHeight(width)
		m.PrepareAnnotation(width, inputHeight)
		bar := r.annotationBar(fd, width, ctx.CursorMarker)
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}
	r.prepareCursorVisible(fd, m.CurrentHighlight(), width, height)
}

// prepareCursorVisible raises the scroll until every painted row of the
// window from the scroll through the cursor fits the viewport. The cursor
// and scroll count logical lines, but a long line wraps onto several
// painted rows, and a comment adds more below its line; sizing the window
// by line count alone lets the cursor walk below the last painted row, so
// the end of a wrapped file is selected but never on screen.
func (r renderer) prepareCursorVisible(fd *diff.FileDiff, hl *Highlight, width, height int) {
	total := len(fd.Lines)
	span := func(i int) int {
		return len(r.renderDiffRow(fd, hl, i, width, false)) + len(r.annotationRows(fd, i, width))
	}
	if r.m.sideBySide {
		rows := fd.SideBySideRows()
		total = len(rows)
		half := (width - 1) / 2
		span = func(i int) int {
			row := rows[i]
			tallest := len(r.renderSideCell(fd, hl, row.Left, half, true))
			if right := len(r.renderSideCell(fd, hl, row.Right, width-half-1, false)); right > tallest {
				tallest = right
			}
			if row.Left >= 0 {
				tallest += len(r.annotationRows(fd, row.Left, width))
			}
			if row.Right >= 0 && row.Right != row.Left {
				tallest += len(r.annotationRows(fd, row.Right, width))
			}
			return tallest
		}
	}

	spans := make([]int, total)
	for i := range spans {
		spans[i] = span(i)
	}
	r.m.PrepareViewport(spans, height)
}

// highlightedLine returns the highlighted text for a diff line, falling back
// to the raw text when highlighting is unavailable.
func (r renderer) highlightedLine(hl *Highlight, line diff.Line, index int) string {
	if line.Kind == diff.Gap {
		return r.s.Muted.Render(presentation.EscapeControls(line.Text))
	}
	if highlighted := hl.Line(index, ""); highlighted != "" {
		return highlighted
	}
	return presentation.EscapeControls(line.Text)
}

func (r renderer) annotationRows(fd *diff.FileDiff, lineIdx, width int) []string {
	var rows []string
	for _, note := range r.m.AnnotationsAt(fd.File.Path, fd.Lines[lineIdx]) {
		label := "¶"
		style := r.s.Annotation
		background := r.s.AnnotationBg
		if note.Round > 0 {
			label = fmt.Sprintf("Review round %d", note.Round)
			if note.Point > 0 {
				label += fmt.Sprintf(" · point %d", note.Point)
			}
		}
		if note.Resolved {
			label += " · handled"
			style = r.s.Done
			background = r.s.HandledAnnotationBg
		} else if note.Round > 0 {
			label += " · open"
		}
		if note.Outdated {
			label += " · outdated"
		}
		comment := style.Render("  " + label + " " + note.Text)
		rows = append(rows, wrapTinted(comment, nil, background, background, width)...)
	}
	return rows
}

// renderDiffRow renders one whole-file diff line into one or more visual
// rows: line numbers, change sign, syntax-highlighted text with the diff
// background tinted through, long lines wrapped with the gutter blanked
// on continuation rows.
func (r renderer) renderDiffRow(fd *diff.FileDiff, hl *Highlight, index, width int, cursor bool) []string {
	line := fd.Lines[index]
	gutterWidth := numWidth(fd)
	gutter := numCell(line.OldNum, gutterWidth) + numCell(line.NewNum, gutterWidth)

	sign, baseBg, spanBg := " ", "", ""
	switch line.Kind {
	case diff.Add:
		sign, baseBg, spanBg = "+", bgAdd, bgAddSpan
	case diff.Del:
		sign, baseBg, spanBg = "−", bgDel, bgDelSpan
	}

	textWidth := width - ansi.StringWidth(gutter) - diffGutterSign
	if textWidth < 4 {
		textWidth = 4
	}
	textRows := wrapTinted(r.highlightedLine(hl, line, index), escapeSpans(line.Text, line.Spans), baseBg, spanBg, textWidth)

	marker := " "
	if len(r.m.AnnotationsAt(fd.File.Path, line)) > 0 {
		marker = lipgloss.NewStyle().Foreground(r.s.Accent).Render("¶")
	}
	signCell := sign
	switch line.Kind {
	case diff.Add:
		signCell = lipgloss.NewStyle().Foreground(r.s.Finished).Render(sign)
	case diff.Del:
		signCell = lipgloss.NewStyle().Foreground(r.s.Errored).Render(sign)
	}
	blankGutter := strings.Repeat(" ", ansi.StringWidth(gutter))

	out := make([]string, len(textRows))
	for i, text := range textRows {
		prefix := r.s.Subtle.Render(blankGutter) + "  "
		if i == 0 {
			prefix = r.s.Subtle.Render(gutter) + marker + signCell
		}
		row := presentation.PadRight(prefix+text, width)
		if cursor {
			row = r.s.selectRow(row)
		}
		out[i] = row
	}
	return out
}

// unifiedRows fills the code viewport with wrapped whole-file rows,
// starting at the scroll line and reserving rows for the overflow
// indicators. A logical line can span several visual rows; annotation
// comments render on their own indented rows beneath the marked line.
func (r renderer) unifiedRows(fd *diff.FileDiff, hl *Highlight, width, height int) []string {
	total := len(fd.Lines)
	scroll := r.m.scroll
	if scroll > total-1 {
		scroll = total - 1
	}
	if scroll < 0 {
		scroll = 0
	}

	var rows []string
	if scroll > 0 {
		rows = append(rows, r.s.Subtle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < total && len(rows) < height; i++ {
		rows = append(rows, r.renderDiffRow(fd, hl, i, width, i == r.m.cursorLine)...)
		rows = append(rows, r.annotationRows(fd, i, width)...)
	}
	if i < total {
		if len(rows) >= height {
			rows = rows[:height-1]
		}
		rows = append(rows, r.s.Subtle.Render(fmt.Sprintf("  ↓ %d more", total-i)))
	} else if len(rows) > height {
		rows = rows[:height]
	}
	return rows
}

func numWidth(fd *diff.FileDiff) int {
	largest := fd.NewTotal
	if fd.OldTotal > largest {
		largest = fd.OldTotal
	}
	width := len(fmt.Sprintf("%d", largest))
	if width < 3 {
		width = 3
	}
	return width + 1
}

func numCell(num, width int) string {
	if num == 0 {
		return strings.Repeat(" ", width)
	}
	return fmt.Sprintf("%*d ", width-1, num)
}

func (r renderer) renderSideBySide(b *strings.Builder, fd *diff.FileDiff, hl *Highlight, width, height int) {
	rows := fd.SideBySideRows()
	half := (width - 1) / 2
	sep := r.s.Subtle.Render("│")

	scroll := r.m.scroll
	if scroll > len(rows)-1 {
		scroll = len(rows) - 1
	}
	if scroll < 0 {
		scroll = 0
	}
	var out []string
	if scroll > 0 {
		out = append(out, r.s.Subtle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < len(rows) && len(out) < height; i++ {
		row := rows[i]
		left := r.renderSideCell(fd, hl, row.Left, half, true)
		right := r.renderSideCell(fd, hl, row.Right, width-half-1, false)
		// A wrapped cell can be taller than its partner; pad the shorter
		// side with blank cells so the columns stay aligned.
		lines := len(left)
		if len(right) > lines {
			lines = len(right)
		}
		for k := 0; k < lines; k++ {
			leftCell, rightCell := presentation.PadRight("", half), presentation.PadRight("", width-half-1)
			if k < len(left) {
				leftCell = left[k]
			}
			if k < len(right) {
				rightCell = right[k]
			}
			line := leftCell + sep + rightCell
			if i == r.m.cursorLine {
				line = r.s.selectRow(presentation.PadRight(line, width))
			}
			out = append(out, line)
		}
		if row.Left >= 0 {
			out = append(out, r.annotationRows(fd, row.Left, width)...)
		}
		if row.Right >= 0 && row.Right != row.Left {
			out = append(out, r.annotationRows(fd, row.Right, width)...)
		}
	}
	if i < len(rows) {
		if len(out) >= height {
			out = out[:height-1]
		}
		out = append(out, r.s.Subtle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-i)))
	} else if len(out) > height {
		out = out[:height]
	}
	b.WriteString(strings.Join(out, "\n"))
}

// renderSideCell renders one half of a side-by-side row into wrapped
// visual rows; -1 renders a single dim filler for an unpaired line.
func (r renderer) renderSideCell(fd *diff.FileDiff, hl *Highlight, index, width int, leftSide bool) []string {
	if index < 0 {
		return []string{presentation.PadRight(r.s.Subtle.Render(" ·"), width)}
	}
	line := fd.Lines[index]
	// The left column carries old-side content: skip adds there.
	if leftSide && line.Kind == diff.Add {
		return []string{presentation.PadRight("", width)}
	}
	gutterWidth := numWidth(fd)
	num := line.NewNum
	if leftSide {
		num = line.OldNum
	}
	gutter := numCell(num, gutterWidth)

	baseBg, spanBg := "", ""
	switch line.Kind {
	case diff.Add:
		baseBg, spanBg = bgAdd, bgAddSpan
	case diff.Del:
		baseBg, spanBg = bgDel, bgDelSpan
	}

	textWidth := width - gutterWidth
	if textWidth < 4 {
		textWidth = 4
	}
	textRows := wrapTinted(r.highlightedLine(hl, line, index), escapeSpans(line.Text, line.Spans), baseBg, spanBg, textWidth)
	blankGutter := strings.Repeat(" ", gutterWidth)
	out := make([]string, len(textRows))
	for i, text := range textRows {
		g := blankGutter
		if i == 0 {
			g = gutter
		}
		out[i] = presentation.PadRight(r.s.Subtle.Render(g)+text, width)
	}
	return out
}
