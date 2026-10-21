package review

import (
	"fmt"
	"path/filepath"
	"strings"

	diff "github.com/YoanWai/agent-manager/internal/diff/model"
	git "github.com/YoanWai/agent-manager/internal/git/value"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	diffFileRailWidth = 28
	diffCodeMinWidth  = 20
	diffPaneSeam      = 2

	annotationInputMaxRows = 5
)

// Styles is a snapshot of the root UI's active presentation values. State
// never retains it, so a theme change reaches the next render immediately.
type Styles struct {
	Muted      lipgloss.Style
	Subtle     lipgloss.Style
	Value      lipgloss.Style
	Err        lipgloss.Style
	Done       lipgloss.Style
	Annotation lipgloss.Style
	Section    lipgloss.Style
	Rule       lipgloss.Style
	Chip       lipgloss.Style
	Selected   lipgloss.Style

	Accent   lipgloss.Color
	Accent2  lipgloss.Color
	Finished lipgloss.Color
	Errored  lipgloss.Color

	// SelectedReapply replaces every SGR reset inside a selected row so the
	// row stays tinted end-to-end.
	SelectedReapply     string
	AnnotationBg        string
	HandledAnnotationBg string
}

// RenderContext is the copied presentation state Review paints with.
type RenderContext struct {
	GlyphMode    presentation.GlyphMode
	Styles       Styles
	CursorMarker string
}

// Pane is one review column's content. Loading names what the pane still
// waits for; root paints its shared loader there instead of Text.
type Pane struct {
	Text    string
	Loading string
}

// LegendSection is one footer tier; root renders it with the shared legend.
type LegendSection struct {
	Title string
	Quiet bool
	Pairs [][2]string
}

type renderer struct {
	m *Model
	s Styles
	g presentation.GlyphSet
}

func (m *Model) renderer(ctx RenderContext) renderer {
	return renderer{m: m, s: ctx.Styles, g: presentation.GlyphsFor(ctx.GlyphMode)}
}

func (s Styles) selectRow(row string) string {
	return s.Selected.Render(strings.ReplaceAll(row, "\x1b[0m", s.SelectedReapply))
}

// keyPill renders a chip with the key that changes it dimmed in front, so
// the header doubles as a key legend.
func (s Styles) keyPill(key, text string, fg lipgloss.Color) string {
	return s.Subtle.Render(key+" ") + s.Chip.Foreground(fg).Render(text)
}

func (s Styles) divider(label string, width int) string {
	return presentation.Divider(label, width, s.Section, s.Rule)
}

// PaneWidths splits the terminal width between the file rail and the code.
func PaneWidths(width int) (fileWidth, codeWidth int) {
	fileWidth = max(width*24/100, diffFileRailWidth)
	if width-fileWidth-diffPaneSeam < diffCodeMinWidth {
		fileWidth = max(width-diffPaneSeam-diffCodeMinWidth, 0)
	}
	return fileWidth, max(width-fileWidth-diffPaneSeam, 0)
}

func (m *Model) AnnotationInputHeight(width int) int {
	inner := width - 2
	if inner < 4 {
		inner = 4
	}
	n := len(wrapTinted(m.AnnotationValue(), nil, "", "", inner))
	if n < 1 {
		n = 1
	}
	if n > annotationInputMaxRows {
		n = annotationInputMaxRows
	}
	return n
}

// annotationBar is the comment box docked under the code while typing.
func (r renderer) annotationBar(fd *diff.FileDiff, width int, cursorMarker string) string {
	num, _ := AnnotationLine(fd.Lines[r.m.CursorDiffLine()])
	return r.s.divider(fmt.Sprintf("Comment · %s:%d", presentation.EscapeControlsInline(fd.File.Path), num), width) + "\n" + r.m.AnnotationView(cursorMarker)
}

func (m *Model) EmptyText(ctx RenderContext) string { return m.renderer(ctx).emptyText() }

func (r renderer) emptyText() string {
	m := r.m
	if m.errText != "" {
		return r.s.Err.Render(r.g.DiffErrorGlyph + " " + presentation.EscapeControls(m.errText))
	}
	if m.target.ID == "" {
		return r.s.Muted.Render("(select a session to diff)")
	}
	if len(m.set.Files) == 0 {
		return r.s.Muted.Render(fmt.Sprintf("%s no changes (%s)", r.g.DiffCleanGlyph, m.scope)) + "\n" +
			r.s.Subtle.Render("s cycles scope")
	}
	return ""
}

// allHiddenNote is what both review panes fall back to when the code-only
// filter leaves nothing to show. It has to fit the narrow file rail.
func (r renderer) allHiddenNote() string {
	return r.s.Muted.Render("(no code files)") + "\n" +
		r.s.Subtle.Render("f shows the rest")
}

func (r renderer) bodyNote(fd *diff.FileDiff) string {
	switch {
	case r.m.fileHidden(fd):
		return r.allHiddenNote()
	case fd.Err != nil:
		return r.s.Err.Render(r.g.DiffErrorGlyph + " " + presentation.EscapeControls(fd.Err.Error()))
	case fd.Binary:
		return r.s.Muted.Render("(binary file)")
	case fd.Truncated && len(fd.Lines) == 0:
		return r.s.Muted.Render("(file too large to diff)")
	case len(fd.Lines) == 0:
		return r.s.Muted.Render("(empty file)")
	}
	return ""
}

func (m *Model) Header(ctx RenderContext, sessName string, width int) string {
	s := ctx.Styles
	state := m.Snapshot()
	layout := "unified"
	if state.SideBySide {
		layout = "split"
	}
	left := "  " + lipgloss.NewStyle().Foreground(s.Accent).Bold(true).Render("review · "+presentation.EscapeControlsInline(sessName))
	var keys []presentation.PaintedKey
	pill := func(key, text string, fg lipgloss.Color) {
		left += "  "
		x := ansi.StringWidth(left)
		left += s.keyPill(key, text, fg)
		keys = append(keys, presentation.PaintedKey{X0: x, X1: ansi.StringWidth(left), Label: key})
	}
	pill("s", state.Scope.String(), s.Accent2)
	pill("u", layout, s.Accent)
	if state.CodeOnly {
		pill("f", "code only", s.Accent)
	}
	if root := state.Set.Repo.Root; root != "" {
		name := filepath.Base(state.RepoSelected)
		if name == "" || name == "." {
			name = filepath.Base(root)
		}
		name = presentation.EscapeControlsInline(name)
		if len(state.RepoRoots) > 1 {
			name = fmt.Sprintf("%s · %d repos", name, len(state.RepoRoots))
		}
		pill("r", name, s.Accent)
		branch := presentation.EscapeControlsInline(state.Set.Repo.Branch)
		if state.Scope == git.ScopeBranch && state.Set.BaseDesc != "" && branch != "" {
			target := presentation.EscapeControlsInline(StripBaseHash(state.Set.BaseDesc))
			summary := target + " → " + branch
			if state.Set.BaseOverride == "" {
				summary += " " + s.Subtle.Render("(auto)")
			}
			pill("B", summary, s.Accent)
		} else if branch != "" {
			left += "  " + s.Subtle.Render(branch)
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
	right := s.Muted.Render(fmt.Sprintf("%d files", shown)) + s.Subtle.Render(" · ") +
		lipgloss.NewStyle().Foreground(s.Finished).Render(fmt.Sprintf("+%d", adds)) + " " +
		lipgloss.NewStyle().Foreground(s.Errored).Render(fmt.Sprintf("−%d", dels))
	if uncounted {
		right += " " + s.Muted.Render("+?")
	}
	if count := m.DraftCount(); count > 0 {
		right += s.Subtle.Render(" · ") + lipgloss.NewStyle().Foreground(s.Accent).Render(fmt.Sprintf("¶%d", count))
	}
	if round := m.Round(); round.Number > 0 {
		label := fmt.Sprintf("Review round %d", round.Number)
		if !state.Loading && round.Fingerprint != 0 &&
			(round.Scope != state.Scope.String() || round.Fingerprint != state.Fingerprint) {
			label += " · changed"
		}
		right += s.Subtle.Render(" · ") + s.Annotation.Render(label)
	}
	right += " "

	// A comment being typed or a send being asked owns the keys, so the
	// pills are not click targets then.
	m.headerKeys = nil
	if !m.annotationOpen && !m.sendConfirm {
		m.headerKeys = keys
	}
	gap := width - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 1 {
		budget := width - ansi.StringWidth(right)
		m.headerKeys = keptWithin(m.headerKeys, budget)
		if budget < 1 {
			return presentation.PadRight(right, width)
		}
		return presentation.PadRight(left, budget) + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// HeaderKeys are the header's key pills as last painted.
func (m *Model) HeaderKeys() []presentation.PaintedKey { return m.headerKeys }

// StatusKeys are the keys the status line last painted.
func (m *Model) StatusKeys() []presentation.PaintedKey { return m.statusKeys }

func keptWithin(keys []presentation.PaintedKey, width int) []presentation.PaintedKey {
	var kept []presentation.PaintedKey
	for _, key := range keys {
		if key.X1 <= width {
			kept = append(kept, key)
		}
	}
	return kept
}

func (m *Model) CodeTitle() string {
	fd := m.currentFile()
	if fd == nil || m.fileHidden(fd) {
		return "Diff"
	}
	return fd.File.Path
}

func (m *Model) FileList(ctx RenderContext, width, height int) Pane {
	r := m.renderer(ctx)
	s := r.s
	state := m.Snapshot()
	m.fileRows.Reset()
	if state.Loading && len(state.Set.Files) == 0 {
		return Pane{Loading: "loading diff"}
	}
	if empty := r.emptyText(); empty != "" {
		return Pane{Text: empty}
	}
	files := state.Set.Files
	shown := make([]int, 0, len(files))
	for i := range files {
		if !files[i].Hidden {
			shown = append(shown, i)
		}
	}
	if len(shown) == 0 {
		return Pane{Text: r.allHiddenNote()}
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
		b.WriteString(s.Subtle.Render(fmt.Sprintf("  ↑ %d more", start)) + "\n")
		m.fileRows.Show(shown[start-1], 1)
	}
	notes := map[string]int{}
	for _, note := range m.Annotations() {
		if !note.Resolved {
			notes[note.File]++
		}
	}
	for pos := start; pos < end; pos++ {
		i := shown[pos]
		fd := files[i]
		glyph := s.Subtle.Render(r.g.DiffPendingGlyph)
		if m.FileReviewed(fd.File.Path) {
			glyph = lipgloss.NewStyle().Foreground(s.Finished).Render(r.g.DiffDoneGlyph)
		}
		bar := " "
		if i == state.FileIndex {
			bar = lipgloss.NewStyle().Foreground(s.Accent).Render("▎")
		}
		counts := lipgloss.NewStyle().Foreground(s.Finished).Render(fmt.Sprintf("+%d", fd.Stat.Adds)) +
			" " + lipgloss.NewStyle().Foreground(s.Errored).Render(fmt.Sprintf("−%d", fd.Stat.Dels))
		if !fd.StatKnown() {
			counts = s.Muted.Render("?")
		}
		if fd.Binary || fd.Stat.Binary {
			counts = s.Muted.Render("binary")
		}
		if count := notes[fd.File.Path]; count > 0 {
			counts = lipgloss.NewStyle().Foreground(s.Accent).Render(fmt.Sprintf("¶%d ", count)) + counts
		}
		nameBudget := width - ansi.StringWidth(counts) - 6
		if nameBudget < 4 {
			nameBudget = 4
		}
		name := presentation.TruncatePath(presentation.EscapeControlsInline(fd.File.Path), nameBudget)
		left := bar + glyph + " " + s.Value.Render(name)
		gap := width - ansi.StringWidth(left) - ansi.StringWidth(counts)
		if gap < 1 {
			gap = 1
		}
		row := presentation.PadRight(left+strings.Repeat(" ", gap)+counts, width)
		if i == state.FileIndex {
			row = s.selectRow(row)
		}
		b.WriteString(row + "\n")
		m.fileRows.Show(i, 1)
	}
	if end < len(shown) {
		b.WriteString(s.Subtle.Render(fmt.Sprintf("  ↓ %d more", len(shown)-end)))
		m.fileRows.Show(shown[end], 1)
	}
	return Pane{Text: strings.TrimRight(b.String(), "\n")}
}

// Status is Review's own status line: a notice or the send confirmation.
// Root shows its error bar ahead of it.
func (m *Model) Status(ctx RenderContext, width int) string {
	s := ctx.Styles
	m.statusKeys = nil
	if m.notice != "" {
		return presentation.PadRight(s.Done.Render(" "+presentation.GlyphsFor(ctx.GlyphMode).DiffDoneGlyph+" "+presentation.EscapeControlsInline(m.notice)), width)
	}
	if m.sendConfirm {
		count := m.DraftCount()
		question := s.Err.Render(fmt.Sprintf(" ¶ send %d %s to the agent?", count, CommentNoun(count)))
		send := ansi.StringWidth(question) + 2
		cancel := send + ansi.StringWidth("↵/y send · ")
		m.statusKeys = keptWithin([]presentation.PaintedKey{
			{X0: send, X1: send + ansi.StringWidth("↵/y send"), Label: "↵/y"},
			{X0: cancel, X1: cancel + ansi.StringWidth("esc cancel"), Label: "esc"},
		}, width)
		return presentation.PadRight(question+s.Subtle.Render("  ↵/y send · esc cancel"), width)
	}
	return ""
}

func (m *Model) Legend() []LegendSection {
	if m.annotationOpen {
		return []LegendSection{{Title: "Comment", Pairs: [][2]string{
			{"↵", "save"}, {"esc", "cancel"},
		}}}
	}
	repo := "repo"
	if len(m.repoRoots) > 0 {
		repo = "repo: " + presentation.EscapeControlsInline(filepath.Base(m.repoSel))
	}
	send := "send"
	if count := m.DraftCount(); count > 0 {
		send = fmt.Sprintf("send %d", count)
	}
	filter := "code only"
	if m.codeOnly {
		filter = "all files"
	}
	return []LegendSection{
		{Title: "Review", Pairs: [][2]string{
			{"c", "comment"}, {"d", "remove/handle"}, {"C", send}, {"space", "reviewed"},
			{"s", "scope: " + m.scope.String()}, {"r", repo}, {"b", "branch"}, {"B", "target"},
		}},
		{Title: "Move", Quiet: true, Pairs: [][2]string{
			{"↑↓/jk", "scroll line"}, {"ctrl+d/ctrl+u", "half page"}, {"pgup/pgdn", "page"},
			{"g/G", "top/bottom"}, {"tab/J K/shift+tab", "file"}, {"n/N", "change"},
			{"u", "layout"}, {"f", filter}, {"o/f3", "open file"}, {"?", "keys"},
			{"esc/q", "close"}, {"ctrl+c", "quit"},
		}},
	}
}

// scrollWindow keeps the cursor visible inside a height-limited window of
// single-line rows, reserving one line for each overflow indicator.
func scrollWindow(total, cursor, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}
