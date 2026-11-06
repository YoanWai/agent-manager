package inbox

import (
	"fmt"
	"strings"
	"time"

	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	uitheme "github.com/YoanWai/agent-manager/internal/ui/theme"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Entry is one message as the inbox shows it, copied from the log by the
// root.
type Entry struct {
	ID       int64
	From     string
	To       string
	SentAt   time.Time
	Subject  string
	State    string
	Reason   string
	Position int
}

// Log is one load: what still waits, next first, then the latest messages
// that already left the queue, oldest first.
type Log struct {
	Queued []Entry
	Recent []Entry
}

// Split builds a Log from the queue and the latest log, which holds the
// queued messages too.
func Split(queued, latest []Entry) Log {
	log := Log{Queued: queued}
	for _, entry := range latest {
		if !waiting(entry.State) {
			log.Recent = append(log.Recent, entry)
		}
	}
	return log
}

func waiting(state string) bool { return state == "queued" || state == "held" }

func (l Log) entries() []Entry { return append(append([]Entry(nil), l.Queued...), l.Recent...) }

// clock is a sent time in local time, dated when it is not today.
func clock(at, now time.Time) string {
	at, now = at.Local(), now.Local()
	if at.Year() == now.Year() && at.YearDay() == now.YearDay() {
		return at.Format("15:04")
	}
	return at.Format("Jan 2 15:04")
}

func stateStyle(state string) lipgloss.Style {
	switch state {
	case "held":
		return uitheme.WarnStyle()
	case "dropped":
		return uitheme.ErrStyle()
	case "queued":
		return uitheme.ValueStyle()
	}
	return uitheme.MutedStyle()
}

// listLines paints the list at width and names the entry each line belongs
// to, -1 for a heading or a gap.
func listLines(log Log, cursor, width int, now time.Time, mode presentation.GlyphMode) ([]string, []int) {
	var lines []string
	var owners []int
	add := func(line string, owner int) {
		lines = append(lines, ansi.Truncate(line, width, "…"))
		owners = append(owners, owner)
	}
	index := 0
	section := func(title string, entries []Entry) {
		if len(entries) == 0 {
			return
		}
		if len(lines) > 0 {
			add("", -1)
		}
		add(uitheme.SectionStyle().Render(fmt.Sprintf("%s (%d)", title, len(entries))), -1)
		for _, entry := range entries {
			add(entryLine(entry, index == cursor, now, mode), index)
			for _, line := range reasonLines(entry.Reason, width) {
				add(line, index)
			}
			index++
		}
	}
	section("Waiting", log.Queued)
	section("Recent", log.Recent)
	return lines, owners
}

// reasonLines wraps a hold or drop reason under its message at spaces only,
// so the command that clears it stays whole on a narrow card.
func reasonLines(reason string, width int) []string {
	const indent = "    "
	limit := max(width-len(indent), 1)
	var lines []string
	line := ""
	for _, word := range strings.Fields(reason) {
		if line != "" && ansi.StringWidth(line)+1+ansi.StringWidth(word) > limit {
			lines = append(lines, indent+uitheme.SubtleStyle().Render(line))
			line = ""
		}
		if line != "" {
			line += " "
		}
		line += word
	}
	if line != "" {
		lines = append(lines, indent+uitheme.SubtleStyle().Render(line))
	}
	return lines
}

func entryLine(entry Entry, selected bool, now time.Time, mode presentation.GlyphMode) string {
	mark := "  "
	if selected {
		mark = uitheme.KeyStyle().Render(presentation.GlyphsFor(mode).SelectionGlyph) + " "
	}
	state := entry.State
	if entry.Position > 0 {
		state = fmt.Sprintf("%s #%d", state, entry.Position)
	}
	names := presentation.EscapeControlsInline(entry.From) + " " + presentation.NavigationFor(mode).ToGlyph + " " + presentation.EscapeControlsInline(entry.To)
	nameStyle := uitheme.ValueStyle()
	if selected {
		nameStyle = nameStyle.Bold(true)
	}
	return mark + uitheme.SubtleStyle().Render(clock(entry.SentAt, now)) + "  " +
		nameStyle.Render(names) + "  " + stateStyle(entry.State).Render(state) + "  " +
		uitheme.MutedStyle().Render(entry.Subject)
}

// bodyLines is a message read in full: who, when and where it stands, then
// its text wrapped to width with any control character shown, not obeyed.
func bodyLines(entry Entry, body string, width int, now time.Time, mode presentation.GlyphMode) []string {
	head := []string{
		uitheme.ValueStyle().Bold(true).Render(presentation.EscapeControlsInline(entry.From)+" "+presentation.NavigationFor(mode).ToGlyph+" "+presentation.EscapeControlsInline(entry.To)) +
			"  " + uitheme.SubtleStyle().Render(clock(entry.SentAt, now)) + "  " + stateStyle(entry.State).Render(entry.State),
	}
	if entry.Reason != "" {
		head = append(head, uitheme.SubtleStyle().Render(entry.Reason))
	}
	if entry.Subject != "" {
		head = append(head, uitheme.MutedStyle().Render(entry.Subject))
	}
	var lines []string
	for _, line := range head {
		lines = append(lines, strings.Split(ansi.Wrap(line, width, ""), "\n")...)
	}
	lines = append(lines, "")
	for _, line := range strings.Split(body, "\n") {
		shown := presentation.EscapeControls(strings.ReplaceAll(line, "\t", "    "))
		lines = append(lines, strings.Split(ansi.Wrap(shown, width, ""), "\n")...)
	}
	return lines
}
