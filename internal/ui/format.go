package ui

import (
	"fmt"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"os"
	"strings"
	"time"
)

// rowColumns lays a list row out as a name column and a right-aligned meta
// column, so status, tool and age line up down the list instead of ragging
// off the end of each name. Rows too narrow to split keep meta inline and
// let the caller's truncation decide what survives.
func rowColumns(lead, meta string, width int) string {
	if meta == "" {
		return lead
	}
	const gap = 2
	leadWidth := ansi.StringWidth(lead)
	metaWidth := ansi.StringWidth(meta)
	if width < 1 || leadWidth+gap+metaWidth > width {
		return lead + strings.Repeat(" ", gap) + meta
	}
	return lead + strings.Repeat(" ", width-leadWidth-metaWidth) + meta
}

// divider renders a labeled section rule that fills the given width: an
// accent tick, the label, then a hairline out to the edge.
func divider(label string, width int) string {
	return presentation.Divider(
		label,
		width,
		sectionStyle,
		lipgloss.NewStyle().Foreground(colorBorder),
	)
}

func displayGroup(path string) string {
	if path == "" {
		return "root"
	}
	return path
}

// relSince is relTime worded as a moment in the past, for columns that
// answer "when did this last happen" rather than "how long has this run".
func relSince(t time.Time) string {
	return relTime(t) + " ago"
}

func relTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		// Five-second steps: a column of ages that ticks every second is
		// motion the eye chases for no information.
		return fmt.Sprintf("%ds", int(d.Seconds()/5)*5)
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// shortHome writes a path under the home directory the way a shell prompt
// does, so a narrow slot spends its room on the part that tells the
// directories apart rather than on the same prefix every session shares.
func shortHome(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	if path == home {
		return "~"
	}
	if strings.HasPrefix(path, home+string(os.PathSeparator)) {
		return "~" + strings.TrimPrefix(path, home)
	}
	return path
}

// truncateTail keeps the end of the string (best for paths).
func truncateTail(s string, max int) string {
	runes := []rune(s)
	if max <= 0 {
		return ""
	}
	if len(runes) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return "…" + string(runes[len(runes)-max+1:])
}

func truncatePath(path string, limit int) string {
	runes := []rune(path)
	if limit <= 0 {
		return ""
	}
	if len(runes) <= limit {
		return path
	}
	if limit == 1 {
		return "…"
	}
	tail := string(runes[len(runes)-limit+1:])
	if i := strings.IndexByte(tail, '/'); i >= 0 && i < len(tail)-1 {
		return "…" + tail[i:]
	}
	return "…" + tail
}

// scrollWindow keeps the cursor visible inside a height-limited window of
// single-line rows, reserving one line for each overflow indicator.
func scrollWindow(total, cursor, height int) (int, int) {
	if total <= height {
		return 0, total
	}
	visible := height - 2
	if visible < 1 {
		visible = 1
	}
	start := cursor - visible/2
	if start < 0 {
		start = 0
	}
	if start+visible > total {
		start = total - visible
	}
	return start, start + visible
}
