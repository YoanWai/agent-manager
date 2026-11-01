package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
)

func TestNavigationNoticesKeepExternalText(t *testing.T) {
	const literal = "literal ↑↓←→↕"
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		src := testSources("")
		src.Feed = []Message{{ID: "literal", Title: literal, Body: []string{literal}}}
		p := NewPanel(nil, nil)
		notices := p.Active(src)
		found := false
		for _, n := range notices {
			if n.ID == "literal" {
				found = true
				p.Open(notices, n.ID)
				if n.Title != literal || n.Body[0] != literal {
					t.Fatal("external notice rewritten")
				}
				for _, width := range []int{24, 40, 66, 100} {
					f := testFrame()
					f.Width = width
					f.GlyphMode = mode
					for _, pair := range p.footPairs(notices, width, f) {
						if mode == presentation.GlyphModeBasic && strings.ContainsAny(pair[0], "↑↓←→") {
							t.Fatal("footer leaked nav glyphs")
						}
					}
				}
			}
		}
		if !found {
			t.Fatal("external notice missing")
		}
	}
}
