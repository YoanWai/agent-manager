package frame

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleLegendLabelsKeepTheirKeyAndPaintedHits(t *testing.T) {
	pairs := [][2]string{{"↵/tab", "choose"}, {"⌫", "delete"}, {"y/↵", "confirm"}, {"esc", "cancel"}}
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{24, 40, 100} {
			for _, painted := range []legend{
				paintInline(pairs, width, mode),
				paintLegend([]legendSection{{Title: "Keys", Pairs: pairs}}, width, mode),
			} {
				rows := strings.Split(ansi.Strip(painted.Text), "\n")
				if mode == presentation.GlyphModeBasic && strings.ContainsAny(painted.Text, "↵⌫") {
					t.Fatalf("basic hint leaked glyphs: %s", painted.Text)
				}
				for _, hit := range painted.Hits {
					want := map[string]string{"enter": presentation.KeyLabel(mode, "↵/tab") + " choose", "backspace": presentation.KeyLabel(mode, "⌫") + " delete", "y": presentation.KeyLabel(mode, "y/↵") + " confirm", "esc": "esc cancel"}[hit.Key.String()]
					// A footer too narrow for the words paints the keys bare.
					bare := map[string]string{"enter": presentation.KeyLabel(mode, "↵/tab"), "backspace": presentation.KeyLabel(mode, "⌫"), "y": presentation.KeyLabel(mode, "y/↵"), "esc": "esc"}[hit.Key.String()]
					got := ansi.Cut(rows[hit.Y], hit.X0, hit.X1)
					if got != ansi.Truncate(want, hit.X1-hit.X0, "…") && got != bare {
						t.Fatalf("%s/%d hit=%q want=%q", mode, width, got, want)
					}
					for _, x := range []int{hit.X0, hit.X1 - 1} {
						clicked, ok := painted.Hits.At(x, hit.Y)
						if !ok || clicked.Key.String() != hit.Key.String() {
							t.Fatal("click lost key identity")
						}
					}
				}
				for key, text := range map[string]string{"enter": "↵/tab choose", "backspace": "⌫ delete", "y": "y/↵ confirm", "esc": "esc cancel"} {
					shown := strings.Contains(strings.Join(rows, "\n"), presentation.KeyLabel(mode, text))
					found := false
					for _, hit := range painted.Hits {
						found = found || hit.Key.String() == key
					}
					if shown && !found {
						t.Fatalf("%s/%d %s shown=%t hit=%t", mode, width, key, shown, found)
					}
				}
			}
		}
	}
}

func TestCompatibleLegendFitsFirstAndWrappedParts(t *testing.T) {
	for _, width := range []int{0, 1, 12, 24, 34, 40} {
		for _, pairs := range [][][2]string{
			{{"⌫", "delete"}},
			{{"↵", "go"}, {"⌫", "delete"}},
			{{"⌫", "delete"}, {"esc", "cancel"}, {"y", "confirm"}},
		} {
			painted := paintLegend([]legendSection{{Title: "Keys", Pairs: pairs}}, width, presentation.GlyphModeBasic)
			rows := strings.Split(ansi.Strip(painted.Text), "\n")
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("width %d overflows with %q", width, row)
				}
			}
			for _, hit := range painted.Hits {
				if hit.X0 >= hit.X1 || hit.X1 > ansi.StringWidth(rows[hit.Y]) || hit.X1 > width {
					t.Fatalf("width %d invisible hit %+v on %q", width, hit, rows[hit.Y])
				}
				for _, x := range []int{hit.X0, hit.X1 - 1} {
					clicked, ok := painted.Hits.At(x, hit.Y)
					if !ok || clicked.Key.String() != hit.Key.String() {
						t.Fatal("clipped hint lost its key")
					}
				}
			}
			if _, ok := painted.Hits.At(width, 0); ok {
				t.Fatal("click outside the footer activated a hint")
			}
		}
	}
}

// A first key too wide for its title's column drops the title rather than
// clip; a key wider than the whole row clips, and its hit covers only what
// was painted.
func TestCompatibleLegendClipsOnlyThePaintedPart(t *testing.T) {
	painted := paintLegend([]legendSection{{Title: "Keys", Pairs: [][2]string{{"⌫", "delete"}}}}, 24, presentation.GlyphModeBasic)
	if got := ansi.Strip(painted.Text); got != "  backspace delete" {
		t.Fatalf("narrow first hint=%q", got)
	}
	if len(painted.Hits) != 1 || painted.Hits[0].X0 != 2 || painted.Hits[0].X1 != 18 || painted.Hits[0].Key.String() != "backspace" {
		t.Fatalf("untitled hit=%+v", painted.Hits)
	}
	painted = paintLegend([]legendSection{{Title: "Keys", Pairs: [][2]string{{"⌫", "delete"}}}}, 10, presentation.GlyphModeBasic)
	if got := ansi.Strip(painted.Text); got != "  backspa…" {
		t.Fatalf("clipped bare hint=%q", got)
	}
	if len(painted.Hits) != 1 || painted.Hits[0].X0 != 2 || painted.Hits[0].X1 != 10 || painted.Hits[0].Key.String() != "backspace" {
		t.Fatalf("clipped hit=%+v", painted.Hits)
	}
}
