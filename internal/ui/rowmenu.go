package ui

import (
	"strings"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/status"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// menuAttach is the attach entry: which key attaches follows the Enter setting.
const menuAttach = "menu:attach"

const (
	menuMinWidth = 24
	menuMaxWidth = 40
	// menuChrome is the two border columns and the space inside each, and
	// menuKeyGap the least room between an entry's label and its key.
	menuChrome = 4
	menuKeyGap = 3
	// menuButtonWidth is the gap and glyph every row keeps at its end for
	// its menu button.
	menuButtonWidth = 2
)

func menuButton(selected bool, bg string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(restingMarkHex()))
	if selected {
		style = keyStyle
	}
	return paint(" "+style.Render("⋯"), menuButtonWidth, bg)
}

// onMenuButton reports whether (x, y) lands on a row's menu button: the
// last cells of the row's first painted line. The rail starts one column
// in, past its edge cell.
func (m *Model) onMenuButton(x, y, row int) bool {
	return x >= m.railWidth-menuButtonWidth && x <= m.railWidth && m.onRowHead(y, row)
}

// menuItem is one row of the context menu. An empty label is a separator.
type menuItem struct {
	label  string
	action string
	danger bool
}

// rowMenu is the row's actions, opened by its ⋯ button or a right click.
// Its box is where the last frame painted it, so clicks resolve against
// what the user saw.
type rowMenu struct {
	active bool
	// held is a menu opened by a press still down: its release over an
	// entry picks it, the way a native menu answers press, drag, release.
	held             bool
	key              string
	title            string
	items            []menuItem
	index            int
	anchorX, anchorY int
	left, top        int
	width, height    int
}

func (m *Model) openRowMenu(row, x, y int) tea.Cmd {
	cmd := m.selectRow(row)
	entry := m.rows[row]
	title := groupLabel(entry.group)
	if !entry.isGroup {
		title = m.displayName(entry.sess)
	}
	m.menu = rowMenu{active: true, key: rowKey(entry), title: title, items: m.rowMenuItems(entry), anchorX: x, anchorY: y}
	m.menu.index = m.menu.nextItem(-1, 1)
	return cmd
}

func (m *Model) rowMenuItems(entry treeRow) []menuItem {
	separator := menuItem{}
	create := []menuItem{
		{label: "New session", action: keybind.NewSession},
		{label: "New terminal", action: keybind.Terminal},
	}
	manage := []menuItem{
		{label: "Open in editor", action: keybind.Editor},
		{label: "Rename", action: keybind.Rename},
		{label: "Move to group", action: keybind.Move},
	}
	if entry.isRoot() {
		return append(create, menuItem{label: "Open in editor", action: keybind.Editor})
	}
	if entry.isGroup {
		items := append(append(append(create, separator), manage...), separator)
		live, dead := m.groupLiveAndDead(entry.group)
		if dead {
			items = append(items, menuItem{label: "Revive", action: keybind.Revive})
		}
		items = append(items, m.archiveMenuItem())
		if live {
			items = append(items, menuItem{label: "Kill", action: keybind.Kill, danger: true})
		}
		return append(items, menuItem{label: "Delete", action: keybind.Delete, danger: true})
	}
	sess := entry.sess
	if sess.Archived {
		return []menuItem{
			{label: "Restore", action: keybind.Restore},
			{label: "Delete", action: keybind.Delete, danger: true},
		}
	}
	if sess.Status == status.Dead {
		return append(append(manage, separator),
			menuItem{label: "Revive", action: keybind.Revive},
			m.archiveMenuItem(),
			menuItem{label: "Delete", action: keybind.Delete, danger: true})
	}
	items := []menuItem{{label: "Attach", action: menuAttach}, separator}
	if !m.isShell(sess.Tool) {
		items = append(items,
			menuItem{label: "Prompt", action: keybind.Prompt},
			menuItem{label: "Copy last reply", action: keybind.CopyReply},
			menuItem{label: "Review changes", action: keybind.Review},
			menuItem{label: "Fork", action: keybind.Fork},
			menuItem{label: "New terminal", action: keybind.Terminal},
			separator)
	}
	return append(append(append(items, manage...), separator),
		menuItem{label: "Restart", action: keybind.Restart},
		m.archiveMenuItem(),
		menuItem{label: "Kill", action: keybind.Kill, danger: true},
		menuItem{label: "Delete", action: keybind.Delete, danger: true})
}

// groupLiveAndDead reports whether the group holds a session to kill and
// one to revive, so the menu offers only what would do something.
func (m *Model) groupLiveAndDead(group string) (live, dead bool) {
	for _, sess := range m.sessionsInGroup(group) {
		if sess.Status == status.Dead {
			dead = true
		} else {
			live = true
		}
	}
	return live, dead
}

func (m *Model) archiveMenuItem() menuItem {
	if m.showArchived {
		return menuItem{label: "Restore", action: keybind.Restore}
	}
	return menuItem{label: "Archive", action: keybind.Archive}
}

// menuGlyph is the key that does the same thing, so the menu teaches it.
func (m *Model) menuGlyph(action string) string {
	if action != menuAttach {
		return m.listGlyph(action)
	}
	if m.enterFocuses() {
		return m.listGlyph(keybind.Attach)
	}
	return m.listGlyph(keybind.Open)
}

// nextItem walks from index in direction step to the next real entry,
// stopping at the ends.
func (menu rowMenu) nextItem(index, step int) int {
	for i := index + step; i >= 0 && i < len(menu.items); i += step {
		if menu.items[i].label != "" {
			return i
		}
	}
	if index < 0 {
		return 0
	}
	return index
}

// runMenuItem runs an entry on the row the menu belongs to, which a
// rebuild may have dropped from the list meanwhile.
func (m *Model) runMenuItem(index int) (tea.Model, tea.Cmd) {
	item := m.menu.items[index]
	key := m.menu.key
	m.menu = rowMenu{}
	if !m.selectRowByKey(key) {
		return m, nil
	}
	if item.action == menuAttach {
		return m.attachSelected()
	}
	return m.runListAction(item.action)
}

func (m *Model) handleMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := keybind.Normalize(msg.String())
	action, _ := m.listKeys.ActionFor(key)
	switch {
	case key == "ctrl+c":
		return m, tea.Quit
	case key == "esc":
		m.menu = rowMenu{}
	case key == "enter" || key == "space":
		return m.runMenuItem(m.menu.index)
	case key == "up" || action == keybind.Up:
		m.menu.index = m.menu.nextItem(m.menu.index, -1)
	case key == "down" || action == keybind.Down:
		m.menu.index = m.menu.nextItem(m.menu.index, 1)
	default:
		for i, item := range m.menu.items {
			if item.label != "" && item.action == action {
				return m.runMenuItem(i)
			}
		}
	}
	return m, nil
}

// handleMenuMouse runs the entry a left press lands on. A press anywhere
// else closes the menu; a right press on another row reopens it there.
func (m *Model) handleMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if tea.MouseEvent(msg).IsWheel() {
		step := 1
		if msg.Button == tea.MouseButtonWheelUp {
			step = -1
		}
		m.menu.index = m.menu.nextItem(m.menu.index, step)
		return m, nil
	}
	index, onItem := m.menuItemAt(msg.X, msg.Y)
	switch msg.Action {
	case tea.MouseActionMotion:
		if onItem {
			m.menu.index = index
		}
		return m, nil
	case tea.MouseActionRelease:
		held := m.menu.held
		m.menu.held = false
		if held && onItem {
			return m.runMenuItem(index)
		}
		return m, nil
	}
	if onItem {
		if msg.Button == tea.MouseButtonLeft {
			return m.runMenuItem(index)
		}
		return m, nil
	}
	m.menu = rowMenu{}
	if msg.Button == tea.MouseButtonRight {
		if row, ok := m.clickRow(msg.X, msg.Y); ok {
			return m, m.openRowMenu(row, msg.X, msg.Y)
		}
	}
	return m, nil
}

func (m *Model) menuItemAt(x, y int) (int, bool) {
	menu := m.menu
	if x <= menu.left || x >= menu.left+menu.width-1 {
		return 0, false
	}
	index := y - menu.top - 1
	if index < 0 || index >= len(menu.items) || menu.items[index].label == "" {
		return 0, false
	}
	return index, true
}

// overlayRowMenu paints the menu beside the pointer that opened it,
// flipped up or left where the frame runs out, and records where it went.
func (m *Model) overlayRowMenu(frame string) string {
	if !m.menu.active {
		return frame
	}
	box := m.renderRowMenu()
	m.menu.width, m.menu.height = maxLineWidth(box), len(box)
	left := m.menu.anchorX + 1
	if left+m.menu.width > m.width {
		left = max(m.menu.anchorX-m.menu.width, 0)
	}
	top := m.menu.anchorY
	if top+m.menu.height > m.height {
		top = max(m.height-m.menu.height, 0)
	}
	m.menu.left, m.menu.top = left, top
	lines := strings.Split(frame, "\n")
	for i, patch := range box {
		if row := top + i; row < len(lines) {
			lines[row] = spliceAtColumn(lines[row], patch, left)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderRowMenu() []string {
	glyphs := make([]string, len(m.menu.items))
	need := menuMinWidth
	for i, item := range m.menu.items {
		if item.label == "" {
			continue
		}
		glyphs[i] = m.menuGlyph(item.action)
		need = max(need, ansi.StringWidth(item.label)+ansi.StringWidth(glyphs[i])+menuChrome+menuKeyGap)
	}
	width := min(need, menuMaxWidth, max(m.width, menuMinWidth))
	inner := width - menuChrome
	border := cardBorderStyle()
	edge := border.Render("│")
	title := ansi.Truncate(m.menu.title, max(width-8, 1), "…")
	rows := []string{cardTitleRow(width, title, border)}
	for i, item := range m.menu.items {
		if item.label == "" {
			rows = append(rows, paint(border.Render("├"+strings.Repeat("─", width-2)+"┤"), width, blockHex()))
			continue
		}
		glyph := glyphs[i]
		gap := max(inner-ansi.StringWidth(item.label)-ansi.StringWidth(glyph), 1)
		if i == m.menu.index {
			ink := lipgloss.NewStyle().Foreground(colorBg).Background(colorAccent).Bold(true)
			if item.danger {
				ink = ink.Background(colorErrored)
			}
			line := ink.Render(" " + item.label + strings.Repeat(" ", gap) + glyph + " ")
			rows = append(rows, paint(edge+line+edge, width, blockHex()))
			continue
		}
		label := lipgloss.NewStyle().Foreground(colorText).Render(item.label)
		if item.danger {
			label = lipgloss.NewStyle().Foreground(colorErrored).Render(item.label)
		}
		line := " " + label + strings.Repeat(" ", gap) + subtleStyle.Render(glyph) + " "
		rows = append(rows, paint(edge+line+edge, width, blockHex()))
	}
	return append(rows, paint(border.Render("╰"+strings.Repeat("─", width-2)+"╯"), width, blockHex()))
}
