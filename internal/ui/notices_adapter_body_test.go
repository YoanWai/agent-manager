package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/feed"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func scrollModel(t *testing.T, lines int) *Model {
	t.Helper()
	m := modalModel(t)
	m.layout.width, m.layout.height = 70, 14
	var body []string
	for index := 0; index < lines; index++ {
		body = append(body, fmt.Sprintf("change line %02d", index))
	}
	m.notices.feedMessages = []feed.Message{{ID: "feed-scroll", Banner: "scroll", Title: "Scrollable summary", Body: body}}
	m.openNotices("feed-scroll")
	return m
}

func thumbRows(frame string) []int {
	var rows []int
	for index, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "┃") {
			rows = append(rows, index)
		}
	}
	return rows
}

func TestShortBodyHasNoScrollbar(t *testing.T) {
	m := scrollModel(t, 2)
	if frame := ansi.Strip(preparedView(m)); strings.Contains(frame, "┃") {
		t.Fatalf("a body that fits needs no scrollbar:\n%s", frame)
	}
}

func TestWheelScrollsTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	before := thumbRows(ansi.Strip(preparedView(m)))
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	if m.notices.Offset() != uinotices.WheelRows {
		t.Fatalf("wheel down moved %d rows, want %d", m.notices.Offset(), uinotices.WheelRows)
	}
	for i := 0; i < 40; i++ {
		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress})
	}
	limit := m.notices.ScrollLimit(m.activeNotices(), m.noticesFrame())
	if m.notices.Offset() != limit {
		t.Fatalf("wheel scrolled to %d, want it bounded at %d", m.notices.Offset(), limit)
	}
	after := thumbRows(ansi.Strip(preparedView(m)))
	if len(before) == 0 || len(after) == 0 || after[0] <= before[0] {
		t.Fatalf("the thumb should have moved down: before %v, after %v", before, after)
	}
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.notices.Offset() != limit-uinotices.WheelRows {
		t.Fatalf("wheel up moved to %d, want %d", m.notices.Offset(), limit-uinotices.WheelRows)
	}
	if m.mode != modeNotices {
		t.Fatalf("the wheel must not leave the panel, mode=%v", m.mode)
	}
}

func TestHomeAndEndJumpTheMessageBody(t *testing.T) {
	m := scrollModel(t, 30)
	limit := m.notices.ScrollLimit(m.activeNotices(), m.noticesFrame())
	if limit <= 0 {
		t.Fatalf("the body should scroll, limit %d", limit)
	}
	for _, jump := range []string{"end", "G"} {
		m.handleNoticesKey(key("home"))
		m.handleNoticesKey(key(jump))
		if m.notices.Offset() != limit {
			t.Fatalf("%s moved to %d, want the bottom at %d", jump, m.notices.Offset(), limit)
		}
	}
	for _, jump := range []string{"home", "g"} {
		m.handleNoticesKey(key("end"))
		m.handleNoticesKey(key(jump))
		if m.notices.Offset() != 0 {
			t.Fatalf("%s moved to %d, want the top", jump, m.notices.Offset())
		}
	}
}

func TestGrowingTheTerminalKeepsTheLastPageInView(t *testing.T) {
	m := scrollModel(t, 30)
	m.handleNoticesKey(key("end"))
	bottom := m.notices.Offset()
	m.layout.height = 20
	frame := ansi.Strip(preparedView(m))
	if !strings.Contains(frame, "change line 29") || !strings.Contains(frame, "╰") {
		t.Fatalf("an offset past the new last page shows that last page inside the frame:\n%s", frame)
	}
	limit := m.notices.ScrollLimit(m.activeNotices(), m.noticesFrame())
	if limit >= bottom {
		t.Fatalf("the taller terminal should have fewer pages: limit %d, saved offset %d", limit, bottom)
	}
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress})
	if m.notices.Offset() != limit-uinotices.WheelRows {
		t.Fatalf("the first scroll up starts from the page on screen: offset %d, want %d", m.notices.Offset(), limit-uinotices.WheelRows)
	}
}

func TestHintNamesHomeAndEndWhenItFits(t *testing.T) {
	m := modalModel(t)
	m.notices.feedMessages = []feed.Message{{ID: "feed-wide", Banner: "wide", Title: "Wide", Body: []string{strings.Repeat("wide words ", 12)}}}
	m.openNotices("feed-wide")
	m.layout.width = 180
	if frame := ansi.Strip(preparedView(m)); !strings.Contains(frame, "pgup/pgdn/home/end scroll") {
		t.Fatalf("a wide frame names home and end:\n%s", frame)
	}
	m.layout.width = 80
	frame := ansi.Strip(preparedView(m))
	for _, want := range []string{"x dismiss", "esc"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("a narrow frame keeps %q:\n%s", want, frame)
		}
	}
	if strings.Contains(frame, "home/end") {
		t.Fatalf("a narrow frame keeps the short hint:\n%s", frame)
	}
}
