package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	uireview "github.com/YoanWai/agent-manager/internal/ui/review"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const (
	maxHighlightBytes = 256 << 10

	bgAdd     = "\x1b[48;2;22;42;22m"
	bgDel     = "\x1b[48;2;48;24;24m"
	bgAddSpan = "\x1b[48;2;38;74;38m"
	bgDelSpan = "\x1b[48;2;82;36;36m"
)

var (
	chromaStyle     = brightCommentStyle()
	chromaFormatter = formatters.Get("terminal256")
)

// brightCommentStyle lifts monokai's dim comment color so source comments
// stay legible over the diff background tints.
func brightCommentStyle() *chroma.Style {
	style, err := styles.Get("monokai").Builder().Add(chroma.Comment, "#b0af99").Build()
	if err != nil {
		return styles.Get("monokai")
	}
	return style
}

// fileHL holds one file's syntax-highlighted lines, indexed the same way
// as the diff's own line model: a hunk-only model skips file lines, so a
// line's number is not its position.
type fileHL = uireview.Highlight

func contentHash(fd *diff.FileDiff) uint64 {
	return uireview.ContentHash(fd)
}

// highlightFile syntax-highlights both sides of a file diff. Deleted
// lines highlight from the old file version so their coloring is exact.
func highlightFile(fd *diff.FileDiff) *fileHL {
	oldText, newText := sideTexts(fd)
	if len(oldText)+len(newText) > maxHighlightBytes {
		return uireview.NewHighlight(nil)
	}
	lexer := lexers.Match(fd.File.Path)
	if lexer == nil {
		lexer = lexers.Analyse(newText)
	}
	if lexer == nil {
		return uireview.NewHighlight(nil)
	}
	lexer = chroma.Coalesce(lexer)
	return uireview.NewHighlight(alignSides(fd, highlightSide(lexer, oldText), highlightSide(lexer, newText)))
}

// alignSides walks the model in the order sideTexts wrote the two sides,
// pairing every line with its highlighted text.
func alignSides(fd *diff.FileDiff, oldLines, newLines []string) []string {
	lines := make([]string, len(fd.Lines))
	oldIdx, newIdx := 0, 0
	for i, line := range fd.Lines {
		switch line.Kind {
		case diff.Gap:
			continue
		case diff.Del:
			if oldIdx < len(oldLines) {
				lines[i] = oldLines[oldIdx]
			}
			oldIdx++
		case diff.Add:
			if newIdx < len(newLines) {
				lines[i] = newLines[newIdx]
			}
			newIdx++
		default:
			if newIdx < len(newLines) {
				lines[i] = newLines[newIdx]
			}
			oldIdx++
			newIdx++
		}
	}
	return lines
}

func sideTexts(fd *diff.FileDiff) (oldText, newText string) {
	var oldBuilder, newBuilder strings.Builder
	for _, line := range fd.Lines {
		if line.Kind == diff.Gap {
			continue
		}
		text := escapeControls(line.Text)
		if line.Kind != diff.Add {
			oldBuilder.WriteString(text)
			oldBuilder.WriteByte('\n')
		}
		if line.Kind != diff.Del {
			newBuilder.WriteString(text)
			newBuilder.WriteByte('\n')
		}
		if oldBuilder.Len()+newBuilder.Len() > maxHighlightBytes {
			break
		}
	}
	return oldBuilder.String(), newBuilder.String()
}

func highlightSide(lexer chroma.Lexer, text string) []string {
	if text == "" {
		return nil
	}
	iterator, err := lexer.Tokenise(nil, text)
	if err != nil {
		return nil
	}
	tokenLines := chroma.SplitTokensIntoLines(iterator.Tokens())
	lines := make([]string, 0, len(tokenLines))
	var builder strings.Builder
	for _, tokens := range tokenLines {
		builder.Reset()
		if err := chromaFormatter.Format(&builder, chromaStyle, chroma.Literator(tokens...)); err != nil {
			return nil
		}
		lines = append(lines, strings.TrimRight(builder.String(), "\n"))
	}
	return lines
}

// hlLine returns the highlighted text for a diff line, falling back to
// the raw text when highlighting is unavailable.
func highlightedLine(hl *fileHL, line diff.Line, index int) string {
	if line.Kind == diff.Gap {
		return mutedStyle.Render(escapeControls(line.Text))
	}
	if highlighted := hl.Line(index, ""); highlighted != "" {
		return highlighted
	}
	return escapeControls(line.Text)
}

// wrapTinted overlays a diff background onto a chroma-highlighted line
// and wraps it to width, returning one entry per visual row. The diff
// background is re-emitted after every SGR reset chroma writes so it
// survives across tokens and across wrap boundaries; word spans (byte
// offsets into the raw text) switch to the brighter span background.
// When a background is set, each row is padded so the tint fills the
// full width; a plain line (empty baseBg) is left unpadded for the
// caller to pad. Every row is closed with a reset.
//
// Wrapping prefers word boundaries (space characters): when a non-space
// character would overflow the row, the current word moves to the next
// line instead of being split mid-identifier. A word wider than the line
// width falls back to character-level splitting.
func wrapTinted(highlighted string, spans []diff.Span, baseBg, spanBg string, width int) []string {
	if width < 1 {
		width = 1
	}
	bgFor := func(offset int) string {
		for _, span := range spans {
			if offset >= span.Start && offset < span.End {
				return spanBg
			}
		}
		return baseBg
	}

	type seg struct {
		text    string
		visible int
		bytes   int
	}
	var tokens []seg
	i := 0
	for i < len(highlighted) {
		if highlighted[i] == 0x1b {
			end := i + 1
			if end < len(highlighted) && highlighted[end] == '[' {
				end++
				for end < len(highlighted) && highlighted[end] != 'm' {
					end++
				}
				if end < len(highlighted) {
					end++
				}
			}
			tokens = append(tokens, seg{text: highlighted[i:end]})
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(highlighted[i:])
		if r == ' ' {
			tokens = append(tokens, seg{text: " ", visible: 1, bytes: 1})
			i++
			continue
		}
		end := i + size
		visible := ansi.StringWidth(string(r))
		bytes := size
		for end < len(highlighted) && highlighted[end] != ' ' && highlighted[end] != 0x1b {
			r2, s2 := utf8.DecodeRuneInString(highlighted[end:])
			visible += ansi.StringWidth(string(r2))
			bytes += s2
			end += s2
		}
		tokens = append(tokens, seg{text: highlighted[i:end], visible: visible, bytes: bytes})
		i = end
	}

	var rows []string
	var b strings.Builder
	activeBg := ""
	activeFg := ""
	rowWidth := 0
	fresh := true
	offset := 0

	noteAnsi := func(seq string) {
		if seq == "\x1b[0m" {
			activeFg = ""
		} else {
			activeFg += seq
		}
	}

	closeRow := func() {
		if baseBg != "" && rowWidth < width {
			b.WriteString(strings.Repeat(" ", width-rowWidth))
		}
		if b.Len() > 0 || baseBg != "" {
			b.WriteString("\x1b[0m")
		}
		rows = append(rows, b.String())
		b.Reset()
		fresh = true
		rowWidth = 0
	}

	emitText := func(text string, textBytes int) {
		for ti := 0; ti < len(text); {
			if text[ti] == 0x1b {
				end := ti + 1
				if end < len(text) && text[end] == '[' {
					end++
					for end < len(text) && text[end] != 'm' {
						end++
					}
					if end < len(text) {
						end++
					}
				}
				seq := text[ti:end]
				b.WriteString(seq)
				noteAnsi(seq)
				if seq == "\x1b[0m" && activeBg != "" {
					b.WriteString(activeBg)
				}
				ti = end
				continue
			}
			if fresh {
				activeBg = bgFor(offset)
				if activeBg != "" {
					b.WriteString(activeBg)
				}
				if activeFg != "" {
					b.WriteString(activeFg)
				}
				fresh = false
			}
			r, rSize := utf8.DecodeRuneInString(text[ti:])
			rWidth := ansi.StringWidth(string(r))
			if rowWidth+rWidth > width {
				closeRow()
				continue
			}
			if bg := bgFor(offset); bg != activeBg {
				activeBg = bg
				b.WriteString(bg)
			}
			b.WriteString(text[ti : ti+rSize])
			offset += rSize
			rowWidth += rWidth
			ti += rSize
		}
	}

	for ti := 0; ti < len(tokens); ti++ {
		tok := tokens[ti]
		isAnsi := tok.visible == 0 && tok.bytes == 0
		isSpace := tok.text == " "

		if isAnsi {
			b.WriteString(tok.text)
			noteAnsi(tok.text)
			if tok.text == "\x1b[0m" && activeBg != "" {
				b.WriteString(activeBg)
			}
			continue
		}

		if isSpace {
			if rowWidth+1 > width {
				closeRow()
				continue
			}
			if bg := bgFor(offset); bg != activeBg {
				activeBg = bg
				b.WriteString(bg)
			}
			b.WriteByte(' ')
			offset++
			rowWidth++
			continue
		}

		if rowWidth+tok.visible > width {
			if rowWidth > 0 {
				closeRow()
			}
			if tok.visible > width {
				emitText(tok.text, tok.bytes)
				continue
			}
		}
		emitText(tok.text, tok.bytes)
	}

	if !fresh || len(rows) == 0 {
		closeRow()
	}
	return rows
}

func (m *Model) annotationRows(fd *diff.FileDiff, lineIdx, width int) []string {
	var rows []string
	for _, note := range m.review.AnnotationsAt(fd.File.Path, fd.Lines[lineIdx]) {
		label := "¶"
		style := annotationStyle
		background := annotationBg()
		if note.Round > 0 {
			label = fmt.Sprintf("Review round %d", note.Round)
			if note.Point > 0 {
				label += fmt.Sprintf(" · point %d", note.Point)
			}
		}
		if note.Resolved {
			label += " · handled"
			style = doneStyle
			background = handledAnnotationBg()
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

func (m *Model) reviewRing(label string, width, height int) string {
	return strings.Join(ringLoader(width, height, label, m.startup.startupPhase), "\n")
}

func (m *Model) reviewSpinnerLine(label string) string {
	frame := startupFrames[m.startup.startupPhase%len(startupFrames)]
	return mutedStyle.Render(frame + " " + label)
}

func (m *Model) diffEmptyText() string {
	state := m.review.Snapshot()
	if state.Error != "" {
		return errStyle.Render("✖ " + escapeControls(state.Error))
	}
	if state.SessionID == "" {
		return mutedStyle.Render("(select a session to diff)")
	}
	if len(state.Set.Files) == 0 {
		return mutedStyle.Render(fmt.Sprintf("✓ no changes (%s)", state.Scope)) + "\n" +
			subtleStyle.Render("s cycles scope")
	}
	return ""
}

// diffAllHiddenNote is what both review panes fall back to when the code-only
// filter leaves nothing to show. It has to fit the narrow file rail.
func diffAllHiddenNote() string {
	return mutedStyle.Render("(no code files)") + "\n" +
		subtleStyle.Render("f shows the rest")
}

func (m *Model) diffBodyNote(fd *diff.FileDiff) string {
	switch {
	case m.diffFileHidden(fd):
		return diffAllHiddenNote()
	case fd.Err != nil:
		return errStyle.Render("✖ " + escapeControls(fd.Err.Error()))
	case fd.Binary:
		return mutedStyle.Render("(binary file)")
	case fd.Truncated && len(fd.Lines) == 0:
		return mutedStyle.Render("(file too large to diff)")
	case len(fd.Lines) == 0:
		return mutedStyle.Render("(empty file)")
	}
	return ""
}

// renderDiffRow renders one whole-file diff line into one or more visual
// rows: line numbers, change sign, syntax-highlighted text with the diff
// background tinted through, long lines wrapped with the gutter blanked
// on continuation rows.
func (m *Model) renderDiffRow(fd *diff.FileDiff, hl *fileHL, index, width int, cursor bool) []string {
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
	textRows := wrapTinted(highlightedLine(hl, line, index), escapeSpans(line.Text, line.Spans), baseBg, spanBg, textWidth)

	marker := " "
	if len(m.review.AnnotationsAt(fd.File.Path, line)) > 0 {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("¶")
	}
	signCell := sign
	switch line.Kind {
	case diff.Add:
		signCell = lipgloss.NewStyle().Foreground(colorFinished).Render(sign)
	case diff.Del:
		signCell = lipgloss.NewStyle().Foreground(colorErrored).Render(sign)
	}
	blankGutter := strings.Repeat(" ", ansi.StringWidth(gutter))

	out := make([]string, len(textRows))
	for i, text := range textRows {
		prefix := subtleStyle.Render(blankGutter) + "  "
		if i == 0 {
			prefix = subtleStyle.Render(gutter) + marker + signCell
		}
		row := padRight(prefix+text, width)
		if cursor {
			row = renderSelectedRow(row)
		}
		out[i] = row
	}
	return out
}

// unifiedRows fills the code viewport with wrapped whole-file rows,
// starting at the scroll line and reserving rows for the overflow
// indicators. A logical line can span several visual rows; annotation
// comments render on their own indented rows beneath the marked line.
func (m *Model) unifiedRows(fd *diff.FileDiff, hl *fileHL, width, height int) []string {
	total := len(fd.Lines)
	state := m.review.Snapshot()
	scroll := state.Scroll
	if scroll > total-1 {
		scroll = total - 1
	}
	if scroll < 0 {
		scroll = 0
	}

	var rows []string
	if scroll > 0 {
		rows = append(rows, subtleStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < total && len(rows) < height; i++ {
		rows = append(rows, m.renderDiffRow(fd, hl, i, width, i == state.CursorLine)...)
		rows = append(rows, m.annotationRows(fd, i, width)...)
	}
	if i < total {
		if len(rows) >= height {
			rows = rows[:height-1]
		}
		rows = append(rows, subtleStyle.Render(fmt.Sprintf("  ↓ %d more", total-i)))
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
	fileLines := append([]string{"", strings.Repeat(" ", railInset) + subtleStyle.Render("files")},
		indentLines(splitLines(m.viewDiffFileList(fileWidth-2*railGutter, bodyHeight-2)), railInset)...)
	codeLines := append([]string{"", "  " + subtleStyle.Render(escapeControlsInline(m.diffCodeTitle()))},
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
	state := m.review.Snapshot()
	layout := "unified"
	if state.SideBySide {
		layout = "split"
	}
	left := "  " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("review · "+escapeControlsInline(sessName)) + "  " +
		keyPill("s", state.Scope.String(), colorAccent2) + "  " +
		keyPill("u", layout, colorAccent)
	if state.CodeOnly {
		left += "  " + keyPill("f", "code only", colorAccent)
	}
	if root := state.Set.Repo.Root; root != "" {
		name := filepath.Base(state.RepoSelected)
		if name == "" || name == "." {
			name = filepath.Base(root)
		}
		name = escapeControlsInline(name)
		if len(state.RepoRoots) > 1 {
			name = fmt.Sprintf("%s · %d repos", name, len(state.RepoRoots))
		}
		left += "  " + keyPill("r", name, colorAccent)
		branch := escapeControlsInline(state.Set.Repo.Branch)
		if state.Scope == git.ScopeBranch && state.Set.BaseDesc != "" && branch != "" {
			target := escapeControlsInline(uireview.StripBaseHash(state.Set.BaseDesc))
			summary := target + " → " + branch
			if state.Set.BaseOverride == "" {
				summary += " " + subtleStyle.Render("(auto)")
			}
			left += "  " + keyPill("B", summary, colorAccent)
		} else if branch != "" {
			left += "  " + subtleStyle.Render(branch)
		}
	}

	adds, dels := 0, 0
	shown := 0
	uncounted := false
	for i := range state.Set.Files {
		fd := &state.Set.Files[i]
		if fd.Hidden {
			continue
		}
		shown++
		if !fd.StatKnown() {
			uncounted = true
			continue
		}
		adds += fd.Stat.Adds
		dels += fd.Stat.Dels
	}
	right := mutedStyle.Render(fmt.Sprintf("%d files", shown)) + subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", adds)) + " " +
		lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", dels))
	if uncounted {
		right += " " + mutedStyle.Render("+?")
	}
	if count := m.review.DraftCount(); count > 0 {
		right += subtleStyle.Render(" · ") + lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("¶%d", count))
	}
	if round := m.review.Round(); round.Number > 0 {
		label := fmt.Sprintf("Review round %d", round.Number)
		if !state.Loading && round.Fingerprint != 0 &&
			(round.Scope != state.Scope.String() || round.Fingerprint != state.Fingerprint) {
			label += " · changed"
		}
		right += subtleStyle.Render(" · ") + annotationStyle.Render(label)
	}
	right += " "

	gap := m.layout.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		budget := m.layout.width - ansi.StringWidth(right)
		if budget < 1 {
			return padRight(right, m.layout.width)
		}
		return padRight(left, budget) + right
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) diffCodeTitle() string {
	fd := m.currentFileDiff()
	if fd == nil || m.diffFileHidden(fd) {
		return "Diff"
	}
	return fd.File.Path
}

func (m *Model) viewDiffFileList(width, height int) string {
	state := m.review.Snapshot()
	if state.Loading && len(state.Set.Files) == 0 {
		return m.reviewSpinnerLine("loading diff")
	}
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	files := state.Set.Files
	shown := make([]int, 0, len(files))
	for i := range files {
		if !files[i].Hidden {
			shown = append(shown, i)
		}
	}
	if len(shown) == 0 {
		return diffAllHiddenNote()
	}
	cursor := 0
	for pos, i := range shown {
		if i == state.FileIndex {
			cursor = pos
			break
		}
	}
	start, end := scrollWindow(len(shown), cursor, height)
	var b strings.Builder
	if start > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	notes := map[string]int{}
	for _, note := range m.review.Annotations() {
		if !note.Resolved {
			notes[note.File]++
		}
	}
	for pos := start; pos < end; pos++ {
		i := shown[pos]
		fd := files[i]
		glyph := subtleStyle.Render("○")
		if m.review.FileReviewed(fd.File.Path) {
			glyph = lipgloss.NewStyle().Foreground(colorFinished).Render("✔")
		}
		bar := " "
		if i == state.FileIndex {
			bar = lipgloss.NewStyle().Foreground(colorAccent).Render("▎")
		}
		counts := lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", fd.Stat.Adds)) +
			" " + lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", fd.Stat.Dels))
		if !fd.StatKnown() {
			counts = mutedStyle.Render("?")
		}
		if fd.Binary || fd.Stat.Binary {
			counts = mutedStyle.Render("binary")
		}
		if count := notes[fd.File.Path]; count > 0 {
			counts = lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("¶%d ", count)) + counts
		}
		nameBudget := width - ansi.StringWidth(counts) - 6
		if nameBudget < 4 {
			nameBudget = 4
		}
		name := truncatePath(escapeControlsInline(fd.File.Path), nameBudget)
		left := bar + glyph + " " + valueStyle.Render(name)
		gap := width - ansi.StringWidth(left) - ansi.StringWidth(counts)
		if gap < 1 {
			gap = 1
		}
		row := padRight(left+strings.Repeat(" ", gap)+counts, width)
		if i == state.FileIndex {
			row = renderSelectedRow(row)
		}
		b.WriteString(row + "\n")
	}
	if end < len(shown) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(shown)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewDiffCode(width, height int) string {
	state := m.review.Snapshot()
	if state.Loading && len(state.Set.Files) == 0 {
		return m.reviewRing("loading diff", width, height)
	}
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	fd := m.currentFileDiff()
	if !fd.Loaded() && !m.diffFileHidden(fd) {
		return m.reviewRing("loading file", width, height)
	}
	if body := m.diffBodyNote(fd); body != "" {
		return body
	}

	var bar string
	if state.Annotating {
		fdLine := fd.Lines[m.cursorDiffLine()]
		num, _ := uireview.AnnotationLine(fdLine)
		bar = divider(fmt.Sprintf("Comment · %s:%d", escapeControlsInline(fd.File.Path), num), width) + "\n" + m.review.AnnotationView(cursorAnchorMarker)
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}

	hl := m.currentHL()
	var b strings.Builder
	if state.SideBySide {
		m.renderSideBySide(&b, fd, hl, width, height)
	} else {
		b.WriteString(strings.Join(m.unifiedRows(fd, hl, width, height), "\n"))
	}

	body := strings.TrimRight(b.String(), "\n")
	if bar != "" {
		return padToHeight(body, height) + "\n" + bar
	}
	return body
}

// ensureDiffCursorVisible raises the scroll until every painted row of the
// window from the scroll through the cursor fits the viewport. The cursor
// and scroll count logical lines, but a long line wraps onto several
// painted rows, and a comment adds more below its line; sizing the window
// by line count alone lets the cursor walk below the last painted row, so
// the end of a wrapped file is selected but never on screen.
func (m *Model) prepareDiffCursorVisible(fd *diff.FileDiff, hl *fileHL, width, height int) {
	total := len(fd.Lines)
	span := func(i int) int {
		return len(m.renderDiffRow(fd, hl, i, width, false)) + len(m.annotationRows(fd, i, width))
	}
	if m.review.Snapshot().SideBySide && m.mode == modeDiff {
		rows := fd.SideBySideRows()
		total = len(rows)
		half := (width - 1) / 2
		span = func(i int) int {
			row := rows[i]
			tallest := len(m.renderSideCell(fd, hl, row.Left, half, true))
			if right := len(m.renderSideCell(fd, hl, row.Right, width-half-1, false)); right > tallest {
				tallest = right
			}
			if row.Left >= 0 {
				tallest += len(m.annotationRows(fd, row.Left, width))
			}
			if row.Right >= 0 && row.Right != row.Left {
				tallest += len(m.annotationRows(fd, row.Right, width))
			}
			return tallest
		}
	}

	spans := make([]int, total)
	for i := range spans {
		spans[i] = span(i)
	}
	m.review.PrepareViewport(spans, height)
}

func (m *Model) renderSideBySide(b *strings.Builder, fd *diff.FileDiff, hl *fileHL, width, height int) {
	rows := fd.SideBySideRows()
	half := (width - 1) / 2
	sep := subtleStyle.Render("│")

	state := m.review.Snapshot()
	scroll := state.Scroll
	if scroll > len(rows)-1 {
		scroll = len(rows) - 1
	}
	if scroll < 0 {
		scroll = 0
	}
	var out []string
	if scroll > 0 {
		out = append(out, subtleStyle.Render(fmt.Sprintf("  ↑ %d more", scroll)))
	}
	i := scroll
	for ; i < len(rows) && len(out) < height; i++ {
		row := rows[i]
		left := m.renderSideCell(fd, hl, row.Left, half, true)
		right := m.renderSideCell(fd, hl, row.Right, width-half-1, false)
		// A wrapped cell can be taller than its partner; pad the shorter
		// side with blank cells so the columns stay aligned.
		lines := len(left)
		if len(right) > lines {
			lines = len(right)
		}
		for r := 0; r < lines; r++ {
			leftCell, rightCell := padRight("", half), padRight("", width-half-1)
			if r < len(left) {
				leftCell = left[r]
			}
			if r < len(right) {
				rightCell = right[r]
			}
			line := leftCell + sep + rightCell
			if i == state.CursorLine {
				line = renderSelectedRow(padRight(line, width))
			}
			out = append(out, line)
		}
		if row.Left >= 0 {
			out = append(out, m.annotationRows(fd, row.Left, width)...)
		}
		if row.Right >= 0 && row.Right != row.Left {
			out = append(out, m.annotationRows(fd, row.Right, width)...)
		}
	}
	if i < len(rows) {
		if len(out) >= height {
			out = out[:height-1]
		}
		out = append(out, subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(rows)-i)))
	} else if len(out) > height {
		out = out[:height]
	}
	b.WriteString(strings.Join(out, "\n"))
}

// renderSideCell renders one half of a side-by-side row into wrapped
// visual rows; -1 renders a single dim filler for an unpaired line.
func (m *Model) renderSideCell(fd *diff.FileDiff, hl *fileHL, index, width int, leftSide bool) []string {
	if index < 0 {
		return []string{padRight(subtleStyle.Render(" ·"), width)}
	}
	line := fd.Lines[index]
	// The left column carries old-side content: skip adds there.
	if leftSide && line.Kind == diff.Add {
		return []string{padRight("", width)}
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
	textRows := wrapTinted(highlightedLine(hl, line, index), escapeSpans(line.Text, line.Spans), baseBg, spanBg, textWidth)
	blankGutter := strings.Repeat(" ", gutterWidth)
	out := make([]string, len(textRows))
	for i, text := range textRows {
		g := blankGutter
		if i == 0 {
			g = gutter
		}
		out[i] = padRight(subtleStyle.Render(g)+text, width)
	}
	return out
}

func (m *Model) viewDiffStatus() string {
	state := m.review.Snapshot()
	if m.errBar.text != "" {
		return padRight(m.statusMessage(" ✖", " ✔", " ▲"), m.layout.width)
	}
	if state.Notice != "" {
		return padRight(doneStyle.Render(" ✔ "+escapeControlsInline(state.Notice)), m.layout.width)
	}
	if state.SendConfirm {
		count := m.review.DraftCount()
		return padRight(errStyle.Render(fmt.Sprintf(" ¶ send %d %s to the agent?", count, uireview.CommentNoun(count)))+
			subtleStyle.Render("  ↵/y send · esc cancel"), m.layout.width)
	}
	return ""
}

func (m *Model) viewDiffFooter() string {
	state := m.review.Snapshot()
	if state.Annotating {
		return legendBar([]legendSection{{title: "Comment", pairs: [][2]string{
			{"↵", "save"}, {"esc", "cancel"},
		}}}, m.layout.width)
	}
	repo := "repo"
	if len(state.RepoRoots) > 0 {
		repo = "repo: " + escapeControlsInline(filepath.Base(state.RepoSelected))
	}
	send := "send"
	if count := m.review.DraftCount(); count > 0 {
		send = fmt.Sprintf("send %d", count)
	}
	filter := "code only"
	if state.CodeOnly {
		filter = "all files"
	}
	return legendBar([]legendSection{
		{title: "Review", pairs: [][2]string{
			{"c", "comment"}, {"d", "remove/handle"}, {"C", send}, {"space", "reviewed"},
			{"s", "scope: " + state.Scope.String()}, {"r", repo}, {"b", "branch"}, {"B", "target"},
		}},
		{title: "Move", quiet: true, pairs: [][2]string{
			{"↑↓/jk", "scroll line"}, {"ctrl+d/ctrl+u", "half page"}, {"pgup/pgdn", "page"},
			{"g/G", "top/bottom"}, {"tab/J K/shift+tab", "file"}, {"n/N", "change"},
			{"u", "layout"}, {"f", filter}, {"o/f3", "open file"}, {"?", "keys"},
			{"esc/q", "close"}, {"ctrl+c", "quit"},
		}},
	}, m.layout.width)
}

// ---- rendering ----

const diffGutterSign = 2
