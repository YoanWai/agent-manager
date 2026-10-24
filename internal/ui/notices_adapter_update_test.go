package ui

import (
	"testing"

	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
)

func TestUpdateMsgSetsAndClearsBadge(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	m.Update(uirelease.UpdateMsg{Latest: "v0.11.1", URL: "https://example/rel"})
	if m.release.Latest != "v0.11.1" || m.release.URL != "https://example/rel" {
		t.Errorf("badge not set: %q %q", m.release.Latest, m.release.URL)
	}
	m.Update(uirelease.UpdateMsg{})
	if m.release.Latest != "" || m.release.URL != "" {
		t.Errorf("an up-to-date result should clear the badge: %q %q", m.release.Latest, m.release.URL)
	}
}

func TestFailedUpdateCheckKeepsBadge(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120},
		release: uirelease.State{Latest: "v0.11.1", URL: "https://example/rel"},
	}
	m.Update(uirelease.UpdateMsg{Failed: true})
	if m.release.Latest != "v0.11.1" || m.release.URL != "https://example/rel" {
		t.Errorf("a failed check must leave the badge alone: %q %q", m.release.Latest, m.release.URL)
	}
}

func TestUpdateTickReArms(t *testing.T) {
	m := &Model{
		layout: layoutState{width: 120},
	}
	if _, cmd := m.Update(uirelease.TickMsg{}); cmd == nil {
		t.Error("update tick should re-arm the timer and re-check")
	}
}
