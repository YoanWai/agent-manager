package notices

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func inIsraelTime(t *testing.T) {
	t.Helper()
	zone, err := time.LoadLocation("Asia/Jerusalem")
	if err != nil {
		t.Fatal(err)
	}
	previous := displayZone
	displayZone = zone
	t.Cleanup(func() { displayZone = previous })
}

func published(t *testing.T, stamp string) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	return at
}

// tabsRelease is v0.46.0 as its notes read: labelled highlights, 24 changes
// of every kind with scopes, and ten people thanked across the three groups.
func tabsRelease(t *testing.T) Release {
	release := Release{
		Version:     "v0.46.0",
		URL:         RepoURL + "/releases/tag/v0.46.0",
		PublishedAt: published(t, "2026-10-30T15:55:28Z"),
		Headline:    "Every release, in three tabs",
		Summary:     "The messages panel is an inbox now: releases, news, heads-ups and tips on the left, the one you pick on the right. A release splits into Overview, Changes and Thanks, so nothing sits under anything else.",
		Highlights: []Highlight{
			{Label: "Inbox", Text: "Messages sit in groups on the left, the selected one on the right."},
			{Label: "Every merged PR", Text: "Tests, CI and docs changes show under the hood instead of vanishing."},
			{Label: "Thanks by role", Text: "People who built, found or started something are thanked in their own group."},
		},
	}
	scopes := []string{"UI", "Notices", "Feed", "Update", "Tmux", "E2E"}
	add := func(kind string, count int, words string) {
		for i := range count {
			release.Changes = append(release.Changes, Change{Kind: kind, Scope: scopes[(len(release.Changes)+i)%len(scopes)], Text: fmt.Sprintf("%s %02d", words, i+1)})
		}
	}
	add(KindFeature, 6, "Show the release in tabs")
	add(KindFix, 10, "Keep the bottom edge on screen")
	add(KindOther, 7, "Cover the inbox in tests")
	release.Changes = append(release.Changes, Change{Kind: KindOther, Scope: "E2E", Text: "Run the panel scenario on every platform", Author: "@ribeirojose"})
	release.TotalChanges = len(release.Changes)
	release.Thanks = []Thanks{
		{Handle: "@ribeirojose", Group: ThanksBuilt, Text: "built the quit router", Refs: []string{"#861"}},
		{Handle: "@subbeh", Group: ThanksBuilt, Text: "built environment profiles", Refs: []string{"#834"}},
		{Handle: "@cvas22", Group: ThanksBuilt, Text: "walked the caller tree", Refs: []string{"#786"}},
		{Handle: "@ipl31", Group: ThanksFound, Text: "found the stacked layout overflowing", Refs: []string{"#929", "#930"}},
		{Handle: "@tonyperkins", Group: ThanksFound, Text: "reported the lost scrollback", Refs: []string{"#778"}},
		{Handle: "@fruch", Group: ThanksFound, Text: "reported the rename moving the worktree", Refs: []string{"#418"}},
		{Handle: "@drakeo338", Group: ThanksFound, Text: "caught the reply line", Refs: []string{"#589"}},
		{Handle: "@pandysp", Group: ThanksStarted, Text: "opened the preview toggle", Refs: []string{"#357"}},
		{Handle: "@dolutech", Group: ThanksStarted, Text: "asked for the picker", Refs: []string{"#388"}},
		{Handle: "@Qusavin", Group: ThanksStarted, Text: "started coordination", Refs: []string{"#558"}},
	}
	return release
}

func updatedNotice(t *testing.T, releases ...Release) Notice {
	src := Sources{
		Store:           true,
		Update:          Update{Version: "v0.46.0", Checked: true, Installed: ReleaseRange{Releases: releases, Complete: true}},
		WhatsNewVersion: "v0.46.0",
		WhatsNewFrom:    "v0.45.0",
	}
	return NewPanel(map[string]bool{}, map[string]bool{}).Active(src)[0]
}

func availableNotice(t *testing.T, releases ...Release) Notice {
	src := Sources{
		Store:  true,
		Update: Update{Version: "v0.45.0", Latest: "v0.46.0", URL: RepoURL + "/releases/tag/v0.46.0", Available: ReleaseRange{Releases: releases, Complete: true}},
	}
	return NewPanel(map[string]bool{}, map[string]bool{}).Active(src)[0]
}

const paneTestWidth = 120

func paneRows(n Notice, tab Tab, filter string, width int, t Theme, mode presentation.GlyphMode) []string {
	rows, _ := releasePane(n, tab, filter, width, t, mode)
	return rows
}

func paneText(n Notice, tab Tab, filter string, width int) []string {
	return plainRows(paneRows(n, tab, filter, width, testTheme(), presentation.DefaultGlyphMode))
}

func TestReleaseHeaderShowsVersionDateAndOrigin(t *testing.T) {
	inIsraelTime(t)
	rows := paneText(updatedNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth)
	first := rows[0]
	for _, want := range []string{"v0.46.0", "Oct 30 · you were on 0.45.0", "notes on GitHub"} {
		if !strings.Contains(first, want) {
			t.Fatalf("header %q misses %q", first, want)
		}
	}
	if !strings.HasSuffix(first, "notes on GitHub") || lipgloss.Width(first) != paneTestWidth {
		t.Fatalf("the link sits at the right edge: %q", first)
	}
	if rows[1] != "" || rows[2] != "Every release, in three tabs" || rows[3] != "" {
		t.Fatalf("the headline follows the version after a blank row:\n%s", strings.Join(rows, "\n"))
	}
	available := paneText(availableNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth)
	if !strings.Contains(available[0], "out now · you're on 0.45.0") || strings.Contains(available[0], "notes on GitHub") {
		t.Fatalf("available header %q, want the notes link moved into the update bar", available[0])
	}
}

func TestAvailableReleaseShowsTheUpdateCallToAction(t *testing.T) {
	available := strings.Join(paneText(availableNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth), "\n")
	if at := strings.Index(available, "u    Update to 0.46.0 and restart"); at < 0 || !strings.Contains(available[at:], "↵  notes on GitHub") {
		t.Fatalf("available release lacks the call to action:\n%s", available)
	}
	updated := strings.Join(paneText(updatedNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth), "\n")
	if strings.Contains(updated, "Update to") {
		t.Fatalf("an installed release offers an update:\n%s", updated)
	}
}

func TestTabStripCountsChangesAndPeople(t *testing.T) {
	rows := paneText(updatedNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth)
	at := rowIndex(rows, "Overview")
	if at < 0 {
		t.Fatalf("no tab strip:\n%s", strings.Join(rows, "\n"))
	}
	strip := rows[at]
	if !(strings.Index(strip, "Overview") < strings.Index(strip, "Changes 24") && strings.Index(strip, "Changes 24") < strings.Index(strip, "Thanks 10")) {
		t.Fatalf("tab strip %q", strip)
	}
}

func TestTabCountsAreMutedAndTheActiveTabUnderlined(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	raw := paneRows(updatedNotice(t, tabsRelease(t)), TabChanges, "", paneTestWidth, theme, presentation.DefaultGlyphMode)
	joined := strings.Join(raw, "\n")
	for _, want := range []string{theme.Muted.Render("24"), theme.Muted.Render("10"), headingStyle(theme).Render("Changes")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("tab strip misses %q", ansi.Strip(want))
		}
	}
	plain := plainRows(raw)
	strip := rowIndex(plain, "Overview")
	under := []rune(plain[strip+1])
	start := len([]rune(plain[strip][:strings.Index(plain[strip], "Changes")]))
	if string(under[start]) != "━" || string(under[start-3]) != "─" || string(under[len(under)-1]) != "─" || len(under) != paneTestWidth {
		t.Fatalf("the accent underline sits under the active tab on a rule across the pane:\n%s\n%s", plain[strip], plain[strip+1])
	}
	if strings.Contains(joined, theme.Subtle.Render("24")) {
		t.Fatal("a tab count uses the subtle token")
	}
}

func TestOverviewAlignsHighlightLabelsWhenEveryDetailFits(t *testing.T) {
	rows := paneText(updatedNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth)
	column := func(label, text string) int {
		at := rowIndex(rows, label)
		if at < 0 {
			t.Fatalf("no %q row:\n%s", label, strings.Join(rows, "\n"))
		}
		return strings.Index(rows[at], text)
	}
	inbox, merged := column("Inbox", "Messages sit"), column("Every merged PR", "Tests, CI")
	if inbox != merged || inbox != len("◆ Every merged PR")+2 {
		t.Fatalf("highlight details start at %d and %d, want two cells after the longest label:\n%s", inbox, merged, strings.Join(rows, "\n"))
	}
	if at := rowIndex(rows, "Inbox"); rows[at+1] == "" {
		t.Fatalf("a table of one-row highlights takes no blank rows:\n%s", strings.Join(rows, "\n"))
	}
	if available := paneText(availableNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth); rowIndex(available, "WHAT YOU GET") < 0 {
		t.Fatalf("an available release names what it gives:\n%s", strings.Join(available, "\n"))
	}
}

func TestOverviewStacksHighlightsWhenADetailWouldWrap(t *testing.T) {
	release := tabsRelease(t)
	release.Highlights = append(release.Highlights, Highlight{Text: "An older `highlight` without a label that runs long enough to wrap onto a second row under its bullet"})
	rows := paneText(updatedNotice(t, release), TabOverview, "", 60)
	if rowIndex(rows, "WHAT'S NEW") < 0 {
		t.Fatalf("no WHAT'S NEW label:\n%s", strings.Join(rows, "\n"))
	}
	inbox := rowIndex(rows, "◆ Inbox")
	if inbox < 0 || strings.TrimSpace(rows[inbox]) != "◆ Inbox" || !strings.HasPrefix(rows[inbox+1], "  Messages sit") {
		t.Fatalf("a label takes its own row with the detail under it:\n%s", strings.Join(rows, "\n"))
	}
	merged := rowIndex(rows, "◆ Every merged PR")
	if merged < 2 || rows[merged-1] != "" {
		t.Fatalf("a blank row separates highlights:\n%s", strings.Join(rows, "\n"))
	}
	older := rowIndex(rows, "◆ An older highlight")
	if older < 0 || !strings.HasPrefix(rows[older+1], "  ") || rows[older+1][2] == ' ' {
		t.Fatalf("an unlabelled highlight wraps under its bullet:\n%s", strings.Join(rows, "\n"))
	}
}

func TestOverviewEndsWithCountChips(t *testing.T) {
	rows := paneText(updatedNotice(t, tabsRelease(t)), TabOverview, "", paneTestWidth)
	last := rows[len(rows)-1]
	for _, want := range []string{"✚ 6 features", "● 10 fixes", "◌ 8 under the hood", "♥ 10 people"} {
		if !strings.Contains(last, want) {
			t.Fatalf("last row %q misses %q", last, want)
		}
	}
	for _, row := range rows {
		if lipgloss.Width(row) > paneTestWidth {
			t.Fatalf("row overflows the pane: %q", row)
		}
	}
	if summary := rowIndex(rows, "The messages panel is an inbox"); summary < 0 || lipgloss.Width(rows[summary]) > proseWidth {
		t.Fatalf("the summary wraps at the prose measure:\n%s", strings.Join(rows, "\n"))
	}
}

func changeRows(rows []string, release Release) []string {
	var found []string
	for _, row := range rows {
		for _, change := range release.Changes {
			if strings.Contains(row, change.Text) {
				found = append(found, row)
				break
			}
		}
	}
	return found
}

func TestChangesListEveryChangeWithScopeColumn(t *testing.T) {
	release := tabsRelease(t)
	rows := paneText(updatedNotice(t, release), TabChanges, "", paneTestWidth)
	found := changeRows(rows, release)
	if len(found) != 24 {
		t.Fatalf("%d change rows, want 24:\n%s", len(found), strings.Join(rows, "\n"))
	}
	scopeAt, textAt := -1, -1
	for i, row := range found {
		change := release.Changes[i]
		scope, text := strings.Index(row, change.Scope), strings.Index(row, change.Text)
		if scopeAt < 0 {
			scopeAt, textAt = scope, text
		}
		if scope != scopeAt || text != textAt {
			t.Fatalf("row %d %q puts the scope at %d and text at %d, want %d and %d", i, row, scope, text, scopeAt, textAt)
		}
	}
	e2e := found[len(found)-1]
	if !strings.HasSuffix(e2e, "@ribeirojose") || lipgloss.Width(e2e) != paneTestWidth {
		t.Fatalf("the outside author sits at the right edge: %q", e2e)
	}
	if rowIndex(rows, "All") < 0 || rowIndex(rows, "✚ 6") < 0 || rowIndex(rows, "● 10") < 0 || rowIndex(rows, "◌ 8") < 0 {
		t.Fatalf("no filter chips:\n%s", strings.Join(rows, "\n"))
	}
}

func TestUnderTheHoodRowsAreMuted(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	joined := strings.Join(paneRows(updatedNotice(t, tabsRelease(t)), TabChanges, "", paneTestWidth, theme, presentation.DefaultGlyphMode), "\n")
	for _, want := range []string{theme.Muted.Render("Cover the inbox in tests 01"), theme.Value.Render("Keep the bottom edge on screen 01"), theme.Value.Render("Show the release in tabs 01")} {
		if !strings.Contains(joined, want) {
			t.Fatalf("change row misses %q in its style", ansi.Strip(want))
		}
	}
}

func TestNarrowChangeSetsTheAuthorOnItsOwnRow(t *testing.T) {
	release := tabsRelease(t)
	rows := paneText(updatedNotice(t, release), TabChanges, "", 40)
	for _, row := range rows {
		if lipgloss.Width(row) > 40 {
			t.Fatalf("row overflows the pane: %q", row)
		}
	}
	at := rowIndex(rows, "@ribeirojose")
	if at < 0 || !strings.HasSuffix(rows[at], "@ribeirojose") || lipgloss.Width(rows[at]) != 40 {
		t.Fatalf("the author sits at the right edge:\n%s", strings.Join(rows, "\n"))
	}
}

func TestFilterCyclesAllFeaturesFixesUnderTheHood(t *testing.T) {
	release := tabsRelease(t)
	notices := []Notice{updatedNotice(t, release), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	p.Key(key("f"), notices, testFrame())
	if p.filter != "" {
		t.Fatalf("f on Overview set the filter to %q", p.filter)
	}
	p.Key(key("tab"), notices, testFrame())
	var seen []string
	for range 4 {
		p.Key(key("f"), notices, testFrame())
		seen = append(seen, p.filter)
	}
	if want := []string{KindFeature, KindFix, KindOther, ""}; !slices.Equal(seen, want) {
		t.Fatalf("f cycles %q, want %q", seen, want)
	}
	rows := paneText(notices[0], TabChanges, KindFix, paneTestWidth)
	found := changeRows(rows, release)
	if len(found) != 10 {
		t.Fatalf("fixes only shows %d rows, want 10:\n%s", len(found), strings.Join(rows, "\n"))
	}
	for _, row := range found {
		if !strings.Contains(row, "Keep the bottom edge") {
			t.Fatalf("a non-fix row under the fixes filter: %q", row)
		}
	}
}

func TestThanksGroupsInOrderWithRefsRightAligned(t *testing.T) {
	rows := paneText(updatedNotice(t, tabsRelease(t)), TabThanks, "", paneTestWidth)
	joined := strings.Join(rows, "\n")
	built, found, started := strings.Index(joined, "BUILT IT"), strings.Index(joined, "FOUND IT"), strings.Index(joined, "STARTED SOMETHING")
	if built < 0 || !(built < found && found < started) {
		t.Fatalf("groups out of order:\n%s", joined)
	}
	if !strings.Contains(joined, "10 people made this release since 0.45.0. Thank you.") {
		t.Fatalf("no thank-you line:\n%s", joined)
	}
	at := rowIndex(rows, "#929 #930")
	if at < 0 || !strings.HasSuffix(rows[at], "#929 #930") || lipgloss.Width(rows[at]) != paneTestWidth {
		t.Fatalf("refs sit at the right edge:\n%s", joined)
	}
	handleAt := strings.Index(rows[rowIndex(rows, "@subbeh")], "built environment")
	if other := strings.Index(rows[rowIndex(rows, "@tonyperkins")], "reported the lost"); other != handleAt {
		t.Fatalf("what they did starts at %d and %d:\n%s", handleAt, other, joined)
	}
}

func TestEmptyTabsSayWhatIsMissing(t *testing.T) {
	bare := Release{Version: "v0.46.0", Headline: "A quiet release"}
	n := updatedNotice(t, bare)
	if changes := strings.Join(paneText(n, TabChanges, "", paneTestWidth), "\n"); !strings.Contains(changes, "No summarized changes.") {
		t.Fatalf("empty Changes tab:\n%s", changes)
	}
	if thanks := strings.Join(paneText(n, TabThanks, "", paneTestWidth), "\n"); !strings.Contains(thanks, "No thank-you lines.") {
		t.Fatalf("empty Thanks tab:\n%s", thanks)
	}
}

func TestReleaseWithoutACatalogSaysWhy(t *testing.T) {
	overview := strings.Join(paneText(availableNotice(t), TabOverview, "", paneTestWidth), "\n")
	if !strings.Contains(overview, "The generated change summary is not available yet; r refreshes it now.") {
		t.Fatalf("an empty range says why:\n%s", overview)
	}
}

func TestRangeListsEveryReleaseOnChangesAndMergesThanks(t *testing.T) {
	inIsraelTime(t)
	older := Release{
		Version:      "v0.45.0",
		Headline:     "Settings own the keys",
		Highlights:   []Highlight{{Text: "Keys and the editor live in Settings"}},
		Changes:      []Change{{Kind: KindFeature, Scope: "Settings", Text: "Move the keys into Settings"}, {Kind: KindFix, Scope: "UI", Text: "Keep the picker open"}},
		TotalChanges: 2,
		Thanks:       []Thanks{{Handle: "@ipl31", Group: ThanksBuilt, Text: "built the settings import", Refs: []string{"#688"}}},
	}
	newest := tabsRelease(t)
	n := updatedNotice(t, older, newest)
	header := paneText(n, TabOverview, "", paneTestWidth)[0]
	if !strings.Contains(header, "v0.46.0") || !strings.Contains(header, "2 releases · you were on 0.45.0") {
		t.Fatalf("range header %q", header)
	}
	changes := paneText(n, TabChanges, "", paneTestWidth)
	newer, old := rowIndex(changes, "v0.46.0"), rowIndex(changes, "v0.45.0")
	if newer < 0 || old < newer || rowIndex(changes, "Move the keys into Settings") < old {
		t.Fatalf("Changes lists each release under its heading, newest first:\n%s", strings.Join(changes, "\n"))
	}
	if fixes := changeRows(paneText(n, TabChanges, KindFix, paneTestWidth), Release{Changes: append(older.Changes, newest.Changes...)}); len(fixes) != 11 {
		t.Fatalf("the filter spans the range, got %d fixes", len(fixes))
	}
	if strip := paneText(n, TabOverview, "", paneTestWidth); rowIndex(strip, "Changes 26") < 0 || rowIndex(strip, "Thanks 10") < 0 {
		t.Fatalf("tab counts cover the range:\n%s", strings.Join(strip, "\n"))
	}
	overview := strings.Join(paneText(n, TabOverview, "", paneTestWidth), "\n")
	for _, want := range []string{"v0.45.0 · Settings own the keys", "Keys and the editor live in Settings", "1 feature · 1 fix"} {
		if !strings.Contains(overview, want) {
			t.Fatalf("Overview misses the older release's %q:\n%s", want, overview)
		}
	}
	thanks := paneText(n, TabThanks, "", paneTestWidth)
	var ipl []string
	for _, row := range thanks {
		if strings.Contains(row, "@ipl31") {
			ipl = append(ipl, row)
		}
	}
	if len(ipl) != 1 || !strings.Contains(ipl[0], "#688 #929 #930") {
		t.Fatalf("@ipl31 is thanked once with both releases' numbers: %q", ipl)
	}
	if built, found := rowIndex(thanks, "BUILT IT"), rowIndex(thanks, "FOUND IT"); !(built < rowIndex(thanks, "@ipl31") && rowIndex(thanks, "@ipl31") < found) {
		t.Fatalf("the group of the first appearance wins:\n%s", strings.Join(thanks, "\n"))
	}
	if !strings.Contains(strings.Join(thanks, "\n"), "10 people made these releases since 0.45.0. Thank you.") {
		t.Fatalf("range thank-you line:\n%s", strings.Join(thanks, "\n"))
	}
}

func TestPartialRangeSaysTheNotesAreComplete(t *testing.T) {
	n := updatedNotice(t, tabsRelease(t))
	n.RangeComplete = false
	for _, tab := range []Tab{TabOverview, TabChanges} {
		joined := strings.Join(strings.Fields(strings.Join(paneText(n, tab, "", 50), " ")), " ")
		if !strings.Contains(joined, "The local catalog covers part of this range; ↵ opens the complete notes.") {
			t.Fatalf("tab %d hides that the range is partial:\n%s", tab, joined)
		}
	}
}

func TestCursorMoveResetsTabAndFilter(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	p.Key(key("tab"), notices, testFrame())
	p.Key(key("f"), notices, testFrame())
	p.Key(key("tab"), notices, testFrame())
	if p.tab != TabThanks || p.filter != KindFeature {
		t.Fatalf("tab %d filter %q, want Thanks with the features filter", p.tab, p.filter)
	}
	p.Key(key("down"), notices, testFrame())
	p.Key(key("up"), notices, testFrame())
	if p.tab != TabOverview || p.filter != "" {
		t.Fatalf("after moving: tab %d filter %q", p.tab, p.filter)
	}
	p.Key(key("shift+tab"), notices, testFrame())
	if p.tab != TabThanks {
		t.Fatalf("shift+tab from Overview lands on %d", p.tab)
	}
}

func TestHeaderTimesAreIsraelTime(t *testing.T) {
	inIsraelTime(t)
	release := tabsRelease(t)
	release.PublishedAt = published(t, "2026-10-25T21:56:53Z")
	if header := paneText(updatedNotice(t, release), TabOverview, "", paneTestWidth)[0]; !strings.Contains(header, "Oct 25 · you were on 0.45.0") {
		t.Fatalf("header %q", header)
	}
	release.PublishedAt = time.Time{}
	if header := paneText(updatedNotice(t, release), TabOverview, "", paneTestWidth)[0]; strings.Contains(header, "Jan 1") || !strings.Contains(header, "you were on 0.45.0") {
		t.Fatalf("a release without a date: %q", header)
	}
}

func TestCompatibleReleasePaneIsASCII(t *testing.T) {
	inIsraelTime(t)
	for _, n := range []Notice{availableNotice(t, tabsRelease(t)), updatedNotice(t, tabsRelease(t))} {
		for _, tab := range []Tab{TabOverview, TabChanges, TabThanks} {
			for _, row := range paneRows(n, tab, "", paneTestWidth, testTheme(), presentation.GlyphModeBasic) {
				for _, r := range ansi.Strip(row) {
					if r > 0x7f && !strings.ContainsRune("─·…", r) {
						t.Fatalf("%s tab %d: %q in %q", n.ID, tab, r, ansi.Strip(row))
					}
				}
			}
		}
	}
}

func TestClickOnATabOpensIt(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	for _, width := range []int{100, 160} {
		p := NewPanel(map[string]bool{}, map[string]bool{})
		p.Open(notices, "")
		f := inboxFrame(width)
		line, col := -1, -1
		for row, plain := range plainRows(p.View(notices, f)) {
			if at := strings.Index(plain, "Thanks 10"); at >= 0 {
				line, col = row-1, len([]rune(plain[:at]))-2
			}
		}
		if out := p.Click(col+1, line, notices, f); out.Action != Stay || p.tab != TabThanks || p.Cursor() != 0 {
			t.Fatalf("width %d: click on the Thanks tab = %+v, tab %d", width, out, p.tab)
		}
	}
}

func TestReleaseFootNamesTabAndFilterOnChanges(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip", URL: RepoURL}}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	f := inboxFrame(160)
	edge := func() string { view := p.View(notices, f); return ansi.Strip(view[len(view)-1]) }
	if got := edge(); !strings.Contains(got, "tab section") || strings.Contains(got, "f filter") {
		t.Fatalf("Overview edge %q", got)
	}
	p.Key(key("tab"), notices, f)
	if got := edge(); !strings.Contains(got, "tab section") || !strings.Contains(got, "f filter") {
		t.Fatalf("Changes edge %q", got)
	}
	p.Key(key("down"), notices, f)
	if got := edge(); strings.Contains(got, "tab section") {
		t.Fatalf("a feed message names the tab key: %q", got)
	}
}

func TestReleasePaneScrollsUnderAFixedHeader(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t))}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	f := inboxFrame(160)
	f.Height = 24
	p.Key(key("tab"), notices, f)
	p.Key(key("end"), notices, f)
	if p.Offset() == 0 {
		t.Fatal("the Changes tab should scroll at this height")
	}
	view := plainRows(p.View(notices, f))
	joined := strings.Join(view, "\n")
	if !strings.Contains(joined, "you were on 0.45.0") || !strings.Contains(joined, "Changes 24") {
		t.Fatalf("the header scrolled away:\n%s", joined)
	}
	if !strings.Contains(joined, "Run the panel scenario") || len(view) > f.Height {
		t.Fatalf("end shows the last change within the frame:\n%s", joined)
	}
}

func TestOmittedChangesPointToTheFullNotes(t *testing.T) {
	release := tabsRelease(t)
	release.TotalChanges = 30
	if rows := paneText(updatedNotice(t, release), TabChanges, "", paneTestWidth); rowIndex(rows, "+6 more in the full notes") < 0 {
		t.Fatalf("the cut changes are not mentioned:\n%s", strings.Join(rows, "\n"))
	}
}

func TestClickOnAFilterChipFilters(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t))}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	f := inboxFrame(160)
	p.Key(key("tab"), notices, f)
	for row, plain := range plainRows(p.View(notices, f)) {
		if at := strings.Index(plain, "● 10"); at >= 0 {
			p.Click(len([]rune(plain[:at]))-2, row-1, notices, f)
			break
		}
	}
	if p.tab != TabChanges || p.filter != KindFix {
		t.Fatalf("click on the fixes chip: tab %d filter %q", p.tab, p.filter)
	}
}

func TestEmptyOverviewSaysWhatIsMissing(t *testing.T) {
	rows := releaseTab(updatedNotice(t, Release{Version: "v0.46.0"}), TabOverview, "", paneTestWidth, testTheme(), presentation.DefaultGlyphMode)
	if got := plainRows(rows); len(got) != 1 || got[0] != "No summarized changes." {
		t.Fatalf("an empty release's Overview = %q", got)
	}
}

func TestFilterMatchingNothingSaysSoOverOmittedChanges(t *testing.T) {
	release := tabsRelease(t)
	release.Changes = release.Changes[:6]
	release.TotalChanges = 30
	rows := paneText(updatedNotice(t, release), TabChanges, KindFix, paneTestWidth)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "No fixes.") || strings.Contains(joined, "more in the full notes") {
		t.Fatalf("a fixes filter over features only:\n%s", joined)
	}
	if all := paneText(updatedNotice(t, release), TabChanges, "", paneTestWidth); rowIndex(all, "+24 more in the full notes") < 0 {
		t.Fatalf("the unfiltered list lost the omitted count:\n%s", strings.Join(all, "\n"))
	}
}

func TestChipInkReadsOnEveryTheme(t *testing.T) {
	useTrueColor(t)
	previous := uitheme.Current()
	t.Cleanup(func() { uitheme.Apply(previous) })
	for _, palette := range uitheme.Palettes {
		uitheme.Apply(palette)
		theme := PanelTheme()
		for _, kind := range []Kind{KindRelease, KindNews, KindHeadsUp, KindTip} {
			fill := kindColor(kind, theme)
			best, ratio := theme.Ink, uitheme.ContrastRatio(string(theme.Ink), string(fill))
			if bright := uitheme.ContrastRatio(string(theme.Bright), string(fill)); bright > ratio {
				best, ratio = theme.Bright, bright
			}
			want := lipgloss.NewStyle().Background(fill).Foreground(best).Bold(true).Render(" LABEL ")
			if got := chip("LABEL", fill, theme); got != want {
				t.Errorf("%s %s chip: want the %s ink at %.2f:1, got %q", palette.Name, groupLabels[kind], best, ratio, got)
			}
			if ratio < 4.5 {
				t.Logf("%s %s chip: the better ink %s on %s reaches only %.2f:1", palette.Name, groupLabels[kind], best, fill, ratio)
			}
		}
	}
}

func TestChangesGroupFeaturesFixesAndTheRest(t *testing.T) {
	release := tabsRelease(t)
	release.Changes = []Change{
		{Kind: KindFix, Scope: "UI", Text: "First fix"},
		{Kind: KindOther, Scope: "CI", Text: "Some chore"},
		{Kind: KindFeature, Scope: "UI", Text: "A feature"},
		{Kind: KindFix, Scope: "UI", Text: "Second fix"},
	}
	release.TotalChanges = len(release.Changes)
	rows := paneText(updatedNotice(t, release), TabChanges, "", paneTestWidth)
	feature, first, second, chore := rowIndex(rows, "A feature"), rowIndex(rows, "First fix"), rowIndex(rows, "Second fix"), rowIndex(rows, "Some chore")
	if !(feature < first && first+1 == second && second < chore) || rows[first-1] != "" || rows[chore-1] != "" {
		t.Fatalf("features, fixes and the rest form blocks in note order:\n%s", strings.Join(rows, "\n"))
	}
	fixes := paneText(updatedNotice(t, release), TabChanges, KindFix, paneTestWidth)
	if at := rowIndex(fixes, "First fix"); at < 0 || fixes[at-1] != "" || rowIndex(fixes, "A feature") >= 0 {
		t.Fatalf("a filtered list holds one block:\n%s", strings.Join(fixes, "\n"))
	}
}

func TestArrowsAndHLStepTheTabs(t *testing.T) {
	notices := []Notice{updatedNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	for _, step := range []struct {
		key  string
		want Tab
	}{
		{"right", TabChanges},
		{"l", TabThanks},
		{"right", TabOverview},
		{"left", TabThanks},
		{"h", TabChanges},
	} {
		if out := p.Key(key(step.key), notices, testFrame()); out.Action != Stay || p.tab != step.want {
			t.Fatalf("%s = %+v on tab %d, want tab %d", step.key, out, p.tab, step.want)
		}
	}
	p.Key(key("down"), notices, testFrame())
	if p.Key(key("right"), notices, testFrame()); p.Cursor() != 1 {
		t.Fatalf("→ on a feed message moved the cursor to %d", p.Cursor())
	}
}

func TestClickOnTheUpdateBarUpdates(t *testing.T) {
	notices := []Notice{availableNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	for _, width := range []int{100, 160} {
		p := NewPanel(map[string]bool{}, map[string]bool{})
		p.Open(notices, "")
		f := inboxFrame(width)
		line, label, link := -1, -1, -1
		for row, plain := range plainRows(p.View(notices, f)) {
			if at := strings.Index(plain, "Update to 0.46.0"); at >= 0 {
				line, label = row-1, len([]rune(plain[:at]))-2
				link = len([]rune(plain[:strings.Index(plain, "notes on GitHub")])) - 2
			}
		}
		if line < 0 {
			t.Fatalf("width %d: no update bar", width)
		}
		if out := p.Click(link+1, line, notices, f); out.Action == StartUpdate {
			t.Fatalf("width %d: a click on the notes link started an update", width)
		}
		if out := p.Click(label+1, line, notices, f); out.Action != StartUpdate {
			t.Fatalf("width %d: click on the update bar = %+v", width, out)
		}
	}
}

// clickAt is the click line and column of the first cell of want in view.
func clickAt(t *testing.T, view []string, want string) (line, col int) {
	t.Helper()
	for row, plain := range plainRows(view) {
		if at := strings.Index(plain, want); at >= 0 {
			return row - 1, len([]rune(plain[:at])) - 1
		}
	}
	t.Fatalf("%q is not on screen:\n%s", want, strings.Join(plainRows(view), "\n"))
	return -1, -1
}

func TestClickOnALinkOpensIt(t *testing.T) {
	release := updatedNotice(t, tabsRelease(t))
	feedLink := Notice{ID: "feed-discord", Kind: KindNews, Subtitle: "banner", Title: "Join", URL: discordURL, Action: "Join on Discord"}
	notices := []Notice{release, feedLink}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	f := inboxFrame(160)
	line, col := clickAt(t, p.View(notices, f), "notes on GitHub")
	if out := p.Click(col, line, notices, f); out.Action != OpenLink || out.URL != release.URL {
		t.Fatalf("click on the notes link = %+v", out)
	}
	p.Key(key("down"), notices, f)
	line, col = clickAt(t, p.View(notices, f), "discord.gg")
	if out := p.Click(col, line, notices, f); out.Action != OpenLink || out.URL != discordURL {
		t.Fatalf("click on the feed bar = %+v", out)
	}
}

func TestTabsTakeClicksWhenTheHeaderScrolls(t *testing.T) {
	notices := []Notice{availableNotice(t, tabsRelease(t)), {ID: "feed-tip", Kind: KindTip, Title: "A tip"}}
	p := NewPanel(map[string]bool{}, map[string]bool{})
	p.Open(notices, "")
	f := testFrame()
	f.Width, f.Height = 80, 16
	line, col := clickAt(t, p.View(notices, f), "Thanks 10")
	if out := p.Click(col, line, notices, f); out.Action != Stay || p.tab != TabThanks {
		t.Fatalf("click on the Thanks tab of a short pane = %+v, tab %d", out, p.tab)
	}
}
