package frame

import (
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestNavigationLegendPreservesNonClickableMovement(t *testing.T) {
	for _, label := range []string{"↑↓", "up/down", "tab/up/down", "up/k down/j", "shift+up/K/shift+down/J", "left/right", "left", "right", "up/down/jk", "drag / up/down / wheel"} {
		if _, ok := LegendKey(label); ok {
			t.Errorf("movement hint %q became clickable", label)
		}
	}
	for _, label := range []string{"^", "v", "<", ">", "/", "space/↵", "↵ / release", "esc"} {
		if _, ok := LegendKey(label); !ok {
			t.Errorf("action %q lost click", label)
		}
	}
	for _, width := range []int{24, 40, 100} {
		pairs := [][2]string{{presentation.NavigationLabel(presentation.GlyphModeBasic, "↑↓"), "move"}, {"↵", "save"}, {"esc", "cancel"}}
		for _, bar := range []Legend{PaintInline(pairs, width, LiveCap(presentation.GlyphModeBasic)), PaintLive([]Section{{Title: "Keys", Pairs: pairs}}, width, presentation.GlyphModeBasic)} {
			rows := strings.Split(ansi.Strip(bar.Text), "\n")
			for _, hit := range bar.Hits {
				if hit.Key.String() != "enter" && hit.Key.String() != "esc" {
					t.Fatalf("movement hit: %+v", hit)
				}
				if hit.X1 > ansi.StringWidth(rows[hit.Y]) {
					t.Fatal("hit beyond painted row")
				}
				if _, ok := bar.Hits.At(hit.X1-1, hit.Y); !ok {
					t.Fatal("visible action lost its hit")
				}
			}
		}
	}
}
