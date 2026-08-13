package ui

import (
	"fmt"
	"hash/fnv"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/YoanWai/agent-manager/internal/diff"
	"github.com/YoanWai/agent-manager/internal/git"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	maxHighlightBytes = 256 << 10
	highlightCacheCap = 8

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

// fileHL holds one file's syntax-highlighted lines per side, indexed by
// OldNum/NewNum minus one.
type fileHL struct {
	oldLines []string
	newLines []string
}

type hlKey struct {
	sessID string
	scope  git.Scope
	path   string
	hash   uint64
}

type hlCache struct {
	entries map[hlKey]*fileHL
	order   []hlKey
}

func newHLCache() *hlCache {
	return &hlCache{entries: map[hlKey]*fileHL{}}
}

func (c *hlCache) get(key hlKey) *fileHL {
	return c.entries[key]
}

func (c *hlCache) put(key hlKey, hl *fileHL) {
	if _, ok := c.entries[key]; !ok {
		c.order = append(c.order, key)
		if len(c.order) > highlightCacheCap {
			delete(c.entries, c.order[0])
			c.order = c.order[1:]
		}
	}
	c.entries[key] = hl
}

func contentHash(fd *diff.FileDiff) uint64 {
	hash := fnv.New64a()
	for _, line := range fd.Lines {
		hash.Write([]byte{byte(line.Kind)})
		hash.Write([]byte(line.Text))
		hash.Write([]byte{'\n'})
	}
	return hash.Sum64()
}

// highlightFile syntax-highlights both sides of a file diff. Deleted
// lines highlight from the old file version so their coloring is exact.
func highlightFile(fd *diff.FileDiff) *fileHL {
	oldText, newText := sideTexts(fd)
	if len(oldText)+len(newText) > maxHighlightBytes {
		return &fileHL{}
	}
	lexer := lexers.Match(fd.File.Path)
	if lexer == nil {
		lexer = lexers.Analyse(newText)
	}
	if lexer == nil {
		return &fileHL{}
	}
	lexer = chroma.Coalesce(lexer)
	return &fileHL{
		oldLines: highlightSide(lexer, oldText),
		newLines: highlightSide(lexer, newText),
	}
}

func sideTexts(fd *diff.FileDiff) (oldText, newText string) {
	var oldBuilder, newBuilder strings.Builder
	for _, line := range fd.Lines {
		if line.Kind != diff.Add {
			oldBuilder.WriteString(line.Text)
			oldBuilder.WriteByte('\n')
		}
		if line.Kind != diff.Del {
			newBuilder.WriteString(line.Text)
			newBuilder.WriteByte('\n')
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
func (hl *fileHL) hlLine(line diff.Line) string {
	if hl != nil {
		if line.Kind == diff.Del {
			if line.OldNum >= 1 && line.OldNum <= len(hl.oldLines) {
				return hl.oldLines[line.OldNum-1]
			}
		} else if line.NewNum >= 1 && line.NewNum <= len(hl.newLines) {
			return hl.newLines[line.NewNum-1]
		}
	}
	return line.Text
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

// annotationRows renders the comment attached to line lineIdx, if any, as
// its own indented full-width rows beneath the code line. Shared by the
// unified and side-by-side layouts so both surface saved comments.
func (m *Model) annotationRows(fd *diff.FileDiff, lineIdx, width int) []string {
	note := m.annotationAt(fd.File.Path, fd.Lines[lineIdx])
	if note == nil {
		return nil
	}
	comment := annotationStyle.Render("  ¶ " + note.text)
	return wrapTinted(comment, nil, annotationBg(), annotationBg(), width)
}

func (m *Model) diffEmptyText() string {
	if m.diff.loading && len(m.diff.set.Files) == 0 {
		return mutedStyle.Render("(loading diff…)")
	}
	if m.diff.errText != "" {
		return errStyle.Render("✖ " + m.diff.errText)
	}
	if m.diff.sessID == "" {
		return mutedStyle.Render("(select a session to diff)")
	}
	if len(m.diff.set.Files) == 0 {
		return mutedStyle.Render(fmt.Sprintf("✓ no changes (%s)", m.diff.scope)) + "\n" +
			subtleStyle.Render("s cycles scope")
	}
	return ""
}

func (m *Model) diffBodyNote(fd *diff.FileDiff) string {
	switch {
	case !fd.Loaded():
		return mutedStyle.Render("(loading file…)")
	case fd.Err != nil:
		return errStyle.Render("✖ " + fd.Err.Error())
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
	textRows := wrapTinted(hl.hlLine(line), line.Spans, baseBg, spanBg, textWidth)

	marker := " "
	if m.annotationAt(fd.File.Path, line) != nil {
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
	scroll := m.diff.scroll
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
		rows = append(rows, m.renderDiffRow(fd, hl, i, width, i == m.diff.cursorLine)...)
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
	bodyHeight := m.height - 4 - lipgloss.Height(footer)
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	fileWidth := m.width * 24 / 100
	if fileWidth < 28 {
		fileWidth = 28
	}
	// Two columns go to the seam and the fill's bleed edge between the
	// two surfaces.
	codeWidth := m.width - fileWidth - 2

	// Files sit on the rail surface, the code on the backdrop: the same
	// two-surface split the session list uses, so review reads as the same
	// application rather than a second one.
	fileLines := append([]string{"", strings.Repeat(" ", railInset) + subtleStyle.Render("files")},
		indentLines(splitLines(m.viewDiffFileList(fileWidth-2*railGutter, bodyHeight-2)), railInset)...)
	codeLines := append([]string{"", "  " + subtleStyle.Render(ansi.Strip(m.diffCodeTitle()))},
		indentLines(splitLines(m.viewDiffCode(codeWidth-2*contentGutter, bodyHeight-2)), contentGutter)...)

	// The column seam tees into the rules that open and close the body,
	// same as the list view, so the two screens share one frame language.
	frame := []string{
		paint(m.viewDiffHeader(sess.Name), m.width, backdropHex()),
		m.boundedRuleRow(fileWidth+1, m.width, "▀"),
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
		m.boundedRuleRow(fileWidth+1, m.width, "▄"),
		paint(m.viewDiffStatus(), m.width, backdropHex()),
	)
	for _, line := range splitLines(footer) {
		frame = append(frame, paint(line, m.width, backdropHex()))
	}
	return strings.Join(frame, "\n")
}

// stripBaseHash drops the @<short-sha> suffix BaseDesc carries from
// baseRefFor. The hash matters for telling two merge-bases apart in logs
// but reads as noise in the header, where only the ref name carries
// signal for the user picking a target.
func stripBaseHash(desc string) string {
	if i := strings.LastIndexByte(desc, '@'); i >= 0 {
		return desc[:i]
	}
	return desc
}

func (m *Model) viewDiffHeader(sessName string) string {
	layout := "unified"
	if m.diff.sideBySide {
		layout = "split"
	}
	left := "  " + lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("review · "+sessName) + "  " +
		keyPill("s", m.diff.scope.String(), colorAccent2) + "  " +
		keyPill("u", layout, colorAccent)
	if root := m.diff.set.Repo.Root; root != "" {
		name := filepath.Base(m.diff.repoSel)
		if name == "" || name == "." {
			name = filepath.Base(root)
		}
		if len(m.diff.repoRoots) > 1 {
			name = fmt.Sprintf("%s · %d repos", name, len(m.diff.repoRoots))
		}
		left += "  " + keyPill("r", name, colorAccent)
		branch := m.diff.set.Repo.Branch
		if m.diff.scope == git.ScopeBranch && m.diff.set.BaseDesc != "" && branch != "" {
			target := stripBaseHash(m.diff.set.BaseDesc)
			summary := target + " → " + branch
			if m.diff.set.BaseOverride == "" {
				summary += " " + subtleStyle.Render("(auto)")
			}
			left += "  " + keyPill("B", summary, colorAccent)
		} else if branch != "" {
			left += "  " + subtleStyle.Render(branch)
		}
	}

	adds, dels := 0, 0
	uncounted := false
	for _, fd := range m.diff.set.Files {
		if !fd.StatKnown() {
			uncounted = true
			continue
		}
		adds += fd.Stat.Adds
		dels += fd.Stat.Dels
	}
	right := mutedStyle.Render(fmt.Sprintf("%d files", len(m.diff.set.Files))) + subtleStyle.Render(" · ") +
		lipgloss.NewStyle().Foreground(colorFinished).Render(fmt.Sprintf("+%d", adds)) + " " +
		lipgloss.NewStyle().Foreground(colorErrored).Render(fmt.Sprintf("−%d", dels))
	if uncounted {
		right += " " + mutedStyle.Render("+?")
	}
	if count := len(m.diff.annotations[m.reviewKey()]); count > 0 {
		right += subtleStyle.Render(" · ") + lipgloss.NewStyle().Foreground(colorAccent).Render(fmt.Sprintf("¶%d", count))
	}
	right += " "

	gap := m.width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		return padRight(left, m.width)
	}
	return left + strings.Repeat(" ", gap) + right
}

func (m *Model) diffCodeTitle() string {
	fd := m.currentFileDiff()
	if fd == nil {
		return "Diff"
	}
	return fd.File.Path
}

func (m *Model) viewDiffFileList(width, height int) string {
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	files := m.diff.set.Files
	start, end := scrollWindow(len(files), m.diff.fileIdx, height)
	var b strings.Builder
	if start > 0 {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
	}
	notes := map[string]int{}
	for _, note := range m.diff.annotations[m.reviewKey()] {
		notes[note.file]++
	}
	for i := start; i < end; i++ {
		fd := files[i]
		glyph := subtleStyle.Render("○")
		if m.fileReviewed(fd.File.Path) {
			glyph = lipgloss.NewStyle().Foreground(colorFinished).Render("✔")
		}
		bar := " "
		if i == m.diff.fileIdx {
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
		name := truncateTail(fd.File.Path, width-ansi.StringWidth(counts)-6)
		left := bar + glyph + " " + valueStyle.Render(name)
		gap := width - ansi.StringWidth(left) - ansi.StringWidth(counts)
		if gap < 1 {
			gap = 1
		}
		row := left + strings.Repeat(" ", gap) + counts
		if i == m.diff.fileIdx {
			row = renderSelectedRow(padRight(row, width))
		}
		b.WriteString(row + "\n")
	}
	if end < len(files) {
		b.WriteString(subtleStyle.Render(fmt.Sprintf("  ↓ %d more", len(files)-end)))
	}
	return strings.TrimRight(b.String(), "\n")
}

func (m *Model) viewDiffCode(width, height int) string {
	if empty := m.diffEmptyText(); empty != "" {
		return empty
	}
	fd := m.currentFileDiff()
	if body := m.diffBodyNote(fd); body != "" {
		return body
	}

	var bar string
	if m.diff.annotating {
		fdLine := fd.Lines[m.cursorDiffLine()]
		num, _ := annotationLine(fdLine)
		m.diff.annInput.SetWidth(width)
		m.diff.annInput.SetHeight(1)
		bar = divider(fmt.Sprintf("Comment · %s:%d", fd.File.Path, num), width) + "\n" + m.diff.annInput.View()
		height -= lipgloss.Height(bar) + 1
		if height < 3 {
			height = 3
		}
	}

	hl := m.currentHL()
	m.ensureDiffCursorVisible(fd, hl, width, height)
	var b strings.Builder
	if m.diff.sideBySide {
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
func (m *Model) ensureDiffCursorVisible(fd *diff.FileDiff, hl *fileHL, width, height int) {
	total := len(fd.Lines)
	span := func(i int) int {
		return len(m.renderDiffRow(fd, hl, i, width, false)) + len(m.annotationRows(fd, i, width))
	}
	if m.diff.sideBySide && m.mode == modeDiff {
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

	cursor := m.diff.cursorLine
	if cursor > total-1 {
		cursor = total - 1
	}
	if cursor < 0 {
		return
	}
	if m.diff.scroll > cursor {
		m.diff.scroll = cursor
	}
	if m.diff.scroll < 0 {
		m.diff.scroll = 0
	}
	for m.diff.scroll < cursor {
		// Each overflow indicator takes a row of the same budget.
		used := 0
		if m.diff.scroll > 0 {
			used++
		}
		if cursor < total-1 {
			used++
		}
		fits := true
		for i := m.diff.scroll; i <= cursor; i++ {
			used += span(i)
			if used > height {
				fits = false
				break
			}
		}
		if fits {
			break
		}
		m.diff.scroll++
	}
}

func (m *Model) renderSideBySide(b *strings.Builder, fd *diff.FileDiff, hl *fileHL, width, height int) {
	rows := fd.SideBySideRows()
	half := (width - 1) / 2
	sep := subtleStyle.Render("│")

	scroll := m.diff.scroll
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
			if i == m.diff.cursorLine {
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
	textRows := wrapTinted(hl.hlLine(line), line.Spans, baseBg, spanBg, textWidth)
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
	if m.errBar.text != "" {
		return padRight(m.statusMessage(" ✖", " ✔"), m.width)
	}
	if m.diff.notice != "" {
		return padRight(doneStyle.Render(" ✔ "+m.diff.notice), m.width)
	}
	if m.diff.sendConfirm {
		count := len(m.diff.annotations[m.reviewKey()])
		return padRight(errStyle.Render(fmt.Sprintf(" ¶ send %d %s to the agent?", count, commentNoun(count)))+
			subtleStyle.Render("  ↵/y send · esc cancel"), m.width)
	}
	return ""
}

func (m *Model) viewDiffFooter() string {
	if m.diff.annotating {
		return legendBar([]legendSection{{title: "Comment", pairs: [][2]string{
			{"↵", "save"}, {"esc", "cancel"},
		}}}, m.width)
	}
	repo := "repo"
	if len(m.diff.repoRoots) > 0 {
		repo = "repo: " + filepath.Base(m.diff.repoSel)
	}
	send := "send"
	if count := len(m.diff.annotations[m.reviewKey()]); count > 0 {
		send = fmt.Sprintf("send %d", count)
	}
	return legendBar([]legendSection{
		{title: "Review", pairs: [][2]string{
			{"c", "comment"}, {"d", "remove"}, {"C", send}, {"space", "reviewed"},
			{"s", "scope: " + m.diff.scope.String()}, {"r", repo}, {"b", "branch"}, {"B", "target"},
		}},
		{title: "Move", quiet: true, pairs: [][2]string{
			{"↑↓/jk", "scroll line"}, {"ctrl+d/ctrl+u", "half page"}, {"g/G", "top/bottom"},
			{"tab/J K/shift+tab", "file"}, {"n/N", "change"}, {"u", "layout"},
			{"esc/q", "close"}, {"ctrl+c", "quit"},
		}},
	}, m.width)
}
