package dialogs

import (
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

// Follow mode changes the glyph, never the act the card commits.
func TestConfirmMarksFollowModeWithoutChangingTheAction(t *testing.T) {
	cases := []struct {
		action                 ConfirmAction
		label, advanced, basic string
	}{
		{ConfirmKill, "Kill", "✕", "x"}, {ConfirmArchive, "Archive", "◇", "A"},
		{ConfirmRestore, "Restore", "◆", "R"}, {ConfirmRestart, "Restart", "↻", "R"},
		{ConfirmRevive, "Revive", "◆", "R"}, {ConfirmDelete, "Delete", "⚠", "!"},
	}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, tc := range cases {
			prompt := Confirm{Action: tc.action, Label: "❯⚠界? unchanged"}
			mark := tc.basic
			if mode == presentation.GlyphModeAdvanced {
				mark = tc.advanced
			}
			if got := prompt.Title(mode); got != mark+" "+tc.label+" session" {
				t.Fatalf("%s/%s title = %q", mode, tc.action, got)
			}
			if got := prompt.Label; got != "❯⚠界? unchanged" {
				t.Fatalf("changed confirmation content: %q", got)
			}
		}
	}
}
