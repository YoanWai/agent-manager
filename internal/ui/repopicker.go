package ui

import (
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
		return m, m.selectRepo(rows[min(m.repoPick.cursor, len(rows)-1)])
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

func (m *Model) viewRepoPick() string {
	rows := m.filteredRepoRoots()
	var body strings.Builder
	body.WriteString(mutedStyle.Render("filter: ") + m.repoPick.filter + "\n\n")
	if len(rows) == 0 {
		body.WriteString(subtleStyle.Render("no repo matches"))
	}
	for i, root := range rows {
		marker := "  "
		line := filepath.Base(root) + subtleStyle.Render("  "+filepath.Dir(root))
		if i == min(m.repoPick.cursor, len(rows)-1) {
			marker = lipgloss.NewStyle().Foreground(colorAccent).Render("❯ ")
			line = lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render(filepath.Base(root)) +
				subtleStyle.Render("  "+filepath.Dir(root))
		}
		body.WriteString(marker + line + "\n")
	}
	return m.card("⌥ Review repo", strings.TrimRight(body.String(), "\n"), "type to filter · ↑↓ pick · ↵ select · esc cancel")
}
