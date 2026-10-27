package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/feed"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestClickOnAMessageTitleSelectsIt(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	m.layout.width, m.layout.height = 120, 34
	m.openNotices("")
	notices := m.activeNotices()
	if len(notices) < 2 {
		t.Fatalf("test setup: a fresh model shows two notices, has %d", len(notices))
	}
	x, y := paintedCell(t, m, notices[1].Title)
	if m = leftClick(m, x, y); m.notices.Cursor() != 1 || m.mode != modeNotices {
		t.Fatalf("a click on the second title should select it, cursor %d mode %v", m.notices.Cursor(), m.mode)
	}
	if body := preparedView(m); !strings.Contains(body, notices[1].Title) {
		t.Fatal("the selected notice should stay on screen")
	}
}

// stackedNotices opens messages at width columns, where the footer keys
// stack inside the card above its bottom edge: worded at 20 columns ("r
// refresh", "enter open", "x dismiss", "esc"), bare at 13 ("r", "enter",
// "x", "esc"), and cut to the card at 10 ("r", "e…", "x", "e…").
func stackedNotices(t *testing.T, width, height int) *Model {
	t.Helper()
	m := buildModel(t)
	m.layout.width, m.layout.height = width, height
	m.prefs.glyphMode = presentation.GlyphModeBasic
	m.notices.feedMessages = []feed.Message{{ID: "stacked", Title: "stacked", Body: []string{"content"}, URL: "https://example.com"}}
	m.openNotices("stacked")
	return m
}

func TestClickOnStackedNoticeKeys(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		width                       int
		refresh, open, dismiss, esc string
		offset                      int
	}{
		// A worded key takes the click on its word, too.
		{20, "│ r refresh", "│ enter open", "│ x dismiss", "│ esc", 4},
		{13, "│ r ", "│ enter", "│ x ", "│ esc", 2},
	} {
		t.Run(fmt.Sprintf("%d/esc", tc.width), func(t *testing.T) {
			m := stackedNotices(t, tc.width, 34)
			x, y := paintedCell(t, m, tc.esc)
			if m = leftClick(m, x+2, y); m.mode != modeList {
				t.Fatalf("a click on stacked esc should close messages, mode %v", m.mode)
			}
		})
		t.Run(fmt.Sprintf("%d/x", tc.width), func(t *testing.T) {
			m := stackedNotices(t, tc.width, 34)
			x, y := paintedCell(t, m, tc.dismiss)
			m = leftClick(m, x+tc.offset, y)
			m.drainEffects(t)
			if !m.notices.Dismissed("stacked") {
				t.Fatal("a click on stacked x should dismiss the notice")
			}
		})
		t.Run(fmt.Sprintf("%d/r", tc.width), func(t *testing.T) {
			m := stackedNotices(t, tc.width, 34)
			x, y := paintedCell(t, m, tc.refresh)
			if m = leftClick(m, x+2, y); !m.update.refreshing {
				t.Fatal("a click on stacked r should refresh")
			}
		})
		t.Run(fmt.Sprintf("%d/enter", tc.width), func(t *testing.T) {
			m := stackedNotices(t, tc.width, 34)
			x, y := paintedCell(t, m, tc.open)
			// The command opens the link; the test checks it is returned and
			// never runs it.
			if _, cmd := leftClickCmd(m, x+2+tc.offset, y); cmd == nil {
				t.Fatal("a click on stacked enter should open the link")
			}
		})
		t.Run(fmt.Sprintf("%d/padding", tc.width), func(t *testing.T) {
			m := stackedNotices(t, tc.width, 34)
			x, y := paintedCell(t, m, tc.refresh)
			cursor := m.notices.Cursor()
			// The border, the padding before the key and the fill after it.
			for _, col := range []int{x, x + 1, x + 2 + ansi.StringWidth(strings.TrimPrefix(tc.refresh, "│ ")) + 1} {
				m, cmd := leftClickCmd(m, col, y)
				if m.mode != modeNotices || m.notices.Cursor() != cursor || m.update.refreshing || cmd != nil {
					t.Fatalf("a click on column %d beside the key acted: mode %v cursor %d refreshing %v", col, m.mode, m.notices.Cursor(), m.update.refreshing)
				}
			}
		})
	}
}

// At 10 columns the card's inner width is 2, so esc paints cut as "e…":
// the cut key still closes the card, and the cells beside it take no click.
func TestClickOnACutNoticeKey(t *testing.T) {
	t.Parallel()
	m := stackedNotices(t, 10, 34)
	x, y := paintedCell(t, m, "│ e… │")
	for _, col := range []int{x, x + 1, x + 4, x + 5} {
		m, cmd := leftClickCmd(m, col, y)
		if m.mode != modeNotices || cmd != nil {
			t.Fatalf("a click on column %d beside the cut key acted: mode %v", col, m.mode)
		}
	}
	for _, col := range []int{x + 2, x + 3} {
		m := stackedNotices(t, 10, 34)
		if m = leftClick(m, col, y); m.mode != modeList {
			t.Fatalf("a click on column %d of the cut esc should close messages, mode %v", col, m.mode)
		}
	}
}

// A card taller than the terminal clips at the bottom, and a key cut off
// with it takes no click.
func TestClippedNoticeFooterTakesNoClick(t *testing.T) {
	t.Parallel()
	for _, width := range []int{10, 13, 20} {
		for height := 4; height <= 14; height++ {
			m := stackedNotices(t, width, height)
			preparedView(m)
			for _, hit := range m.layout.keyHits {
				if hit.y >= height {
					t.Fatalf("width %d height %d: a key hit %+v sits past the terminal", width, height, hit)
				}
			}
		}
	}
}
