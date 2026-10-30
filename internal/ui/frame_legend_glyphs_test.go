package ui

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
				paintLegend([]legendSection{{title: "Keys", pairs: pairs}}, width, mode),
			} {
				rows := strings.Split(ansi.Strip(painted.text), "\n")
				if mode == presentation.GlyphModeBasic && strings.ContainsAny(painted.text, "↵⌫") {
					t.Fatalf("basic hint leaked glyphs: %s", painted.text)
				}
				for _, hit := range painted.hits {
					want := map[string]string{"enter": presentation.KeyLabel(mode, "↵/tab") + " choose", "backspace": presentation.KeyLabel(mode, "⌫") + " delete", "y": presentation.KeyLabel(mode, "y/↵") + " confirm", "esc": "esc cancel"}[hit.key.String()]
					got := ansi.Cut(rows[hit.y], hit.x0, hit.x1)
					if got != ansi.Truncate(want, hit.x1-hit.x0, "…") {
						t.Fatalf("%s/%d hit=%q want=%q", mode, width, got, want)
					}
					for _, x := range []int{hit.x0, hit.x1 - 1} {
						clicked, ok := painted.hits.at(x, hit.y)
						if !ok || clicked.key.String() != hit.key.String() {
							t.Fatal("click lost key identity")
						}
					}
				}
				for key, text := range map[string]string{"enter": "↵/tab choose", "backspace": "⌫ delete", "y": "y/↵ confirm", "esc": "esc cancel"} {
					shown := strings.Contains(strings.Join(rows, "\n"), presentation.KeyLabel(mode, text))
					found := false
					for _, hit := range painted.hits {
						found = found || hit.key.String() == key
					}
					if shown && !found {
						t.Fatalf("%s/%d %s shown=%t hit=%t", mode, width, key, shown, found)
					}
				}
			}
		}
	}
}

func TestCompatibleDialogHintsPreserveContentAndModelIsolation(t *testing.T) {
	const literal = "↵⌫🬹🬂🬨 user content"
	basic, advanced := shotModel(), shotModel()
	basic.prefs.glyphMode = presentation.GlyphModeBasic
	advanced.prefs.glyphMode = presentation.GlyphModeAdvanced
	for _, m := range []*Model{basic, advanced, basic} {
		got := ansi.Strip(m.card("title", literal, [][2]string{{"↵", "save"}, {"esc", "cancel"}}))
		if !strings.Contains(got, literal) {
			t.Fatalf("content changed: %s", got)
		}
		want := presentation.KeyLabel(m.prefs.glyphMode, "↵") + " save"
		if !strings.Contains(got, want) {
			t.Fatalf("model hint missing %q: %s", want, got)
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
			painted := paintLegend([]legendSection{{title: "Keys", pairs: pairs}}, width, presentation.GlyphModeBasic)
			rows := strings.Split(ansi.Strip(painted.text), "\n")
			for _, row := range rows {
				if ansi.StringWidth(row) > width {
					t.Fatalf("width %d overflows with %q", width, row)
				}
			}
			for _, hit := range painted.hits {
				if hit.x0 >= hit.x1 || hit.x1 > ansi.StringWidth(rows[hit.y]) || hit.x1 > width {
					t.Fatalf("width %d invisible hit %+v on %q", width, hit, rows[hit.y])
				}
				for _, x := range []int{hit.x0, hit.x1 - 1} {
					clicked, ok := painted.hits.at(x, hit.y)
					if !ok || clicked.key.String() != hit.key.String() {
						t.Fatal("clipped hint lost its key")
					}
				}
			}
			if _, ok := painted.hits.at(width, 0); ok {
				t.Fatal("click outside the footer activated a hint")
			}
		}
	}
}

func TestCompatibleLegendClipsOnlyThePaintedPart(t *testing.T) {
	painted := paintLegend([]legendSection{{title: "Keys", pairs: [][2]string{{"⌫", "delete"}}}}, 24, presentation.GlyphModeBasic)
	if got := ansi.Strip(painted.text); got != "   Keys      backspace …" {
		t.Fatalf("narrow first hint=%q", got)
	}
	if len(painted.hits) != 1 || painted.hits[0].x0 != 13 || painted.hits[0].x1 != 24 || painted.hits[0].key.String() != "backspace" {
		t.Fatalf("clipped hit=%+v", painted.hits)
	}
}
