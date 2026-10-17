package rail_test

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/rail"
	"github.com/charmbracelet/x/ansi"
)

func TestGroupRollupCountsTheSubtreeOnly(t *testing.T) {
	sessions := []rail.Session{
		{ID: "a", Group: "work", Status: "working"},
		{ID: "b", Group: "work/api", Status: "waiting"},
		{ID: "c", Group: "workshop", Status: "working"},
		{ID: "d", Group: "", Status: "idle"},
	}
	if got := rail.GroupSessionCount(sessions, "work"); got != 2 {
		t.Fatalf("work subtree count = %d, want 2", got)
	}
	counts := rail.GroupStatusCounts(sessions, "work")
	if counts["working"] != 1 || counts["waiting"] != 1 || len(counts) != 2 {
		t.Fatalf("work subtree counts = %v", counts)
	}
	if got := ansi.Strip(rail.GroupStatusBreakdown(rail.Theme{}, sessions, "work")); got != "1 working · 1 waiting" {
		t.Fatalf("breakdown = %q", got)
	}
	if got := rail.GroupStatusBreakdown(rail.Theme{}, sessions, "empty"); strings.TrimSpace(ansi.Strip(got)) != "" {
		t.Fatalf("an empty subtree should render no breakdown, got %q", got)
	}
}
