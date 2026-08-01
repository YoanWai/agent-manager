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
		t.Fatalf("want a messages card, got %q", joined)
	}
	if !strings.Contains(joined, "welcome") {
		t.Fatalf("want the welcome banner, got %q", joined)
	}

	for _, line := range lines {
		clean := ansi.Strip(line)
		if !strings.Contains(clean, "messages") {
			continue
		}
		if strings.Index(clean, "messages") < strings.Index(ansi.Strip(lines[0]), "computer") {
			t.Fatalf("messages must sit right of computer, got %q", clean)
		}
		return
	}
	t.Fatal("MESSAGES header row not found")
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

func TestRailFootCardBorderAndFit(t *testing.T) {
	m := footModel(t)
	lines := m.railFootLines(90)
	joined := ansi.Strip(strings.Join(lines, "\n"))
	for _, corner := range []string{"╭", "╮", "╰", "╯"} {
		if !strings.Contains(joined, corner) {
			t.Fatalf("card border missing %q:\n%s", corner, joined)
		}
	}
	if !strings.Contains(strings.Join(lines, "\n"), bgSeq(noticeCardHex())) {
		t.Fatal("card interior missing its fill")
	}
	for i, line := range lines {
		if !strings.Contains(ansi.Strip(line), "│") {
			t.Fatalf("row %d missing the separator: %q", i, ansi.Strip(line))
		}
	}

	var top string
	for _, line := range lines {
		if strings.Contains(ansi.Strip(line), "╭") {
			top = line
		}
	}
	if got := lipgloss.Width(top); got >= 90 {
		t.Fatalf("card must hug its content, top border spans %d of 90", got)
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
