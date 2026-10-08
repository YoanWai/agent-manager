package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestQuickFooterKeepsTheCaretRowOnScreen(t *testing.T) {
	for _, full := range []bool{false, true} {
		m := buildModel(t)
		seedTwoGroups(t, m)
		setRailCursor(m, 1)
		m.prefs.fullLayout = full
		m.width = 56
		m.height = 12
		m.openQuickMode()

		painted := func() string {
			frame := preparedView(m)
			if rows := len(strings.Split(frame, "\n")); rows > m.height {
				t.Fatalf("full layout %v: the frame painted %d rows into a %d row terminal", full, rows, m.height)
			}
			return ansi.Strip(frame)
		}
		painted()
		m = applyMsg(t, m, pasteTextMsg{
			target: composerQuick,
			gen:    m.quick.gen,
			inner: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(
				"FIRSTROWMARK " + strings.Repeat("filler word ", 20) + "LASTROWMARK")},
		})
		painted()

		for i := 0; i < 40 && !m.quick.caretOnFirstRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
			painted()
		}
		if !m.quick.caretOnFirstRow() {
			t.Fatal("up never reached the prompt's first row")
		}
		if !strings.Contains(painted(), "FIRSTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's first row", full)
		}

		for i := 0; i < 40 && !m.quick.caretOnLastRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
			painted()
		}
		if !m.quick.caretOnLastRow() {
			t.Fatal("down never reached the prompt's last row")
		}
		if !strings.Contains(painted(), "LASTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's last row", full)
		}
	}
}

func TestQuickBarMeasuresRowsAtTheWidthItJustSet(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick.input.SetWidth(80)
	m.quick.input.SetValue("one two three four five six seven eight nine ten")
	bar := m.viewQuickBar(14, quickBarMaxRows)
	if rows := len(splitLines(bar)) - 1; rows < 2 {
		t.Fatalf("rows = %d, want the wrap at width 14, not the previous width", rows)
	}
}

func TestQuickFooterFillsEveryBandCell(t *testing.T) {
	useTrueColor(t)
	for _, full := range []bool{false, true} {
		for _, width := range []int{56, 120, 180} {
			m := buildModel(t)
			seedTwoGroups(t, m)
			setRailCursor(m, 1)
			m.prefs.fullLayout = full
			m.width = width
			answered(m, claudeLike, claudeAnswer)
			m.openQuickMode()
			m.quick.input.SetValue("hello 界 " + strings.Repeat("wrapping ", 12))
			frame := preparedView(m)
			frameRows := splitLines(frame)
			if len(frameRows) != m.height || strings.TrimSpace(strings.TrimPrefix(ansi.Strip(frameRows[len(frameRows)-1]), quickEdge)) == "" {
				t.Fatalf("full %v width %d footer must end on the last terminal row", full, width)
			}
			backgrounds, _ := cellColors(frame, m.width, m.height)
			footerHeight := lipgloss.Height(m.viewFooter())
			keysHeight := lipgloss.Height(legendBar([]legendSection{{title: quickModeTitle, pairs: m.quickLegend()}}, m.width-1))
			if !strings.Contains(ansi.Strip(frameRows[m.height-footerHeight]), "new") {
				t.Fatalf("full %v width %d footer must begin with the target", full, width)
			}
			if m.quick.originY != m.height-footerHeight {
				t.Fatalf("target hit row = %d, want %d", m.quick.originY, m.height-footerHeight)
			}
			for y := m.height - footerHeight; y < m.height; y++ {
				if !strings.HasPrefix(ansi.Strip(frameRows[y]), quickEdge) {
					t.Fatalf("footer row %d must carry the accent edge", y)
				}
				if cells := ansi.StringWidth(frameRows[y]); cells != m.width {
					t.Fatalf("band row %d width = %d, want %d", y, cells, m.width)
				}
				for x := 0; x < m.width; x++ {
					if y == m.height-keysHeight && x >= quickGutter && x < quickGutter+ansi.StringWidth(legendBadgeStyle.Render(quickModeTitle)) {
						continue
					}
					want := quickModeHex()
					if y > m.height-footerHeight && y < m.height-keysHeight {
						want = blockHex()
					}
					if !sameColor(backgrounds[y][x], hexColor(want)) {
						t.Fatalf("full %v width %d row %d col %d background = %v, want %s", full, width, y, x, backgrounds[y][x], want)
					}
				}
			}
		}
	}
}

// Resizing a pane for prompt growth makes agents redraw their transcripts.
func TestQuickBarKeepsPaneHeight(t *testing.T) {
	m := buildModel(t)
	createSession(t, m, "sizer", t.TempDir(), "")
	m.applyCmd(t, m.refreshCmd())
	sess := railSelectedSession(m)
	pinned := m.previewPaneHeight()

	rows := make([]string, pinned+10)
	for i := range rows {
		rows[i] = fmt.Sprintf("line%03d", i+1)
	}
	m.workspace.preview = strings.Join(rows, "\n") + "\n"
	listed := paintedPreview(m)

	m.openQuickMode()
	m.quick.input.SetValue(strings.Repeat("filler word ", 40))
	if got := m.previewPaneHeight(); got != pinned {
		t.Fatalf("box height with the quick bar open = %d, want %d", got, pinned)
	}
	painted := paintedPreview(m)
	// The bar's rows come off the top of the view; the pane's live end stays.
	if oldest := rows[len(rows)-pinned]; !strings.Contains(listed, oldest) || strings.Contains(painted, oldest) {
		t.Fatalf("the quick bar should have cropped %s off the top:\n%s", oldest, painted)
	}
	if newest := rows[len(rows)-1]; !strings.Contains(painted, newest) {
		t.Fatalf("the quick bar hid the pane's live end (%s):\n%s", newest, painted)
	}
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height with the quick bar open = %d, want %d", got, pinned)
	}

	m.quick.active = false
	m.applyCmd(t, m.refreshCmd())
	if got := windowHeight(t, sess.ID); got != pinned {
		t.Fatalf("pane height after closing the quick bar = %d, want %d", got, pinned)
	}
}

func paintedPreview(m *Model) string {
	_, rightWidth := m.splitWidths()
	var painted strings.Builder
	for _, row := range m.contentLines(rightWidth, m.listBodyHeight()) {
		painted.WriteString(ansi.Strip(row.text) + "\n")
	}
	return painted.String()
}
