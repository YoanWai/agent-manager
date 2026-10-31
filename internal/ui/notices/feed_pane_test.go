package notices

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

const discordURL = "https://discord.gg/ecS88An7pF"

func feedText(n Notice, width int) []string {
	return plainRows(feedPane(n, width, testTheme(), presentation.DefaultGlyphMode))
}

func TestFeedPaneShowsKindChipBannerAndExpiry(t *testing.T) {
	useTrueColor(t)
	inIsraelTime(t)
	theme := testTheme()
	n := Notice{
		Kind:     KindHeadsUp,
		Subtitle: "control mode can crash",
		Title:    "tmux below 3.7",
		Body:     []string{"Update tmux before you attach."},
		Expires:  published(t, "2026-11-21T22:30:00Z"),
	}
	rows := feedPane(n, 80, theme, presentation.DefaultGlyphMode)
	plain := plainRows(rows)
	if !strings.HasPrefix(plain[0], " HEADS UP   control mode can crash ") || !strings.HasSuffix(plain[0], " until Nov 22") || lipgloss.Width(plain[0]) != 80 {
		t.Fatalf("first row = %q, want the chip, the banner and the local expiry date at the right", plain[0])
	}
	if want := chip("HEADS UP", theme.Working, theme) + "  " + theme.Muted.Render("control mode can crash"); !strings.HasPrefix(rows[0], want) || !strings.HasSuffix(rows[0], theme.Muted.Render("until Nov 22")) {
		t.Fatalf("chip and banner styled %q, want %q", rows[0], want)
	}
	if plain[1] != "" || rows[2] != headingStyle(theme).Render("tmux below 3.7") {
		t.Fatalf("a blank row, then the title bright bold:\n%s", strings.Join(plain, "\n"))
	}
	if at := rowIndex(plain, "Update tmux"); at != 4 || plain[3] != "" {
		t.Fatalf("a blank row and then the body:\n%s", strings.Join(plain, "\n"))
	}

	for kind, want := range map[Kind]string{KindNews: chip("NEWS", theme.Accent2, theme), KindTip: chip("TIP", theme.Waiting, theme)} {
		n := Notice{Kind: kind, Subtitle: "banner", Title: "title"}
		if rows := feedPane(n, 80, theme, presentation.DefaultGlyphMode); !strings.HasPrefix(rows[0], want) {
			t.Fatalf("%d chip = %q, want %q", kind, rows[0], want)
		}
		if got := feedText(n, 80)[0]; strings.Contains(got, "until") {
			t.Fatalf("an entry that never expires names a date: %q", got)
		}
	}
}

func TestFeedCallToActionUsesTheActionOrOpenLink(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	n := Notice{Kind: KindNews, Subtitle: "Agent Manager has a Discord now", Title: "Join the community", Body: []string{"Ask in #help."}, URL: discordURL, Action: "Join on Discord"}
	rows := feedPane(n, 80, theme, presentation.DefaultGlyphMode)
	plain := plainRows(rows)
	last := strings.TrimRight(plain[len(plain)-1], " ")
	if !strings.HasPrefix(last, "▍   ↵    Join on Discord  ") || !strings.HasSuffix(last, "  discord.gg") || lipgloss.Width(last) != 79 {
		t.Fatalf("call to action = %q, want the key, the action and the host at the right", last)
	}
	if !strings.Contains(rows[len(rows)-1], chip(" ↵ ", theme.Accent2, theme)) {
		t.Fatalf("the key is not on the kind's fill: %q", rows[len(rows)-1])
	}
	if !strings.Contains(rows[len(rows)-1], theme.Muted.Render("discord.gg")) {
		t.Fatalf("the link host is not dim: %q", rows[len(rows)-1])
	}
	if plain[len(plain)-2] != "" {
		t.Fatalf("no blank row above the call to action:\n%s", strings.Join(plain, "\n"))
	}

	n.Action = ""
	got := feedText(n, 80)
	if bar := strings.TrimRight(got[len(got)-1], " "); !strings.HasPrefix(bar, "▍   ↵    Open link  ") || !strings.HasSuffix(bar, "discord.gg") {
		t.Fatalf("without an action the bar reads %q, want Open link", got[len(got)-1])
	}
	if got := feedText(n, 30); lipgloss.Width(got[len(got)-1]) > 30 {
		t.Fatalf("the bar overflows a narrow pane: %q", got[len(got)-1])
	}
}

func TestFeedWithoutURLHasNoCallToAction(t *testing.T) {
	n := Notice{Kind: KindTip, Subtitle: "a quick one", Title: "Try the tip", Body: []string{"Press s for Settings."}, Action: "Open Settings"}
	rows := feedText(n, 80)
	for _, row := range rows {
		if strings.Contains(row, "▍") || strings.Contains(row, "↵") || strings.Contains(row, "Open") {
			t.Fatalf("a call to action without a link:\n%s", strings.Join(rows, "\n"))
		}
	}
	if rows[len(rows)-1] != "Press s for Settings." {
		t.Fatalf("the pane should end on its body:\n%s", strings.Join(rows, "\n"))
	}
}

func TestFeedKeepsAccentPhrases(t *testing.T) {
	useTrueColor(t)
	theme := testTheme()
	n := Notice{
		Kind:     KindNews,
		Subtitle: "Model + reasoning pickers are here!",
		Title:    "v0.40.0: model and reasoning pickers are here!",
		Accent:   []string{"ctrl+l"},
		Body:     []string{"In quick prompt mode, ctrl+l opens the model list."},
	}
	rows := feedPane(n, 80, theme, presentation.DefaultGlyphMode)
	if !strings.Contains(strings.Join(rows, "\n"), theme.Key.Render("ctrl+l")) {
		t.Fatalf("accent phrase is not styled:\n%q", rows)
	}
	if !strings.Contains(strings.Join(rows, "\n"), theme.Value.Render("In quick prompt mode, ")) {
		t.Fatalf("body text is not in the text colour:\n%q", rows)
	}
}

func TestFeedFootHasNoSectionOrFilterKeys(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	notices := p.Active(inboxSources())
	for _, id := range []string{inboxNews, inboxHeadsUp} {
		p.Open(notices, id)
		for _, width := range []int{100, 160} {
			view := plainRows(p.View(notices, inboxFrame(width)))
			foot := view[len(view)-1]
			if strings.Contains(foot, "tab") || strings.Contains(foot, "filter") {
				t.Fatalf("%s at %d: a feed message names keys it has no use for: %q", id, width, foot)
			}
		}
	}
}

func TestConfigImportWarningDrawsAsAHeadsUp(t *testing.T) {
	p := NewPanel(map[string]bool{}, map[string]bool{})
	src := inboxSources()
	src.ConfigImportError = "bad key"
	notices := p.Active(src)
	warning := notices[len(notices)-1]
	if warning.ID != ConfigNotImported || warning.Kind != KindHeadsUp {
		t.Fatalf("test setup: last notice is %s", warning.ID)
	}
	rows := feedText(warning, 80)
	if !strings.HasPrefix(rows[0], " HEADS UP   Settings could not import it") {
		t.Fatalf("first row = %q", rows[0])
	}
	if rowIndex(rows, "bad key") < 0 {
		t.Fatalf("the reason is missing:\n%s", strings.Join(rows, "\n"))
	}
	if got := strings.TrimRight(rows[len(rows)-1], " "); !strings.HasPrefix(got, "▍   ↵    Read what Settings holds  ") || !strings.HasSuffix(got, "  github.com") {
		t.Fatalf("call to action = %q, want the link's host", got)
	}
	if rowIndex(rows, "Enter opens") >= 0 {
		t.Fatalf("the bar already says where Enter goes:\n%s", strings.Join(rows, "\n"))
	}
}
