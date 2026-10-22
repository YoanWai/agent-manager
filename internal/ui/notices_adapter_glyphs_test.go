package ui

import (
	"github.com/YoanWai/agent-manager/internal/feed"
	uinotices "github.com/YoanWai/agent-manager/internal/ui/notices"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
	"strings"
	"testing"
)

func TestNoticeMarksAreResolvedWithoutChangingExternalContent(t *testing.T) {
	m := noticeModel(noticeStore(t), "v1.0.0")
	const text = "⚙❯✳ literal notice"
	m.notices.feedMessages = []feed.Message{{ID: "literal", Title: text, Body: []string{text}, URL: "https://example.com"}}
	m.update.latest = "v1.1.0"
	m.update.refreshing, m.update.applying = true, true
	for _, tc := range []struct {
		mode                                                   presentation.GlyphMode
		feed, welcome, update, refresh, download, link, cursor string
	}{
		{presentation.GlyphModeBasic, "i", "i", "^", "R", "v", "@", ">"},
		{presentation.GlyphModeAdvanced, "◆", "✳", "↑", "↻", "↓", "↗", "▸"},
	} {
		m.prefs.glyphMode = tc.mode
		notices := m.activeNotices()
		for _, n := range notices {
			want := ""
			switch n.ID {
			case "literal":
				want = tc.feed
			case uinotices.Welcome:
				want = tc.welcome
			case "update-v1.1.0":
				want = tc.update
			}
			if want != "" && n.Glyph != want {
				t.Fatalf("%s/%s glyph=%q", tc.mode, n.ID, n.Glyph)
			}
			if n.ID == "literal" && (n.Title != text || strings.Join(n.Body, "") != text) {
				t.Fatal("external message rewritten")
			}
		}
		m.layout.width, m.layout.height = 120, 40
		m.notices.Open(notices, "literal")
		got := ansi.Strip(strings.Join(m.notices.View(notices, m.noticesFrame()), "\n"))
		tail := got
		for _, want := range []string{tc.refresh + " refreshing", tc.download + " downloading", tc.link + " example.com"} {
			if !strings.Contains(tail, want) {
				t.Fatalf("%s tail missing %q: %s", tc.mode, want, tail)
			}
		}
		m.notices.Open(notices, "update-v1.1.0")
		got = ansi.Strip(strings.Join(m.viewNotices(), "\n"))
		if !strings.Contains(got, tc.cursor+" "+tc.update) {
			t.Fatalf("%s notice cursor = %s", tc.mode, got)
		}
	}
}
