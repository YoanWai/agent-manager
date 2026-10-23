package ui

import (
	"testing"
)

func TestLinkPageMsgHandsTheTerminalOver(t *testing.T) {
	t.Parallel()
	m := buildModel(t)
	_, cmd := m.Update(linkPageMsg{url: "https://example.com/manual"})
	if cmd == nil || cmd() == nil {
		t.Fatal("a link page message started nothing")
	}
}
