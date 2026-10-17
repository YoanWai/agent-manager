package review

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// PickerKind selects what a picker's Enter does: retarget the repo/branch, or
// set the diff base.
type PickerKind int

const (
	PickRepo PickerKind = iota
	PickBase
)

// PickerRow is one selectable target: Label is what the user reads (a repo base
// name, a worktree's branch, or a base ref), Root carries the path a repo pick
// retargets to, or the ref a base pick persists ("" for auto).
type PickerRow struct {
	Label string
	Root  string
}

// PickerSource is the review identity whose projection a picker shows.
// It follows the message through the worker and stays on the open picker so a
// late read or Enter cannot retarget a newer review.
type PickerSource struct {
	Generation int
	TargetID   string
	RepoRoot   string
}

// Picker is the review's repo, branch and base picker: the rows snapshotted at
// open, the filter and the cursor. Reading the rows and acting on a choice are
// root adapters (Git, SQLite, the effect lane).
//
// Rows are snapshotted at open because a refresh landing behind the picker
// would reorder rows under the cursor.
type Picker struct {
	rows      []PickerRow
	filter    string
	cursor    int
	title     string
	kind      PickerKind
	source    PickerSource
	storeRoot string
}

// PickerChoice is the row Enter picked, for root to retarget or persist.
type PickerChoice struct {
	Kind  PickerKind
	Value string
}

// PickerResult tells root how a key left the picker. Close returns to Review;
// Choice is set only on a close that picked a row.
type PickerResult struct {
	Close  bool
	Quit   bool
	Error  string
	Choice *PickerChoice
}

// Card lines around the rows: border, padding, title, spacers, error, hint, "+N more".
const pickerChrome = 12

// PickerSourceCurrent reports whether source still names this review's
// target, repo and generation.
func (m Model) PickerSourceCurrent(source PickerSource) bool {
	return m.active && m.gen == source.Generation && m.target.ID == source.TargetID && m.repoSel == source.RepoRoot
}

func (p *Picker) Open(rows []PickerRow, title string, kind PickerKind, current string, source PickerSource, storeRoot string) {
	*p = Picker{rows: rows, title: title, kind: kind, source: source, storeRoot: storeRoot}
	for i, row := range rows {
		if row.Root == current {
			p.cursor = i
			break
		}
	}
}

func (p Picker) Hints() [][2]string {
	return [][2]string{{"type", "filter"}, {"↑↓", "pick"}, {"↵", "select"}, {"esc", "cancel"}}
}

func (p Picker) Title() string             { return p.title }
func (p Picker) Cursor() int               { return p.cursor }
func (p Picker) Source() PickerSource      { return p.source }
func (p Picker) StoreRoot() string         { return p.storeRoot }
func (p Picker) Rows() []PickerRow         { return append([]PickerRow(nil), p.rows...) }
func (p Picker) FilteredRows() []PickerRow { return append([]PickerRow(nil), p.filteredRows()...) }

func (p Picker) filteredRows() []PickerRow {
	if p.filter == "" {
		return p.rows
	}
	needle := strings.ToLower(p.filter)
	var out []PickerRow
	for _, row := range p.rows {
		if strings.Contains(strings.ToLower(row.Label), needle) ||
			strings.Contains(strings.ToLower(row.Root), needle) {
			out = append(out, row)
		}
	}
	return out
}

// Key applies one key. sourceCurrent is whether the picker's source still
// names the open review; Enter on a stale picker closes it without a choice.
func (p *Picker) Key(msg tea.KeyMsg, sourceCurrent bool) PickerResult {
	rows := p.filteredRows()
	switch msg.Type {
	case tea.KeyCtrlC:
		return PickerResult{Quit: true}
	case tea.KeyEsc:
		return PickerResult{Close: true}
	case tea.KeyUp:
		p.moveCursor(-1, len(rows))
	case tea.KeyDown:
		p.moveCursor(1, len(rows))
	case tea.KeyBackspace:
		if p.filter != "" {
			p.filter = p.filter[:len(p.filter)-1]
			p.cursor = 0
		}
	case tea.KeyEnter:
		if len(rows) == 0 {
			return PickerResult{}
		}
		if !sourceCurrent {
			return PickerResult{Close: true, Error: "review changed; reopen the picker"}
		}
		return PickerResult{Close: true, Choice: &PickerChoice{Kind: p.kind, Value: rows[p.cursor].Root}}
	case tea.KeyRunes:
		p.filter += string(msg.Runes)
		p.cursor = 0
	}
	return PickerResult{}
}

func (p *Picker) moveCursor(delta, count int) {
	if count == 0 {
		p.cursor = 0
		return
	}
	p.cursor = (p.cursor + delta + count) % count
}

func (p Picker) window(height, count int) (start, end int) {
	visible := max(3, height-pickerChrome)
	if count <= visible {
		return 0, count
	}
	start = p.cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > count {
		start = count - visible
	}
	return start, start + visible
}

// row renders one picker row inside a card of inner width.
func (p Picker) row(s Styles, inner int, row PickerRow, selected bool) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle()
	if selected {
		marker = lipgloss.NewStyle().Foreground(s.Accent).Render("❯ ")
		nameStyle = nameStyle.Foreground(s.Accent).Bold(true)
	}
	name := presentation.TruncateTail(presentation.EscapeControlsInline(row.Label), inner-lipgloss.Width(marker))
	budget := inner - lipgloss.Width(marker) - lipgloss.Width(name) - 2
	dir := ""
	if budget > 1 {
		dir = s.Subtle.Render("  " + presentation.TruncateTail(presentation.EscapeControlsInline(filepath.Dir(row.Root)), budget))
	}
	return marker + nameStyle.Render(name) + dir
}

// Body is the card content for a terminal height rows tall and a card whose
// rows are inner columns wide; root wraps it in the shared card chrome.
func (p Picker) Body(ctx RenderContext, inner, height int) string {
	s := ctx.Styles
	rows := p.filteredRows()
	var body strings.Builder
	body.WriteString(s.Muted.Render("filter: ") + p.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(s.Subtle.Render("no match"))
	}
	start, end := p.window(height, len(rows))
	for i := start; i < end; i++ {
		body.WriteString(p.row(s, inner, rows[i], i == p.cursor) + "\n")
	}
	if hidden := len(rows) - (end - start); hidden > 0 {
		body.WriteString(s.Subtle.Render(fmt.Sprintf("+%d more", hidden)) + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}
