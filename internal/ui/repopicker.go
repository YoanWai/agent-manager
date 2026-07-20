package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type repoPickState struct {
	filter string
	cursor int
}

func (m *Model) openRepoPick() {
	if len(m.diff.repoRoots) < 2 {
		return
	}
	m.repoPick = repoPickState{}
	for i, root := range m.diff.repoRoots {
		if root == m.diff.repoSel {
			m.repoPick.cursor = i
			break
		}
	}
	m.mode = modeRepoPick
	m.err = ""
}

func (m *Model) filteredRepoRoots() []string {
	if m.repoPick.filter == "" {
		return m.diff.repoRoots
	}
	needle := strings.ToLower(m.repoPick.filter)
	var out []string
	for _, root := range m.diff.repoRoots {
		if strings.Contains(strings.ToLower(root), needle) {
			out = append(out, root)
		}
	}
	return out
}

func (m *Model) handleRepoPickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.filteredRepoRoots()
	// A reload can shrink repoRoots while the picker is open, stranding the cursor.
	if m.repoPick.cursor >= len(rows) {
		m.repoPick.cursor = max(0, len(rows)-1)
	}
	switch msg.Type {
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
		return m, m.selectRepo(rows[m.repoPick.cursor])
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

func (m *Model) repoPickRow(root string, selected bool) string {
	marker := "  "
	nameStyle := lipgloss.NewStyle()
	if selected {
		marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
		nameStyle = nameStyle.Foreground(colorAccent).Bold(true)
	}
	name := filepath.Base(root)
	budget := m.cardWidth() - 2*cardPaddingX - lipgloss.Width(marker) - lipgloss.Width(name) - 2
	dir := ""
	if budget > 1 {
		dir = subtleStyle.Render("  " + truncateTail(filepath.Dir(root), budget))
	}
	return marker + nameStyle.Render(name) + dir
}

func (m *Model) viewRepoPick() string {
	rows := m.filteredRepoRoots()
	var body strings.Builder
	body.WriteString(mutedStyle.Render("filter: ") + m.repoPick.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no repo matches"))
	}
	start, end := m.repoPickWindow(len(rows))
	for i := start; i < end; i++ {
		body.WriteString(m.repoPickRow(rows[i], i == m.repoPick.cursor) + "\n")
	}
	if hidden := len(rows) - (end - start); hidden > 0 {
		body.WriteString(subtleStyle.Render(fmt.Sprintf("+%d more", hidden)) + "\n")
	}
	return m.card("⌥ Review repo", strings.TrimRight(body.String(), "\n"), "type to filter · ↑↓ pick · ↵ select · esc cancel")
}
