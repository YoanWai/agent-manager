package notices

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestNoticeFooterAtNarrowWidths(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, id := range []string{"", "custom", "update-test"} {
			for _, width := range []int{10, 12, 13, 15, 20, 30, 40, 80, 120, 160} {
				t.Run(fmt.Sprintf("%s/%s/%d", mode, id, width), func(t *testing.T) {
					p := NewPanel(nil, nil)
					notices := footNotices(id)
					f := testFrame()
					f.Width = width
					f.Height = 40
					f.GlyphMode = mode
					view := p.View(notices, f)
					inner, _ := inboxWidths(notices, width, p.splitView(notices, f))
					found := map[string]bool{}
					for _, hit := range p.Foot(notices, f) {
						row := len(view) - 1 + hit.Y
						if row < 0 || row >= len(view) || hit.X0 >= hit.X1 || hit.X1 > footStart+inner+1 {
							t.Fatalf("invisible footer target %+v", hit)
						}
						text := ansi.Cut(ansi.Strip(view[row]), hit.X0, hit.X1)
						want := presentation.KeyLabel(mode, hit.Label)
						if !strings.HasPrefix(text, ansi.Truncate(want, hit.X1-hit.X0, "…")) {
							t.Fatalf("%+v paints %q want %q", hit, text, want)
						}
						found[hit.Label] = true
					}
					for _, key := range []string{"r", "esc/q"} {
						if !found[key] {
							t.Fatalf("missing %s", key)
						}
					}
					if id != "" {
						for _, key := range []string{"↵", "x"} {
							if !found[key] {
								t.Fatalf("missing %s", key)
							}
						}
					}
					if id == "update-test" && !found["u"] {
						t.Fatal("missing update")
					}
					for _, row := range view {
						if ansi.StringWidth(row) > inner+4 {
							t.Fatalf("footer/card exceeds its width: %q", row)
						}
					}
				})
			}
		}
	}
}
func TestStackedNoticeFooterKeepsTheBodyBudget(t *testing.T) {
	notices := longNotice()
	for _, height := range []int{20, 30, 40} {
		p := NewPanel(nil, nil)
		f := testFrame()
		f.Width = 20
		f.Height = height
		f.GlyphMode = presentation.GlyphModeBasic
		view := p.View(notices, f)
		if len(view) > height {
			t.Fatalf("height %d paints %d rows", height, len(view))
		}
		p.Key(tea.KeyMsg{Type: tea.KeyEnd}, notices, f)
		if p.Offset() == 0 || p.Offset() != p.ScrollLimit(notices, f) {
			t.Fatal("end did not reach scroll limit")
		}
		view = p.View(notices, f)
		if !strings.Contains(strings.Join(view, "\n"), "line 30") {
			t.Fatal("footer hid last body line")
		}
	}
}
func TestStackedNoticeFooterKeepsConfiguredCloseKeys(t *testing.T) {
	for _, mode := range []presentation.GlyphMode{presentation.GlyphModeBasic, presentation.GlyphModeAdvanced} {
		for _, binding := range []string{"M", "alt+m", "backspace", "←"} {
			for _, width := range []int{13, 20, 40, 120} {
				for _, id := range []string{"", "custom"} {
					f := testFrame()
					f.Width, f.GlyphMode, f.CloseKey = width, mode, bindingOf(t, binding)
					p := NewPanel(nil, nil)
					notices := footNotices(id)
					view := p.View(notices, f)
					label := "esc/q/" + keybind.Keys(f.CloseKey.Keys()[0]).Text("", mode == presentation.GlyphModeAdvanced)
					found := false
					for _, hit := range p.Foot(notices, f) {
						if hit.Label != label {
							continue
						}
						found = true
						row := len(view) - 1 + hit.Y
						got := ansi.Cut(ansi.Strip(view[row]), hit.X0, hit.X1)
						want := ansi.Truncate(presentation.KeyLabel(mode, label), innerWidth(notices, width), "…")
						if got != want {
							t.Fatalf("%s/%s/%d close target paints %q, want %q", mode, binding, width, got, want)
						}
					}
					if !found {
						t.Fatalf("%s/%s/%d: no close target", mode, binding, width)
					}
					key := f.CloseKey.Keys()[0]
					msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'M'}}
					if key.Tea() == "alt+m" {
						msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}, Alt: true}
					}
					if key.Tea() == "←" {
						msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'←'}}
					}
					if key.Tea() == "backspace" {
						msg = tea.KeyMsg{Type: tea.KeyBackspace}
					}
					if out := p.Key(msg, notices, f); out.Action != Close {
						t.Fatalf("%s did not close Messages", binding)
					}
				}
			}
		}
	}
}

func TestStackedReleaseFooterKeepsSectionAndFilterKeys(t *testing.T) {
	p := NewPanel(nil, nil)
	notices := []Notice{{ID: "release", Title: "release", ReleaseNotes: true}}
	p.Open(notices, "release")
	p.tab = TabChanges
	f := testFrame()
	f.Width = 20
	view := p.View(notices, f)
	found := map[string]bool{}
	for _, hit := range p.Foot(notices, f) {
		found[hit.Label] = true
		if row := len(view) - 1 + hit.Y; row < 0 || row >= len(view) {
			t.Fatalf("invalid release footer row %+v", hit)
		}
	}
	for _, label := range []string{"tab", "f", "r", "↵", "x", "esc/q"} {
		if !found[label] {
			t.Fatalf("release footer lost %s", label)
		}
	}
}
