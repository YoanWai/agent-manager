package ui

import (
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sysstat"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func footModel(t *testing.T) *Model {
	t.Helper()
	m := noticeModel(noticeStore(t), "v0.2.0")
	m.snap = sysstat.Snapshot{
		CPUPercent: 42, CPUOK: true,
		MemPercent: 63, MemOK: true, MemUsed: 10 << 30, MemTotal: 16 << 30,
		DiskPercent: 71, DiskOK: true, DiskFree: 120 << 30,
	}
	return m
}

func TestRailFootPutsMessagesRightOfComputer(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(70)

	joined := ansi.Strip(strings.Join(lines, "\n"))
	if !strings.Contains(joined, "messages") {
		t.Fatalf("want a messages panel, got %q", joined)
	}
	if !strings.Contains(joined, "welcome") {
		t.Fatalf("want the welcome banner, got %q", joined)
	}

	var computerRow, messagesRow int
	for i, line := range lines {
		clean := ansi.Strip(line)
		if strings.Contains(clean, "computer") {
			computerRow = i
		}
		if strings.Contains(clean, "messages") {
			messagesRow = i
		}
	}
	if computerRow != messagesRow {
		t.Fatalf("panels should share their header row: computer=%d messages=%d", computerRow, messagesRow)
	}
	header := ansi.Strip(lines[computerRow])
	if strings.Index(header, "computer") > strings.Index(header, "messages") {
		t.Fatalf("messages must sit right of computer, got %q", header)
	}
}

func TestRailFootNarrowDropsMessages(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(34)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	if strings.Contains(joined, "messages") {
		t.Fatalf("narrow rail should keep only the meters, got %q", joined)
	}
	if !strings.Contains(joined, "computer") {
		t.Fatalf("meters must survive, got %q", joined)
	}
}

func TestRailFootAllDismissedShowsOnlyMeters(t *testing.T) {
	m := footModel(t)
	for _, n := range m.activeNotices() {
		m.dismissNotice(n.id)
	}
	joined := ansi.Strip(strings.Join(m.railFootLines(70), "\n"))
	if strings.Contains(joined, "messages") {
		t.Fatalf("no notices means no panel, got %q", joined)
	}
}

func TestRailFootCardSeparatorAndFill(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(70)
	fill := bgSeq(noticeCardHex())
	for i, line := range lines {
		if !strings.Contains(ansi.Strip(line), "│") {
			t.Fatalf("row %d missing the separator: %q", i, ansi.Strip(line))
		}
		if !strings.Contains(line, fill) {
			t.Fatalf("row %d missing the card fill", i)
		}
		if got := lipgloss.Width(line); got != 70 {
			t.Fatalf("card must flex to the full width, row %d is %d", i, got)
		}
	}
}

func TestRailFootLinesFitWidth(t *testing.T) {
	m := footModel(t)
	m.update.latest = "v0.9.9"
	m.update.url = "https://example.com"
	for _, width := range []int{40, 55, 70} {
		for _, line := range m.railFootLines(width) {
			if got := lipgloss.Width(line); got > width {
				t.Fatalf("width %d: line overflows at %d: %q", width, got, ansi.Strip(line))
			}
		}
	}
}
