package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickRow is one selectable target: label is what the user reads (a repo base
// name or a worktree's branch), root is the path selectRepo retargets to.
type pickRow struct {
	label string
	root  string
}

// rows is snapshotted at open because a refresh landing behind the picker would reorder rows under the cursor.
type repoPickState struct {
	rows   []pickRow
	filter string
	cursor int
	title  string
}

func (m *Model) openRepoPick() {
	if len(m.diff.repoRoots) < 2 {
		return
	}
	rows := make([]pickRow, len(m.diff.repoRoots))
	for i, root := range m.diff.repoRoots {
		rows[i] = pickRow{label: filepath.Base(root), root: root}
	}
	m.openPick(rows, "⌥ Review repo")
}

// openBranchPick lists the currently selected repo's worktrees, one branch per
// row, so the user can retarget review to another worktree. Listing shells out
// synchronously; a failure stays in review with the error shown.
func (m *Model) openBranchPick() tea.Cmd {
	root := m.diff.set.Repo.Root
	if m.gitDrv == nil || root == "" {
		m.err = "no repo under review"
		return nil
	}
	worktrees, err := m.gitDrv.Worktrees(root)
	if err != nil {
		m.err = err.Error()
		return nil
	}
	rows := make([]pickRow, len(worktrees))
	for i, wt := range worktrees {
		rows[i] = pickRow{label: wt.Branch, root: wt.Root}
	}
	m.openPick(rows, "⌥ Review branch")
	return nil
}

func (m *Model) openPick(rows []pickRow, title string) {
	m.repoPick = repoPickState{rows: rows, title: title}
	for i, row := range rows {
		if row.root == m.diff.repoSel {
			m.repoPick.cursor = i
			break
		}
	}
	m.mode = modeRepoPick
	m.err = ""
}

func (m *Model) filteredRows() []pickRow {
	if m.repoPick.filter == "" {
		return m.repoPick.rows
	}
	needle := strings.ToLower(m.repoPick.filter)
	var out []pickRow
	for _, row := range m.repoPick.rows {
		if strings.Contains(strings.ToLower(row.label), needle) ||
			strings.Contains(strings.ToLower(row.root), needle) {
			out = append(out, row)
		}
	}
	return out
}

func (m *Model) handleRepoPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.filteredRows()
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		m.mode = modeDiff
		return m, nil
	case tea.KeyUp:
		m.moveRepoPickCursor(-1, len(rows))
		return m, nil
	case tea.KeyDown:
		m.moveRepoPickCursor(1, len(rows))
		return m, nil
	case tea.KeyBackspace:
		if m.repoPick.filter != "" {
			m.repoPick.filter = m.repoPick.filter[:len(m.repoPick.filter)-1]
			m.repoPick.cursor = 0
		}
		return m, nil
	case tea.KeyEnter:
		if len(rows) == 0 {
			return m, nil
		}
		m.mode = modeDiff
		return m, m.selectRepo(rows[m.repoPick.cursor].root)
	case tea.KeyRunes:
		m.repoPick.filter += string(msg.Runes)
		m.repoPick.cursor = 0
		return m, nil
	}
	return m, nil
}

func (m *Model) moveRepoPickCursor(delta, count int) {
	if count == 0 {
		m.repoPick.cursor = 0
		return
	}
	m.repoPick.cursor = (m.repoPick.cursor + delta + count) % count
}

func (m *Model) selectRepo(root string) tea.Cmd {
	sess, ok := m.diffSession()
	if !ok {
		m.err = "session is gone"
		return nil
	}
	m.diff.repoSel = root
	if m.pickedRepos == nil {
		m.pickedRepos = map[string]string{}
	}
	m.pickedRepos[sess.ID] = root
	m.diff.gen++
	m.diff.loading = true
	m.diff.errText = ""
	m.diff.fileIdx = 0
	m.diff.scroll = 0
	m.diff.cursorLine = 0
	return m.diffLoadCmd(sess, m.diff.scope, m.diff.gen, m.diff.repoSel, false)
}

func (m *Model) repoPickWindow(count int) (start, end int) {
	visible := max(3, m.height-repoPickChrome)
	if count <= visible {
		return 0, count
	}
	start = m.repoPick.cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > count {
		start = count - visible
	}
	return start, start + visible
}

// Card lines around the rows: border, padding, title, spacers, error, hint, "+N more".
const repoPickChrome = 12

func (m *Model) repoPickRow(row pickRow, selected bool) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle()
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		nameStyle = nameStyle.Foreground(colorAccent).Bold(true)
	}
	inner := m.cardWidth() - 2*cardPaddingX
	name := truncateTail(row.label, inner-lipgloss.Width(marker))
	budget := inner - lipgloss.Width(marker) - lipgloss.Width(name) - 2
	dir := ""
	if budget > 1 {
		dir = subtleStyle.Render("  " + truncateTail(filepath.Dir(row.root), budget))
	}
	return marker + nameStyle.Render(name) + dir
}

func (m *Model) viewRepoPick() string {
	rows := m.filteredRows()
	var body strings.Builder
	body.WriteString(mutedStyle.Render("filter: ") + m.repoPick.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no match"))
	}
	start, end := m.repoPickWindow(len(rows))
	for i := start; i < end; i++ {
		body.WriteString(m.repoPickRow(rows[i], i == m.repoPick.cursor) + "\n")
	}
	if hidden := len(rows) - (end - start); hidden > 0 {
		body.WriteString(subtleStyle.Render(fmt.Sprintf("+%d more", hidden)) + "\n")
	}
	return m.card(m.repoPick.title, strings.TrimRight(body.String(), "\n"), "type to filter · ↑↓ pick · ↵ select · esc cancel")
}
