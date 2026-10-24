package ui

import (
	uirelease "github.com/YoanWai/agent-manager/internal/ui/notices/release"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestHeaderShowsUpdateBadgeBesideWordmark(t *testing.T) {
	m := &Model{
		layout:  layoutState{width: 120},
		release: uirelease.State{Latest: "v0.9.0"},
	}
	header := ansi.Strip(m.viewHeaderRows()[0])
	if !strings.Contains(header, "v0.9.0") || !strings.Contains(header, "available") {
		t.Errorf("header missing update badge: %q", header)
	}
	if strings.Index(header, "available") > strings.Index(header, "agents") {
		t.Errorf("badge should sit left, by the wordmark: %q", header)
	}
	m.release.Latest = ""
	if header := ansi.Strip(m.viewHeaderRows()[0]); strings.Contains(header, "available") {
		t.Errorf("header should have no badge when up to date: %q", header)
	}
}
