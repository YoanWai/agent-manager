package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/status"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func forceANSI256(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestSelectedRowMetaUsesBrightNotSubtle(t *testing.T) {
	forceANSI256(t)

	m := &Model{}
	entry := treeRow{
		sess: store.Session{
			ID:        "s1",
			Name:      "demo-session",
			Tool:      "grok",
			Status:    status.Finished,
			CreatedAt: time.Now().Add(-3 * time.Hour),
		},
	}
	selected := m.renderTreeRow(entry, true, 80)
	unselected := m.renderTreeRow(entry, false, 80)

	if !strings.Contains(selected, "\x1b[") {
		t.Fatal("selected row has no SGR; color profile not active")
	}
	if strings.Contains(selected, "38;5;240") {
		t.Fatalf("selected row still uses subtle fg 240:\n%q", selected)
	}
	if !strings.Contains(unselected, "38;5;240") {
		t.Fatalf("unselected row should use subtle fg 240:\n%q", unselected)
	}
	if !strings.Contains(selected, "38;5;231") {
		t.Fatalf("selected row missing bright reapply fg 231:\n%q", selected)
	}
	if !strings.Contains(selected, " · grok · ") {
		t.Fatalf("selected missing meta text:\n%q", selected)
	}
}
