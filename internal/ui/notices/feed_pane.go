package notices

import (
	"net/url"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/x/ansi"
)

var feedLabels = [...]string{KindNews: "NEWS", KindHeadsUp: "HEADS UP", KindTip: "TIP"}

// feedPane is a news entry, heads-up or tip: its kind, banner and expiry,
// the title, the body, and the link it opens when it has one.
func feedPane(n Notice, width int, t Theme, mode presentation.GlyphMode) []string {
	head := chip(feedLabels[n.Kind], kindColor(n.Kind, t), t) + "  " + t.Muted.Render(n.Subtitle)
	if expires := feedExpiry(n); expires != "" {
		head = atRight(ansi.Truncate(head, max(width-ansi.StringWidth(expires)-2, 0), "…"), t.Muted.Render(expires), width)
	}
	rows := []string{ansi.Truncate(head, width, "…"), ""}
	rows = appendWrapped(rows, []textRun{{text: n.Title}}, width, headingStyle(t), t)
	var body []string
	for _, line := range n.Body {
		body = appendWrapped(body, phraseRuns(line, n.Accent), width, t.Value, t)
	}
	if len(body) > 0 {
		rows = spaced(rows, body...)
	}
	if n.URL == "" {
		return rows
	}
	action := n.Action
	if action == "" {
		action = "Open link"
	}
	bar, _ := actionBar("↵", action, t.Muted.Render(linkHost(n.URL)), kindColor(n.Kind, t), width, t, mode)
	return spaced(rows, bar)
}

// linkHost is the host a link leads to, or the whole URL when it does not parse.
func linkHost(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	return parsed.Host
}

func feedExpiry(n Notice) string {
	if n.Expires.IsZero() {
		return ""
	}
	return "until " + n.Expires.In(displayZone).Format("Jan 2")
}

// feedMeasure is a feed notice's rows as they would read unwrapped, for the
// modal's width.
func feedMeasure(n Notice) []string {
	return append([]string{" " + feedLabels[n.Kind] + "   " + n.Subtitle + "  " + feedExpiry(n), n.Title}, n.Body...)
}
