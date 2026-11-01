package frame

import (
	"fmt"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

var narrowGlyphModes = []presentation.GlyphMode{presentation.GlyphModeAdvanced, presentation.GlyphModeBasic}

// The tiers below take the shapes rail.RowLegend, ViewLegend, RemoteLegend
// and the transient footers give under the default keys.

func narrowSessionTier() legendSection {
	return legendSection{
		Title: "Session",
		Leads: [][2]string{{"space", "quick prompt mode"}, {"ctrl+r", "review mode"}},
		Hint:  `tell your agent "set review mode"`,
		Pairs: [][2]string{
			{"↵", "focus / fold"}, {"A", "attach"}, {"click", "focus"}, {"→", "focus"}, {".", "mark idle"}, {"c", "cancel archive"},
			{"y", "copy"}, {"f", "fork"}, {"o", "editor"}, {"r", "rename"}, {"m", "move"}, {"x/X", "kill / all"},
			{"v/V", "revive / all"}, {"R", "restart"}, {"a", "archive"}, {"d", "delete"},
		},
	}
}

func narrowShellTier() legendSection {
	return legendSection{Title: "Shell", Pairs: [][2]string{
		{"↵", "attach / fold"}, {"A", "focus"}, {"double click", "focus"}, {"c", "cancel kill"}, {"o", "editor"}, {"r", "rename"},
		{"m", "move"}, {"x/X", "kill / all"}, {"v/V", "revive / all"}, {"R", "restart"}, {"u", "restore"}, {"d", "delete"},
	}}
}

func narrowBrowserTier() legendSection {
	tier := narrowShellTier()
	tier.Title = "Browser"
	return tier
}

func narrowGroupTier() legendSection {
	return legendSection{
		Title: "Group",
		Leads: [][2]string{{"space", "quick prompt mode"}},
		Pairs: [][2]string{
			{"↵", "unfold"}, {"double click", "unfold"}, {"←/→", "close / open"}, {"o", "editor"}, {"r", "rename"}, {"m", "move"},
			{"x/X", "kill / all"}, {"v/V", "revive / all"}, {"a", "archive"}, {"d", "delete"},
		},
	}
}

func narrowConnectionTier() legendSection {
	return legendSection{
		Title: "SSH connection",
		Leads: [][2]string{{"space", "quick prompt mode"}},
		Pairs: [][2]string{{"↵", "unfold"}, {"t", "terminal"}, {"g", "group"}, {"r", "edit"}, {"d", "remove"}},
	}
}

func narrowRemoteShellTier() legendSection {
	return legendSection{Title: "Shell on build-host", Pairs: [][2]string{
		{"↵/A", "attach over SSH"}, {"x", "close"}, {"v", "revive"}, {"a", "archive"},
	}}
}

func narrowViewTier(alert string) legendSection {
	view := legendSection{Title: "View", Quiet: true, Pairs: [][2]string{
		{"↑/k ↓/j", "navigate"}, {"n", "new"}, {"t", "terminal"}, {"b", "browser"}, {"g", "group"}, {"/", "search"}, {"T", "archived"},
		{"w", "attention"}, {"e", "hide empty"}, {"?", "keys"}, {"q", "quit"}, {"C", "connection"},
		{"⇧↑/K/⇧↓/J", "reorder"}, {"F", "fold all"}, {"|", "resize"}, {"s", "settings"},
	}}
	if alert != "" {
		view.Alerts = [][2]string{{"M", alert}}
	}
	return view
}

// narrowFooters is every list footer the tiers make: each row tier, or none,
// over the View tier with no alert, the unread alert and the pending one.
func narrowFooters() [][]legendSection {
	var footers [][]legendSection
	for _, row := range []legendSection{narrowSessionTier(), narrowShellTier(), narrowBrowserTier(), narrowGroupTier(), narrowConnectionTier(), narrowRemoteShellTier(), {}} {
		for _, alert := range []string{"", "unread", "checking messages…"} {
			footers = append(footers, []legendSection{row, narrowViewTier(alert)})
		}
	}
	return footers
}

func narrowTransientTiers() []legendSection {
	return []legendSection{
		{Title: "Focused", Pairs: [][2]string{
			{"ctrl+q / ctrl+\\ / click the list / mouse back", "back"}, {"←", "prompt start: back"}, {"ctrl+r", "review"},
			{"f3", "editor"}, {"f4", "saved prompts"}, {"pgup/pgdn", "scroll"}, {"click / alt+drag", "agent UI"},
		}},
		{Title: "Resize", Pairs: [][2]string{{"←→", "nudge"}, {"drag", "divider"}, {"| / release", "commit"}, {"esc", "cancel"}}},
		{Title: "Rename", Pairs: [][2]string{{"tab", "tool: claude"}, {"↵", "save"}, {"esc", "cancel"}}},
		{Title: "Reorder", Pairs: [][2]string{{"drag / ↑↓ / wheel", "move"}, {"↵ / release", "drop"}, {"esc", "put back"}}},
		{Title: "Move", Pairs: [][2]string{{"release", "move to backend"}, {"esc", "put back"}}},
	}
}

func narrowReviewSections() []legendSection {
	return []legendSection{
		{Title: "Review", Pairs: [][2]string{
			{"c", "comment"}, {"d", "remove/handle"}, {"C", "send"}, {"space", "reviewed"},
			{"s", "scope: session"}, {"r", "repo"}, {"b", "branch"}, {"B", "target"},
		}},
		{Title: "Move", Quiet: true, Pairs: [][2]string{
			{"↑↓/jk", "scroll line"}, {"ctrl+d/ctrl+u", "half page"}, {"pgup/pgdn", "page"},
			{"g/G", "top/bottom"}, {"tab/J K/shift+tab", "file"}, {"n/N", "change"},
			{"u", "layout"}, {"f", "code only"}, {"o/f3", "open file"}, {"?", "keys"},
			{"esc/q", "close"}, {"ctrl+c", "quit"},
		}},
	}
}

// narrowRoom is the rows a list footer may take under a 1-row header and
// its rule: half of what is left below them, and never under 3.
func narrowRoom(height int) int {
	return max(legendMaxRows, (height-3)/2)
}

func narrowWidths() []int {
	widths := []int{200, 120, 80}
	for width := 60; width >= 10; width-- {
		widths = append(widths, width)
	}
	return widths
}

// clickableParts is every part of sections a click can press, in order.
func clickableParts(sections []legendSection, glyph presentation.GlyphMode) []legendPart {
	var parts []legendPart
	for _, section := range sections {
		for _, part := range section.Parts(LiveCap(glyph)) {
			if _, ok := legendKey(part.Label); ok {
				parts = append(parts, part)
			}
		}
	}
	return parts
}

// assertLegendFits checks that a footer stays in its box: rows rows of
// width cells, and every hit on painted cells of a row it has.
func assertLegendFits(t *testing.T, name string, bar legend, width, rows int) {
	t.Helper()
	lines := strings.Split(ansi.Strip(bar.Text), "\n")
	if len(lines) > rows {
		t.Errorf("%s: %d rows, budget %d:\n%s", name, len(lines), rows, ansi.Strip(bar.Text))
	}
	for _, line := range lines {
		if ansi.StringWidth(line) > width {
			t.Errorf("%s: row %q is wider than %d", name, line, width)
		}
	}
	for _, hit := range bar.Hits {
		if hit.Y >= len(lines) || hit.X0 >= hit.X1 || hit.X1 > width || hit.X1 > ansi.StringWidth(lines[hit.Y]) {
			t.Errorf("%s: hit %+v is off the painted cells:\n%s", name, hit, ansi.Strip(bar.Text))
		}
	}
}

// assertEveryKeyPainted checks that an untitled footer painted each part a
// click can press once, in order, whole, and that its hit presses the key
// it shows: the key and its words when worded, the first key alone when
// bare. Padding a lead or alert carries is trimmed.
func assertEveryKeyPainted(t *testing.T, name string, bar legend, sections []legendSection, glyph presentation.GlyphMode, mode legendMode) {
	t.Helper()
	lines := strings.Split(ansi.Strip(bar.Text), "\n")
	want := clickableParts(sections, glyph)
	if len(bar.Hits) != len(want) {
		t.Errorf("%s: %d hits for %d keys:\n%s", name, len(bar.Hits), len(want), ansi.Strip(bar.Text))
		return
	}
	for i, part := range want {
		hit := bar.Hits[i]
		text := strings.TrimSpace(ansi.Strip(part.Text))
		if mode == legendBare {
			first, _, _ := strings.Cut(part.Label, " / ")
			text = presentation.KeyLabel(glyph, strings.TrimSpace(first))
		}
		if got := strings.TrimSpace(ansi.Cut(lines[hit.Y], hit.X0, hit.X1)); got != text {
			t.Errorf("%s: hit %d paints %q, want %q", name, i, got, text)
		}
		if key, _ := legendKey(part.Label); hit.Key.String() != key.String() {
			t.Errorf("%s: hit %d presses %q, want %q", name, i, hit.Key.String(), key.String())
		}
	}
}

// paintedMode is legendBare when an untitled footer painted its first key
// without its words, legendWorded otherwise.
func paintedMode(bar legend, sections []legendSection, glyph presentation.GlyphMode) legendMode {
	parts := clickableParts(sections, glyph)
	if len(parts) == 0 || len(bar.Hits) == 0 {
		return legendWorded
	}
	hit := bar.Hits[0]
	line := strings.Split(ansi.Strip(bar.Text), "\n")[hit.Y]
	if strings.TrimSpace(ansi.Cut(line, hit.X0, hit.X1)) == strings.TrimSpace(ansi.Strip(parts[0].Text)) {
		return legendWorded
	}
	return legendBare
}

// With room to spare, a narrow footer drops its titles and gestures and
// paints every key it has, whole, with a hit that presses it: ? and q
// included, whatever the cursor is on and whether messages wait.
func TestNarrowLegendPaintsEveryKeyWhole(t *testing.T) {
	refs := narrowFooters()
	for _, glyph := range narrowGlyphModes {
		for width := 10; width <= 60; width++ {
			mode, rows := legendLayout(refs, width, narrowRoom(40), glyph)
			if mode == legendTitled {
				continue
			}
			for i, footer := range refs {
				name := fmt.Sprintf("%s/%d/footer %d", glyph, width, i)
				bar := paintUntitledLegend(footer, width, glyph, mode, rows)
				assertLegendFits(t, name, bar, width, rows)
				assertEveryKeyPainted(t, name, bar, footer, glyph, mode)
			}
		}
	}
}

// The terminals the report came from: 10 to 15 columns with ample height
// paint the help and quit keys, which the titled footer cut.
func TestNarrowLegendPaintsHelpAt10To15Columns(t *testing.T) {
	refs := narrowFooters()
	for _, glyph := range narrowGlyphModes {
		for width := 10; width <= 15; width++ {
			mode, rows := legendLayout(refs, width, narrowRoom(40), glyph)
			if mode != legendBare {
				t.Fatalf("%s/%d: mode %d, want bare", glyph, width, mode)
			}
			// The session tier over the View tier with the unread alert.
			bar := paintUntitledLegend(refs[1], width, glyph, mode, rows)
			pressed := map[string]bool{}
			for _, hit := range bar.Hits {
				pressed[hit.Key.String()] = true
			}
			for _, key := range []string{"?", "q", "s", "/", "n", "M", " "} {
				if !pressed[key] {
					t.Errorf("%s/%d: no hit presses %q:\n%s", glyph, width, key, ansi.Strip(bar.Text))
				}
			}
			text := ansi.Strip(bar.Text)
			for _, gone := range []string{"Session", "View", "click", "navigate", "set review mode", "↑"} {
				if strings.Contains(text, gone) {
					t.Errorf("%s/%d: an untitled footer paints %q:\n%s", glyph, width, gone, text)
				}
			}
		}
	}
}

// As the terminal narrows at a fixed height the footer only steps down,
// titled, then worded, then bare, and normal widths keep their titles.
func TestNarrowLegendStepsDownOnce(t *testing.T) {
	refs := narrowFooters()
	for _, glyph := range narrowGlyphModes {
		for _, height := range []int{8, 12, 16, 24, 40} {
			step := legendTitled
			for _, width := range narrowWidths() {
				mode, _ := legendLayout(refs, width, narrowRoom(height), glyph)
				if mode < step {
					t.Fatalf("%s/%dx%d: mode %d came back after %d", glyph, width, height, mode, step)
				}
				step = mode
				if width >= 80 && mode != legendTitled {
					t.Errorf("%s/%dx%d: a normal width lost its titles", glyph, width, height)
				}
				if width <= 15 && mode != legendBare {
					t.Errorf("%s/%dx%d: mode %d, want bare", glyph, width, height, mode)
				}
			}
		}
	}
}

// The mode and the rows come from every footer at once, so the cursor
// moving or an alert arriving never changes the footer's height; each
// footer fits the rows, which stay inside half the body.
func TestNarrowLegendHoldsItsRowsForEveryFooter(t *testing.T) {
	refs := narrowFooters()
	for _, glyph := range narrowGlyphModes {
		for _, height := range []int{8, 12, 16, 24, 40} {
			for _, width := range narrowWidths() {
				mode, rows := legendLayout(refs, width, narrowRoom(height), glyph)
				if mode == legendTitled {
					continue
				}
				if rows > narrowRoom(height) || rows < 2 {
					t.Fatalf("%s/%dx%d: %d rows, room %d", glyph, width, height, rows, narrowRoom(height))
				}
				for i, footer := range refs {
					name := fmt.Sprintf("%s/%dx%d/footer %d", glyph, width, height, i)
					if need := untitledRows(footer, width, LiveCap(glyph), mode); mode == legendWorded && need > rows {
						t.Errorf("%s: needs %d rows, budget %d", name, need, rows)
					}
					bar := paintUntitledLegend(footer, width, glyph, mode, rows)
					assertLegendFits(t, name, bar, width, rows)
					keys := clickableParts(footer, glyph)
					if len(bar.Hits) < len(keys) && !strings.Contains(ansi.Strip(bar.Text), "…") {
						t.Errorf("%s: %d of %d keys painted and no cut marked:\n%s", name, len(bar.Hits), len(keys), ansi.Strip(bar.Text))
					}
					// The View tier keeps at least a row of its own.
					lines := strings.Split(ansi.Strip(bar.Text), "\n")
					if last := bar.Hits[len(bar.Hits)-1]; last.Y != len(lines)-1 {
						t.Errorf("%s: the last row holds no Key:\n%s", name, ansi.Strip(bar.Text))
					}
				}
			}
		}
	}
}

// Wide footers are the titled footer as it was.
func TestTitledLegendKeepsItsLayout(t *testing.T) {
	bar := paintLegend([]legendSection{{Title: "Keys", Pairs: [][2]string{{"esc", "cancel"}}}}, 40, presentation.GlyphModeAdvanced)
	if got := ansi.Strip(bar.Text); got != "   Keys      esc cancel" {
		t.Fatalf("titled legend = %q", got)
	}
	if len(bar.Hits) != 1 || bar.Hits[0].X0 != 13 || bar.Hits[0].X1 != 23 || bar.Hits[0].Key.String() != "esc" {
		t.Fatalf("titled hits = %+v", bar.Hits)
	}
}

// The transient tiers narrow with the list footer, inside its rows, and keep
// the keys they press: back presses the detach key, commit the resize key
// and drop enter. A tier whose words would not fit those rows goes bare, and
// none paints words the list footer dropped.
func TestNarrowTransientTiersShareTheListRows(t *testing.T) {
	refs := narrowFooters()
	for _, glyph := range narrowGlyphModes {
		for _, height := range []int{12, 40} {
			for width := 10; width <= 60; width++ {
				mode, rows := legendLayout(refs, width, narrowRoom(height), glyph)
				if mode == legendTitled {
					continue
				}
				for _, tier := range narrowTransientTiers() {
					name := fmt.Sprintf("%s/%dx%d/%s", glyph, width, height, tier.Title)
					bar := paintNarrowLegend([]legendSection{tier}, width, glyph, mode, rows)
					assertLegendFits(t, name, bar, width, rows)
					painted := paintedMode(bar, []legendSection{tier}, glyph)
					if painted < mode {
						t.Errorf("%s: painted mode %d, the list footer's is %d", name, painted, mode)
					}
					assertEveryKeyPainted(t, name, bar, []legendSection{tier}, glyph, painted)
					if strings.Contains(ansi.Strip(bar.Text), tier.Title) {
						t.Errorf("%s: an untitled tier paints its title", name)
					}
				}
			}
		}
	}
	bar := paintNarrowLegend([]legendSection{narrowTransientTiers()[0]}, 12, presentation.GlyphModeAdvanced, legendBare, 3)
	if len(bar.Hits) == 0 || bar.Hits[0].Key.String() != "ctrl+q" || !strings.Contains(ansi.Strip(bar.Text), "ctrl+q") {
		t.Fatalf("bare back = %+v:\n%s", bar.Hits, ansi.Strip(bar.Text))
	}
}

// The quick prompt keys and the Review footer step down the same way,
// inside their own three rows.
func TestNarrowQuickAndReviewFootersKeepThreeRows(t *testing.T) {
	quick := []legendSection{{Title: "Quick prompt mode", Pairs: [][2]string{
		{"↵", "send"}, {"↑↓", "target or caret"}, {"tab", "tool"}, {"shift+tab", "previous tool"},
		{"ctrl+o", "saved prompts"}, {"ctrl+t", "worktree"}, {"esc", "close"},
	}}}
	for _, glyph := range narrowGlyphModes {
		for _, sections := range [][]legendSection{quick, narrowReviewSections()} {
			step := legendTitled
			for _, width := range narrowWidths() {
				name := fmt.Sprintf("%s/%s/%d", glyph, sections[0].Title, width)
				mode, rows := legendLayout([][]legendSection{sections}, width, legendMaxRows, glyph)
				if mode < step {
					t.Fatalf("%s: mode %d came back after %d", name, mode, step)
				}
				step = mode
				if width >= 80 && mode != legendTitled {
					t.Errorf("%s: a normal width lost its titles", name)
				}
				bar := paintLegend(sections, width, glyph)
				assertLegendFits(t, name, bar, width, legendMaxRows)
				if mode == legendWorded {
					assertEveryKeyPainted(t, name, bar, sections, glyph, mode)
				}
				if mode != legendTitled && rows > legendMaxRows {
					t.Errorf("%s: %d rows past the budget", name, rows)
				}
			}
		}
	}
}

// A bare key presses what its worded part pressed.
func TestBareKeysPressTheirPartsKey(t *testing.T) {
	sections := append(narrowTransientTiers(), narrowSessionTier(), narrowShellTier(), narrowBrowserTier(), narrowGroupTier(), narrowConnectionTier(), narrowRemoteShellTier(), narrowViewTier("unread"))
	for _, glyph := range narrowGlyphModes {
		for _, section := range sections {
			for _, part := range section.Parts(LiveCap(glyph)) {
				worded, ok := legendKey(part.Label)
				if !ok {
					continue
				}
				if bare, ok := legendKey(bareKey(part.Label)); !ok || bare.String() != worded.String() {
					t.Errorf("%s/%s: bare %q presses %q, worded %q", glyph, section.Title, bareKey(part.Label), bare.String(), worded.String())
				}
			}
		}
	}
}
