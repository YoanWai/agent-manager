package ui

import (
	"strings"
	"testing"
)

func TestHeaderShowsUpdateBadge(t *testing.T) {
	m := &Model{width: 120, update: updateInfo{latest: "v0.9.0"}}
	if got := m.headerScope(); !strings.Contains(got, "v0.9.0") || !strings.Contains(got, "available") {
		t.Errorf("header missing update badge: %q", got)
	}
	m.update.latest = ""
	if got := m.headerScope(); strings.Contains(got, "available") {
		t.Errorf("header should have no badge when up to date: %q", got)
	}
}

func TestUpdateMsgSetsAndClearsBadge(t *testing.T) {
	m := &Model{width: 120}
	m.Update(updateMsg{latest: "v0.11.1", url: "https://example/rel"})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("badge not set: %q %q", m.update.latest, m.update.url)
	}
	m.Update(updateMsg{})
	if m.update.latest != "" || m.update.url != "" {
		t.Errorf("an up-to-date result should clear the badge: %q %q", m.update.latest, m.update.url)
	}
}

func TestFailedUpdateCheckKeepsBadge(t *testing.T) {
	m := &Model{width: 120, update: updateInfo{latest: "v0.11.1", url: "https://example/rel"}}
	m.Update(updateMsg{failed: true})
	if m.update.latest != "v0.11.1" || m.update.url != "https://example/rel" {
		t.Errorf("a failed check must leave the badge alone: %q %q", m.update.latest, m.update.url)
	}
}

func TestUpdateTickReArms(t *testing.T) {
	m := &Model{width: 120}
	if _, cmd := m.Update(updateTickMsg{}); cmd == nil {
		t.Error("update tick should re-arm the timer and re-check")
	}
}
