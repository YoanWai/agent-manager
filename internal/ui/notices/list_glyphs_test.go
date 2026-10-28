package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestNavigationNoticesKeepExternalTextAndLiteralBindings(t *testing.T) {
	const literal = "literal ↑↓←→↕"
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		src := testSources("")
		src.GlyphMode = mode
		src.Feed = []Message{{ID: "literal", Title: literal, Body: []string{literal}}}
		src.ListKeys = src.ListKeys.With(keybind.Settings, bindingOf(t, "←"))
		if !strings.Contains(strings.Join(src.welcomeBody(), "\n"), "←") {
			t.Fatal("literal configured key changed")
		}
		notices := NewPanel(nil).Active(src)
		found := false
		for _, n := range notices {
			if n.ID == "literal" && (n.Title != literal || n.Body[0] != literal) {
				t.Fatal("external notice rewritten")
			}
			if n.ID == ArrowStep {
				found = true
				text := n.Title + strings.Join(n.Body, "\n")
				if mode == presentation.GlyphModeBasic && (strings.ContainsAny(text, "↑↓←→↕") || !strings.Contains(text, "arrow step in/out")) {
					t.Fatalf("basic beta notice: %s", text)
				}
				if mode == presentation.GlyphModeAdvanced && !strings.Contains(text, "←→ step in/out") {
					t.Fatal("advanced notice changed")
				}
			}
		}
		if !found {
			t.Fatal("beta notice missing")
		}
		for _, width := range []int{24, 40, 66, 100} {
			pairs := footPairs(notices, 0, width, mode, keybind.Binding{})
			for _, pair := range pairs {
				if mode == presentation.GlyphModeBasic && strings.ContainsAny(pair[0], "↑↓") {
					t.Fatal("footer leaked nav glyphs")
				}
			}
		}
	}
}

func TestCompatibleArrowNoticeKeepsBodyMeasure(t *testing.T) {
	widest := func(n Notice) int {
		width := 0
		for _, line := range measure(n) {
			width = max(width, ansi.StringWidth(line))
		}
		return width
	}
	src := testSources("")
	src.GlyphMode = presentation.GlyphModeAdvanced
	advanced := widest(src.arrowStepNotice(presentation.GlyphsFor(src.GlyphMode)))
	src.GlyphMode = presentation.GlyphModeBasic
	if basic := widest(src.arrowStepNotice(presentation.GlyphsFor(src.GlyphMode))); basic > advanced {
		t.Fatalf("Compatible body grew from %d to %d cells", advanced, basic)
	}
}
