package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

func TestCompatibleNoticeFooterMeasuresDisplayedKeys(t *testing.T) {
	const literal = "↵⌫🬹🬂🬨 notice content"
	notices := []Notice{{ID: "custom", Title: literal, Body: []string{literal}, URL: "https://example.com"}}
	p := NewPanel(nil)
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{66, 80, 120} {
			f := testFrame()
			f.Width = width
			f.GlyphMode = mode
			rows := p.View(notices, f)
			frame := ansi.Strip(strings.Join(rows, "\n"))
			if !strings.Contains(frame, literal) {
				t.Fatal("notice content rewritten")
			}
			for _, hit := range p.Foot(notices, f) {
				got := ansi.Cut(ansi.Strip(rows[len(rows)-1]), hit.X0, hit.X1)
				want := presentation.KeyLabel(mode, hit.Label)
				if !strings.HasPrefix(got, want) {
					t.Fatalf("%s/%d label %q hit=%q", mode, width, hit.Label, got)
				}
			}
			if mode == presentation.GlyphModeBasic && strings.ContainsAny(ansi.Strip(rows[len(rows)-1]), "↵⌫") {
				t.Fatal("footer contains key glyphs")
			}
		}
	}
}

func TestCompatibleWelcomeFormatsRemappedKeys(t *testing.T) {
	src := testSources("")
	src.GlyphMode = presentation.GlyphModeBasic
	src.ListKeys = src.ListKeys.With(keybind.Settings, bindingOf(t, "backspace"))
	body := strings.Join(src.welcomeBody(), "\n")
	if strings.ContainsAny(body, "↵⌫") || !strings.Contains(body, "enter") || !strings.Contains(body, "backspace") {
		t.Fatalf("welcome key labels=%s", body)
	}
}

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

func TestCompatibleNarrowUpdateKeepsCloseMouseTarget(t *testing.T) {
	p := NewPanel(nil)
	notices := p.Active(testSources("v0.3.0"))
	p.Open(notices, "update-v0.3.0")
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, width := range []int{40, 42, 66, 100} {
			f := testFrame()
			f.Width = width
			f.GlyphMode = mode
			f.CloseKey = keybind.DefaultList().Binding(keybind.Messages)
			rows := p.View(notices, f)
			bottom := ansi.Strip(rows[len(rows)-1])
			found := false
			for _, hit := range p.Foot(notices, f) {
				if hit.Label == "esc/q/M" {
					found = true
					if got := ansi.Cut(bottom, hit.X0, hit.X1); !strings.HasPrefix(got, "esc/q/M") {
						t.Fatalf("%s/%d close target paints %q", mode, width, got)
					}
				}
			}
			if !found {
				t.Fatalf("%s/%d close mouse target missing: %s", mode, width, bottom)
			}
		}
	}
}
