package ui

import (
	"fmt"
	"testing"

	"github.com/YoanWai/agent-manager/internal/keybind"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"strings"
)

func narrowFooterModel(t *testing.T) *Model {
	t.Helper()
	m := buildModel(t)
	if err := m.services.store.CreateGroup("narrow-group", ""); err != nil {
		t.Fatalf("create group: %v", err)
	}
	m.applyCmd(t, m.refreshCmd())
	createSession(t, m, "narrow", t.TempDir(), "")
	return m
}

// An untitled list footer is as tall at a given size whatever the cursor is
// on and whether messages wait, so neither resizes the preview box; the
// transient tiers keep that height too.
func TestNarrowListFooterHoldsItsHeight(t *testing.T) {
	t.Parallel()
	for _, glyph := range []presentation.GlyphMode{presentation.GlyphModeAdvanced, presentation.GlyphModeBasic} {
		for _, size := range [][2]int{{10, 12}, {10, 40}, {12, 40}, {15, 40}, {20, 40}, {28, 40}, {30, 24}} {
			name := fmt.Sprintf("%s/%dx%d", glyph, size[0], size[1])
			m := narrowFooterModel(t)
			m.prefs.glyphMode = glyph
			m.layout.width, m.layout.height = size[0], size[1]
			mode, rows := m.listLegendLayout()
			if mode == legendTitled {
				t.Fatalf("%s: a narrow footer kept its titles", name)
			}
			states := []struct {
				name  string
				apply func()
			}{
				{"session", func() { m.selectSessionRow(t, "narrow") }},
				{"alert", func() { m.notices.feedPending = true }},
				{"pending archive", func() {
					for i := range m.workspace.sessions {
						m.workspace.sessions[i].Status = "finished"
						m.workspace.sessions[i].AfterTurn = store.AfterTurnArchive
					}
					m.rebuildRows()
					m.selectSessionRow(t, "narrow")
				}},
				{"group", func() { m.selectGroupRow(t, "narrow-group") }},
				{"collapsed group", func() {
					m.rail.SetCollapsed("narrow-group", true)
					m.selectGroupRow(t, "narrow-group")
				}},
				{"no alert", func() { m.notices.feedPending = false }},
				{"expanded group", func() { m.rail.SetCollapsed("narrow-group", false) }},
				{"archived view", func() { m.rail.SetArchived(true) }},
				{"active view", func() { m.rail.SetArchived(false) }},
				{"attention filter", func() { m.rail.SetFilteringAttention(true) }},
				{"all statuses", func() { m.rail.SetFilteringAttention(false) }},
				{"hide empty", func() { m.rail.SetHideEmptyGroups(true) }},
				{"show empty", func() { m.rail.SetHideEmptyGroups(false) }},
				{"no selection", func() { resetRailModel(m) }},
			}
			for _, state := range states {
				state.apply()
				if got := lipgloss.Height(m.listFooter()); got != rows {
					t.Errorf("%s/%s: list footer %d rows, want %d", name, state.name, got, rows)
				}
				for _, tier := range []struct {
					name string
					open func()
				}{
					{"resize", func() { m.layout.split.resizeMode = true }},
					{"rename", func() { m.mode = modeRename }},
					{"focus", func() { m.mode = modeFocus }},
				} {
					tier.open()
					if got := lipgloss.Height(m.viewFooter()); got != rows {
						t.Errorf("%s/%s/%s: footer %d rows, want %d", name, state.name, tier.name, got, rows)
					}
					m.layout.split.resizeMode, m.mode = false, modeList
				}
			}
		}
	}
}

// At 12 columns with height to spare, the footer paints a key for help,
// quit, settings, search and new, and each takes a click.
func TestNarrowListFooterPaintsTheAppKeys(t *testing.T) {
	t.Parallel()
	m := narrowFooterModel(t)
	m.layout.width, m.layout.height = 12, 40
	m.selectSessionRow(t, "narrow")
	preparedView(m)
	pressed := map[string]bool{}
	for _, hit := range m.layout.keyHits {
		pressed[hit.key.String()] = true
	}
	for _, key := range []string{"?", "q", "s", "/", "n", " "} {
		if !pressed[key] {
			t.Errorf("no footer hit presses %q:\n%s", key, m.viewFooter())
		}
	}
	if body := m.listBodyHeight(); body < 3 {
		t.Fatalf("body = %d rows", body)
	}
}

// A short terminal keeps its body: the footer takes at most half the rows
// under the header, and never fewer than three.
func TestNarrowFooterKeepsTheBodyAtShortHeights(t *testing.T) {
	t.Parallel()
	m := narrowFooterModel(t)
	m.selectSessionRow(t, "narrow")
	for _, height := range []int{8, 12, 16} {
		for _, width := range []int{10, 15, 30} {
			m.layout.width, m.layout.height = width, height
			room := max(legendMaxRows, (height-m.listChromeRows()-1)/2)
			if got := lipgloss.Height(m.listFooter()); got > room {
				t.Errorf("%dx%d: footer %d rows, room %d", width, height, got, room)
			}
			if body := m.listBodyHeight(); body < 3 {
				t.Errorf("%dx%d: body %d rows", width, height, body)
			}
		}
	}
}

func TestNarrowListFooterKeepsConfiguredActionKeys(t *testing.T) {
	t.Parallel()
	for _, glyph := range []presentation.GlyphMode{presentation.GlyphModeAdvanced, presentation.GlyphModeBasic} {
		for _, width := range []int{12, 20, 28} {
			for _, binding := range []struct{ action, key string }{
				{keybind.Settings, "pgup"}, {keybind.Messages, "pgup"}, {keybind.Help, "up"},
			} {
				t.Run(fmt.Sprintf("%s/%d/%s", glyph, width, binding.action), func(t *testing.T) {
					m := narrowFooterModel(t)
					m.prefs.glyphMode = glyph
					m.layout.width, m.layout.height = width, 60
					if binding.key == "up" {
						m.services.listKeys = m.services.listKeys.With(keybind.Up, keybind.Keys())
					}
					m.services.listKeys = m.services.listKeys.With(binding.action, bindingOf(t, binding.key))
					m.notices.feedPending = binding.action == keybind.Messages
					m.selectSessionRow(t, "narrow")
					preparedView(m)
					found := false
					var entry keyHit
					for _, hit := range m.layout.keyHits {
						if hit.key.String() != binding.key {
							continue
						}
						found = true
						entry = hit
						local := m.listLegend()
						for _, painted := range local.hits {
							if painted.key.String() != binding.key {
								continue
							}
							lines := strings.Split(ansi.Strip(local.text), "\n")
							label := presentation.KeyLabel(glyph, m.services.listKeys.Binding(binding.action).Glyph("/"))
							if !strings.Contains(lines[painted.y], label) {
								t.Fatalf("missing visible key %q: %s", label, local.text)
							}
						}
					}
					if !found {
						t.Fatalf("%s=%s has no mouse target:\n%s", binding.action, binding.key, m.listFooter())
					}
					for _, section := range []legendSection{m.viewLegend()} {
						for _, part := range section.parts(glyph) {
							if strings.Contains(part.text, "navigate") || strings.Contains(part.text, "reorder") {
								if part.clickable() {
									t.Fatalf("navigation became clickable: %s", part.text)
								}
							}
						}
					}
					m = leftClick(m, entry.x0, entry.y)
					want := map[string]mode{keybind.Settings: modeSettings, keybind.Messages: modeNotices, keybind.Help: modeHelp}[binding.action]
					if m.mode != want {
						t.Fatalf("click on %s opened mode %v, want %v", binding.action, m.mode, want)
					}
					m.handleKey(key("esc"))
					msg, ok := pressKey(binding.key)
					if !ok {
						t.Fatalf("invalid test binding %q", binding.key)
					}
					m.handleKey(msg)
					if m.mode != want {
						t.Fatalf("key %s opened mode %v, want %v", binding.key, m.mode, want)
					}
				})
			}
		}
	}
}

func TestListFooterLayoutAcrossViewStates(t *testing.T) {
	t.Parallel()
	m := narrowFooterModel(t)
	m.selectSessionRow(t, "narrow")
	for _, glyph := range []presentation.GlyphMode{presentation.GlyphModeAdvanced, presentation.GlyphModeBasic} {
		m.prefs.glyphMode = glyph
		for width := 10; width <= 100; width++ {
			m.layout.width, m.layout.height = width, 40
			m.rail.SetArchived(false)
			m.rail.SetCollapsed("narrow-group", false)
			m.rail.SetFilteringAttention(false)
			m.rail.SetHideEmptyGroups(false)
			wantMode, wantRows := m.listLegendLayout()
			for _, state := range []struct {
				name  string
				apply func()
			}{
				{"folded", func() { m.rail.SetCollapsed("narrow-group", true) }},
				{"archived", func() { m.rail.SetArchived(true) }},
				{"active", func() { m.rail.SetArchived(false) }},
				{"attention", func() { m.rail.SetFilteringAttention(true) }},
				{"empty hidden", func() { m.rail.SetHideEmptyGroups(true) }},
			} {
				state.apply()
				gotMode, gotRows := m.listLegendLayout()
				if gotMode != wantMode || gotRows != wantRows {
					t.Fatalf("%s width=%d %s layout=(%d,%d), want (%d,%d)", glyph, width, state.name, gotMode, gotRows, wantMode, wantRows)
				}
			}
		}
	}
}
