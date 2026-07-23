package ui

import (
	"strings"
	"testing"
)

func TestHeaderShowsUpdateBadge(t *testing.T) {
	m := &Model{width: 120, updateLatest: "v0.9.0"}
	if got := m.viewHeader(); !strings.Contains(got, "v0.9.0") || !strings.Contains(got, "available") {
		t.Errorf("header missing update badge: %q", got)
	}
	m.updateLatest = ""
	if got := m.viewHeader(); strings.Contains(got, "available") {
		t.Errorf("header should have no badge when up to date: %q", got)
	}
}
