package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/tmux"
	"github.com/YoanWai/agent-manager/internal/ui/composer"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uiquick "github.com/YoanWai/agent-manager/internal/ui/quick"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func TestRetainedQuickPromptReadsTheCurrentGlyphMode(t *testing.T) {
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetWidth(70)
	m.quick().Input.SetValue("❯↳⚙ literal input")
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, ">"}, {presentation.GlyphModeAdvanced, "❯"}, {presentation.GlyphModeBasic, ">"}} {
		m.prefs.glyphMode = tc.mode
		got := ansi.Strip(m.quick().Input.View())
		if !strings.Contains(got, tc.mark+" ❯↳⚙ literal input") {
			t.Fatalf("retained prompt in %s = %q", tc.mode, got)
		}
	}
}

func TestQuickWorktreeMarkKeepsItsKeyboardAndMouseTarget(t *testing.T) {
	m := buildModel(t)
	if err := m.services.store.CreateGroup("work", gitTestRepo(t)); err != nil {
		t.Fatal(err)
	}
	m.applyCmd(t, m.refreshCmd())
	m.selectGroupRow(t, "work")
	m.openQuickMode()
	for _, tc := range []struct {
		mode presentation.GlyphMode
		mark string
	}{{presentation.GlyphModeBasic, "Y"}, {presentation.GlyphModeAdvanced, "⎇"}} {
		m.prefs.glyphMode = tc.mode
		for _, width := range []int{80, 120} {
			m.layout.width, m.layout.height = width, 30
			m.quick().SetWorktree(false)
			applyQuickKeyCmd(t, m, tea.KeyMsg{Type: tea.KeyCtrlT})
			if !m.quickWorktreeOn() {
				t.Fatal("keyboard did not enable worktree")
			}
			frame := ansi.Strip(preparedView(m))
			if !strings.Contains(frame, tc.mark+" on") {
				t.Fatalf("%s/%d worktree mark missing:\n%s", tc.mode, width, frame)
			}
			found := false
			for _, hit := range m.quick().Hits() {
				if hit.Action == uiquick.ClickWorktree {
					found = true
					m = leftClick(m, m.quick().OriginX+hit.X0, m.quick().OriginY+hit.Line)
					break
				}
			}
			if !found || m.quickWorktreeOn() {
				t.Fatalf("%s/%d worktree click missed", tc.mode, width)
			}
		}
	}
}

// ctrl+o in the quick prompt puts a saved prompt at the caret as a chip,
// and the send carries its text where the chip stood.
func TestQuickSavedPromptChipSendsItsText(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	savePrompt(t, m, "Role", " \n instruct\n")
	createSession(t, m, "answer-me", t.TempDir(), "")
	m.selectSessionRow(t, "answer-me")
	m.openQuickMode()
	var sent string
	m.services.quickSendText = func(_ *tmux.Driver, _ string, text string) (tmux.SendResult, error) {
		sent = text
		return tmux.SendResult{}, nil
	}
	m.quick().Input.SetValue("then ")
	_, load := m.handleQuickKey(tea.KeyMsg{Type: tea.KeyCtrlO})
	m.runBatch(t, load)
	m.handleQuickKey(tea.KeyMsg{Type: tea.KeyEnter})
	if got := m.quick().Input.Value(); got != "then "+composer.SavedToken("Role")+" " {
		t.Fatalf("chip landed as %q", got)
	}
	if !strings.Contains(ansi.Strip(m.quick().Composer.View(composerPaint())), "[saved prompt:Role]") {
		t.Fatal("the chip is not painted")
	}
	for _, r := range "go" {
		m.handleQuickKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.submitQuick()
	m.applyTestMsg(t, cmd())
	if sent != "then  \n instruct\n go" || m.errBar.text != "" {
		t.Fatalf("sent %q, err %q", sent, m.errBar.text)
	}
	if m.quick().Input.Value() != "" || len(m.quick().Saved) != 0 {
		t.Fatal("a confirmed send kept the chip")
	}
}

// pushQuick puts a bare bar on the stack, for a test that needs one open
// without the settings and catalog reads openQuickMode starts.
func pushQuick(m *Model) *uiquick.Surface {
	q := newQuickSurface(m)
	q.Composer = composer.Composer{Input: textarea.New()}
	m.push(q)
	return q
}

// The bar opens by its key, holds its draft through a stray click, and
// closes on esc, letting go of its images.
func TestQuickBarIsASurface(t *testing.T) {
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	createSession(t, m, "alpha", t.TempDir(), "")
	m.selectSessionRow(t, "alpha")
	m.applyCmd(t, m.openQuickMode())
	if m.quick() == nil {
		t.Fatalf("the quick bar did not open, err %q", m.errBar.text)
	}
	if mustTop(t, m).Dismissible() {
		t.Fatal("the bar holds a draft and must not close on a click")
	}
	if m.cardOpen() || !m.listClear() {
		t.Fatal("the bar sits on the list; it is no card over it")
	}
	image := tempImage(t, "draft.png")
	m.quick().Attachments = []composer.Attachment{{ID: 1, Path: image}}
	m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if m.quick() != nil || len(m.surfaces) != 0 {
		t.Fatal("esc did not close the bar")
	}
	if imageExists(t, image) {
		t.Fatal("closing the bar kept its image")
	}
}

// A settings read that lands after its bar closed, or on a newer bar, is
// dropped.
func TestStaleQuickCompletionIsDropped(t *testing.T) {
	m := buildModel(t)
	m.applyCmd(t, m.openQuickMode())
	gen := m.quick().Gen
	m.closeQuick()
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a closed bar still answers its generation")
	}
	m.applyCmd(t, m.openQuickMode())
	if _, ok := m.quickFor(gen); ok {
		t.Fatal("a newer bar answered an older generation")
	}
}

func TestQuickFooterKeepsTheCaretRowOnScreen(t *testing.T) {
	t.Parallel()
	for _, full := range []bool{false, true} {
		m := buildModel(t)
		seedTwoGroups(t, m)
		setRailCursor(m, 1)
		m.prefs.fullLayout = full
		m.layout.width = 56
		m.layout.height = 12
		m.openQuickMode()

		painted := func() string {
			frame := preparedView(m)
			if rows := len(strings.Split(frame, "\n")); rows > m.layout.height {
				t.Fatalf("full layout %v: the frame painted %d rows into a %d row terminal", full, rows, m.layout.height)
			}
			return ansi.Strip(frame)
		}
		painted()
		m = applyMsg(t, m, composer.PasteTextMsg{
			Target: composer.Quick,
			Gen:    m.quick().Gen,
			Inner: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(
				"FIRSTROWMARK " + strings.Repeat("filler word ", 20) + "LASTROWMARK")},
		})
		painted()

		for i := 0; i < 40 && !m.quick().CaretOnFirstRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyUp})
			painted()
		}
		if !m.quick().CaretOnFirstRow() {
			t.Fatal("up never reached the prompt's first row")
		}
		if !strings.Contains(painted(), "FIRSTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's first row", full)
		}

		for i := 0; i < 40 && !m.quick().CaretOnLastRow(); i++ {
			_, _ = m.handleQuickKey(tea.KeyMsg{Type: tea.KeyDown})
			painted()
		}
		if !m.quick().CaretOnLastRow() {
			t.Fatal("down never reached the prompt's last row")
		}
		if !strings.Contains(painted(), "LASTROWMARK") {
			t.Fatalf("full layout %v: the frame clipped the prompt's last row", full)
		}
	}
}

func TestQuickBarMeasuresRowsAtTheWidthItJustSet(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.openQuickMode()
	m.quick().Input.SetWidth(80)
	m.quick().Input.SetValue("one two three four five six seven eight nine ten")
	bar := m.viewQuickBar(14, uiquick.BarMaxRows)
	if rows := len(uitheme.SplitLines(bar)) - 1; rows < 2 {
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
			m.layout.width = width
			answered(m, claudeLike, claudeAnswer)
			m.openQuickMode()
			m.quick().Input.SetValue("hello 界 " + strings.Repeat("wrapping ", 12))
			frame := preparedView(m)
			frameRows := uitheme.SplitLines(frame)
			if len(frameRows) != m.layout.height || strings.TrimSpace(strings.TrimPrefix(ansi.Strip(frameRows[len(frameRows)-1]), quickEdge)) == "" {
				t.Fatalf("full %v width %d footer must end on the last terminal row", full, width)
			}
			backgrounds, _ := cellColors(frame, m.layout.width, m.layout.height)
			footerHeight := lipgloss.Height(m.viewFooter())
			keysHeight := lipgloss.Height(paintLegend([]legendSection{{title: quickModeTitle, pairs: m.quick().Legend(m.quickBarHost())}}, m.layout.width-1).text)
			if !strings.Contains(ansi.Strip(frameRows[m.layout.height-footerHeight]), "new") {
				t.Fatalf("full %v width %d footer must begin with the target", full, width)
			}
			if m.quick().OriginY != m.layout.height-footerHeight {
				t.Fatalf("target hit row = %d, want %d", m.quick().OriginY, m.layout.height-footerHeight)
			}
			for y := m.layout.height - footerHeight; y < m.layout.height; y++ {
				if !strings.HasPrefix(ansi.Strip(frameRows[y]), quickEdge) {
					t.Fatalf("footer row %d must carry the accent edge", y)
				}
				if cells := ansi.StringWidth(frameRows[y]); cells != m.layout.width {
					t.Fatalf("band row %d width = %d, want %d", y, cells, m.layout.width)
				}
				for x := 0; x < m.layout.width; x++ {
					if y == m.layout.height-keysHeight && x >= quickGutter && x < quickGutter+ansi.StringWidth(uitheme.LegendBadgeStyle().Render(quickModeTitle)) {
						continue
					}
					want := uitheme.QuickModeHex()
					if y > m.layout.height-footerHeight && y < m.layout.height-keysHeight {
						want = uitheme.BlockHex()
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
	t.Parallel()
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
	m.quick().Input.SetValue(strings.Repeat("filler word ", 40))
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

	m.closeQuick()
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
