package ui

import (
	"time"

	uiframe "github.com/YoanWai/agent-manager/internal/ui/frame"
	"github.com/YoanWai/agent-manager/internal/ui/presentation"
	"github.com/charmbracelet/lipgloss"
)

func rowColumns(lead, meta string, width int) string { return uiframe.RowColumns(lead, meta, width) }

// divider renders a labeled section rule that fills the given width: an
// accent tick, the label, then a hairline out to the edge.
func divider(label string, width int) string {
	return uiframe.Divider(label, width, sectionStyle(), lipgloss.NewStyle().Foreground(colorBorder()))
}

func displayGroup(path string) string { return uiframe.DisplayGroup(path) }

func relSince(t time.Time) string { return uiframe.RelSince(t) }

func diskBytes(b uint64) string { return uiframe.DiskBytes(b) }

func humanBytes(b uint64) string { return uiframe.HumanBytes(b) }

func shortHome(path, home string) string { return uiframe.ShortHome(path, home) }

func truncateTail(s string, max int) string { return presentation.TruncateTail(s, max) }

func scrollWindow(total, cursor, height int) (int, int) {
	return uiframe.ScrollWindow(total, cursor, height)
}
