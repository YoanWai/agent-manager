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
		{actionKill, "Kill", "✕", "x"}, {actionArchive, "Archive", "◇", "A"},
		{actionRestore, "Restore", "◆", "R"}, {actionRestart, "Restart", "↻", "R"},
		{actionRevive, "Revive", "◆", "R"}, {actionDelete, "Delete", "⚠", "!"},
	}
	m := buildModel(t)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		m.prefs.glyphMode = mode
		for _, tc := range cases {
			m.confirm = confirmDialog{confirmTarget{action: tc.action, label: "❯⚠界? unchanged"}}
			mark := tc.basic
			if mode == presentation.GlyphModeAdvanced {
				mark = tc.advanced
			}
			if got := m.confirm.prompt().Title(m.prefs.glyphMode); got != mark+" "+tc.label+" session" {
				t.Fatalf("%s/%s title = %q", mode, tc.action, got)
			}
			if got := ansi.Strip(m.viewConfirm()); !strings.Contains(got, "❯⚠界?") {
				t.Fatalf("changed confirmation content: %q", got)
			}
		}
	}
}
