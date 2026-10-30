package spawn

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleFormChoicesKeepValuesAndClickRows(t *testing.T) {
	const literal = "◂▸╭╮╰╯━╸┃┄▐▜▟▀▄▍▎▏▌║"
	h := newFakeFormHost()
	d := NewForm(h, []string{literal}, 0)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		st := savedStyles()
		st.GlyphMode = mode
		body, _ := d.View(h, FormFacts{Width: 80, Height: 40, Styles: st, Chrome: func([][2]string) int { return 6 }, WorktreeKnown: true, WorktreeCapable: true})
		plain := ansi.Strip(body)
		left, right := "<", ">"
		if mode == presentation.GlyphModeAdvanced {
			left, right = "◂", "▸"
		}
		if !strings.Contains(plain, left+" "+literal+" "+right) || !strings.Contains(plain, left+" off "+right) {
			t.Fatalf("%s choices=%s", mode, plain)
		}
		for i, line := range strings.Split(plain, "\n") {
			if strings.Contains(line, left+" "+literal+" "+right) {
				d.focus = FieldName
				d.Click(h, i)
				if d.focus != FieldTool {
					t.Fatal("tool click row moved")
				}
			}
		}
	}
}
