package ui

import (
	"strings"
	"testing"
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
