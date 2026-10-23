package connections

import (
	"errors"
	"strings"
	"testing"

	remote "github.com/YoanWai/agent-manager/internal/remote/value"
	"github.com/charmbracelet/lipgloss"
)

var plainScreen = ScreenView{Err: lipgloss.NewStyle(), Muted: lipgloss.NewStyle(), ExpandTabs: func(line string, _ int) string { return line }}

func agentRow(id string) Row {
	return Row{Kind: SessionRow, Host: "box", Ref: remote.Ref{Host: "box", ID: id}}
}

func TestPreviewDropsAReadForAnEarlierSelection(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	if !c.Preview.Follow(agentRow("s1"), true) {
		t.Fatal("a new row asked for no read")
	}
	stale, _ := c.StartRead()
	c.Preview.Follow(agentRow("s2"), true)
	fresh, ok := c.StartRead()
	if !ok {
		t.Fatal("the new selection could not read")
	}
	c.Preview.Apply(stale, "old screen", nil)
	if got := c.Preview.Screen(80, 5, plainScreen); got[0] != "reading the screen over SSH…" {
		t.Fatalf("screen = %q, want the s1 read dropped", got)
	}
	c.Preview.Apply(fresh, "new screen", nil)
	if got := c.Preview.Screen(80, 5, plainScreen); got[0] != "new screen" {
		t.Fatalf("screen = %q", got)
	}
}

func TestPreviewReadsOnceAndNotFromAnOfflineHost(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	c.Preview.Follow(agentRow("s1"), true)
	if _, ok := c.StartRead(); !ok {
		t.Fatal("no first read")
	}
	if _, again := c.StartRead(); again {
		t.Fatal("a second read went out while the first was")
	}
	if c.Preview.Follow(agentRow("s1"), true) {
		t.Fatal("the same row asked for another read")
	}
	var down Connections
	down.SetList([]remote.Connection{box})
	down.Refreshed("box", down.Gen(), Host{Err: "box: unreachable over SSH", Failures: 1})
	down.Preview.Follow(agentRow("s1"), true)
	if _, ok := down.StartRead(); ok {
		t.Fatal("a read went to an offline host")
	}
}

func TestPreviewShowsAFailedRead(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{box})
	c.Preview.Follow(agentRow("s1"), true)
	read, _ := c.StartRead()
	c.Preview.Apply(read, "", errors.New("box: no session named s1\x07"))
	if got := c.Preview.Screen(80, 5, plainScreen); len(got) != 1 || strings.ContainsRune(got[0], '\x07') || !strings.Contains(got[0], "no session named s1") {
		t.Fatalf("screen = %q", got)
	}
}

func TestScreenTextKeepsColorsNewlinesAndTabs(t *testing.T) {
	in := "\x1b[1;38;5;208mbuild\x1b[0m\r\b\x07\x1b]8;;http://x\x07ing\x1b[2J\tok\u009b31m\n$ "
	if got := ScreenText(in); got != "\x1b[1;38;5;208mbuild\x1b[0ming\tok31m\n$ " {
		t.Fatalf("ScreenText = %q", got)
	}
}

// A remote screen paints in its colors, and a color still set at the end of
// a row ends there rather than bleeding into the frame beside it.
func TestPreviewPaintsColors(t *testing.T) {
	var c Connections
	c.SetList([]remote.Connection{{Name: "box", Destination: "me@box"}})
	c.Preview.Follow(agentRow("s1"), true)
	read, _ := c.StartRead()
	c.Preview.Apply(read, "\x1b[32mok\x1b[0m done\n\x1b[31mfailed", nil)
	got := c.Preview.Screen(80, 5, plainScreen)
	if len(got) != 2 || got[0] != "\x1b[32mok\x1b[0m done\x1b[m" || got[1] != "\x1b[31mfailed\x1b[m" {
		t.Fatalf("screen = %q", got)
	}
}
