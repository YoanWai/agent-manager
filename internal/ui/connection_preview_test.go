package ui

import (
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func contentText(m *Model) string {
	var lines []string
	for _, line := range m.contentLines(80, 20) {
		lines = append(lines, ansi.Strip(line.text))
	}
	return strings.Join(lines, "\n")
}

func TestRemotePreviewReadsTheSelectedScreenOffTheUpdatePath(t *testing.T) {
	fake := &fakeSSH{screen: "\x1b[1mbuilding\x1b[0m api\n$ "}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	cmd := m.syncRemotePreview()
	if cmd == nil || len(fake.taken()) != 0 {
		t.Fatal("the read was not deferred to a command")
	}
	if text := contentText(m); !strings.Contains(text, "api  on box") || !strings.Contains(text, "reading the screen over SSH") {
		t.Fatalf("content before the read:\n%s", text)
	}
	m.applyTestMsg(t, cmd())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"read", "--json", "--", "s1"}) {
		t.Fatalf("calls = %q", calls)
	}
	if text := contentText(m); !strings.Contains(text, "building api") || strings.Contains(text, "\x1b") {
		t.Fatalf("content after the read:\n%s", text)
	}
}

func TestRemotePreviewReadsATerminalAsOne(t *testing.T) {
	fake := &fakeSSH{screen: "$ ls"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("t1"))
	m.applyTestMsg(t, m.syncRemotePreview()())
	if calls := fake.taken(); len(calls) != 1 || !slices.Equal(calls[0], []string{"terminal", "read", "--json", "--", "t1"}) {
		t.Fatalf("calls = %q", calls)
	}
}

func TestRemotePreviewDropsAReadForAnEarlierSelection(t *testing.T) {
	fake := &fakeSSH{screen: "old screen"}
	m := connectedModel(t, fake)
	m.rail.Focus(boxSession("s1"))
	stale := m.syncRemotePreview()
	m.rail.Focus(boxSession("s2"))
	m.syncRemotePreview()
	m.applyTestMsg(t, stale())
	if m.ssh.preview.text != "" || m.ssh.preview.ref.ID != "s2" {
		t.Fatalf("preview = %+v, want the s1 read dropped", m.ssh.preview)
	}
}

func TestConnectionRowShowsItsFacts(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.rail.Focus(boxRow)
	text := contentText(m)
	for _, want := range []string{"box", "online", "me@box", "3 sessions · 1 terminals · manager awake"} {
		if !strings.Contains(text, want) {
			t.Fatalf("content lacks %q:\n%s", want, text)
		}
	}
}
