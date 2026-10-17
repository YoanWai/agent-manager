package rail_test

import (
	"slices"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/rail"
)

func TestRowLegendReadsTheRowRootPasses(t *testing.T) {
	model := rail.New([]string{"work"})
	ctx := rail.LegendContext{ListKeys: keybind.DefaultList(), EnterFocuses: true}
	if legend := model.RowLegend(ctx); legend.Title != "" || legend.Pairs != nil {
		t.Fatalf("no row should give no tier, got %+v", legend)
	}

	ctx.RowOK, ctx.Row = true, rail.Selection{Kind: rail.GroupRow, Group: "work"}
	group := model.RowLegend(ctx)
	if group.Title != "Group" || !slices.Contains(group.Pairs, [2]string{"↵", "unfold"}) {
		t.Fatalf("a folded group should offer unfolding, got %+v", group)
	}

	ctx.Row = rail.Selection{Kind: rail.SessionRow, SessionID: "sh"}
	ctx.Session = rail.Session{ID: "sh", Status: "finished", IsShell: true}
	shell := model.RowLegend(ctx)
	if shell.Title != "Shell" || shell.Leads != nil || shell.Hint != "" {
		t.Fatalf("a shell row should drop the conversation leads, got %+v", shell)
	}
	if !slices.ContainsFunc(shell.Pairs, func(pair [2]string) bool { return pair[1] == "mark idle" }) {
		t.Fatalf("a finished row should offer mark idle, got %+v", shell.Pairs)
	}
}

func TestViewLegendCarriesTheUnreadAlert(t *testing.T) {
	model := rail.New(nil)
	ctx := rail.LegendContext{ListKeys: keybind.DefaultList()}
	if legend := model.ViewLegend(ctx); legend.Alerts != nil || !legend.Quiet {
		t.Fatalf("nothing unread should raise no alert, got %+v", legend)
	}
	ctx.Unread = [2]string{"m", "unread"}
	if legend := model.ViewLegend(ctx); !slices.Equal(legend.Alerts, [][2]string{{"m", "unread"}}) {
		t.Fatalf("unread messages should lead the tier, got %+v", legend.Alerts)
	}
}
