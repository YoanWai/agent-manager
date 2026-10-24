package ui

import (
	uidialogs "github.com/YoanWai/agent-manager/internal/ui/dialogs"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestConfirmMarksFollowModeWithoutChangingTheAction(t *testing.T) {
	cases := []struct {
		action                 uidialogs.ConfirmAction
		label, advanced, basic string
	}{
		{uidialogs.ConfirmKill, "Kill", "✕", "x"}, {uidialogs.ConfirmArchive, "Archive", "◇", "A"},
		{uidialogs.ConfirmRestore, "Restore", "◆", "R"}, {uidialogs.ConfirmRestart, "Restart", "↻", "R"},
		{uidialogs.ConfirmRevive, "Revive", "◆", "R"}, {uidialogs.ConfirmDelete, "Delete", "⚠", "!"},
	}
	m := buildModel(t)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		m.prefs.glyphMode = mode
		for _, tc := range cases {
			m.clearSurfaces()
			m.openConfirm(confirmTarget{action: tc.action, label: "❯⚠界? unchanged"})
			mark := tc.basic
			if mode == presentation.GlyphModeAdvanced {
				mark = tc.advanced
			}
			if got := mustTop(t, m).(*uidialogs.ConfirmSurface).Prompt().Title(m.prefs.glyphMode); got != mark+" "+tc.label+" session" {
				t.Fatalf("%s/%s title = %q", mode, tc.action, got)
			}
			if got := ansi.Strip(preparedView(m)); !strings.Contains(got, "❯⚠界?") {
				t.Fatalf("changed confirmation content: %q", got)
			}
		}
	}
}
