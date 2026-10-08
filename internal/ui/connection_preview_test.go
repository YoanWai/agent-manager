package ui

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
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

// hostile is what a compromised host could put in any string it returns:
// an OSC 52 clipboard write, a carriage return, a backspace and a bell.
const hostile = "\x1b]52;c;cHduZWQ=\x07\r\b\x07"

// A connection's host is not trusted to drive the terminal: whatever it
// names, reports or prints reaches a frame as text.
func TestHostTextCannotDriveTheTerminal(t *testing.T) {
	fake := &fakeSSH{}
	m := connectedModel(t, fake)
	m.layout.width, m.layout.height = 120, 40
	group := "web" + hostile
	fake.mu.Lock()
	fake.snapshot.Groups = []sessioncmd.Group{{Path: group}}
	fake.snapshot.Sessions = []sessioncmd.Session{{ID: "s1", Name: "api" + hostile, Tool: "claude" + hostile, Group: group, Status: "idle" + hostile}}
	fake.snapshot.Terminals = nil
	fake.screen = "build" + hostile + "ing\x1bc\x0e\x0fdone\n$ "
	raw := fake.snapshot
	fake.mu.Unlock()
	m.applyTestMsg(t, m.pollConnection("box")())
	// internal/remote already cleans what a host answers; the UI escapes on
	// its own too, so this test hands it the host's raw rows.
	state := m.ssh.hosts["box"]
	state.Snapshot = raw
	m.ssh.hosts["box"] = state
	m.rebuildRows()

	frame := func(surface string) {
		t.Helper()
		view := preparedView(m)
		if stray := strayControl(view); stray != "" {
			t.Fatalf("%s leaks a control byte near %q", surface, stray)
		}
		if !strings.Contains(view, "52;c;cHduZWQ=") {
			t.Fatalf("%s: the host's text should be on screen to be a real test:\n%s", surface, ansi.Strip(view))
		}
	}
	session := uirail.Selection{Kind: uirail.SessionRow, SessionID: "s1", Group: group, Host: "box"}
	m.rail.Focus(session)
	m.applyTestMsg(t, m.syncRemotePreview()())
	if text := contentText(m); !strings.Contains(text, "building") || !strings.Contains(text, "done") {
		t.Fatalf("the screen lost its text:\n%s", text)
	}
	frame("the rail and the remote screen")

	m.rail.Focus(uirail.Selection{Kind: uirail.GroupRow, Group: group, Host: "box"})
	frame("a remote group")
	m.openForm()
	m.form.focus = fieldGroup
	frame("the New Session form")
	m.mode = modeList

	m.rail.Focus(session)
	m.openQuickMode()
	frame("the quick bar")
	m.quick.active = false

	_, cmd := m.runRailIntent(uirail.Intent{Kind: uirail.Kill, Target: session})
	m.runRemoteCmd(t, cmd)
	if m.mode != modeConfirmDelete {
		t.Fatalf("mode = %v, want the kill confirm", m.mode)
	}
	frame("the confirm")
	m.mode = modeList

	fake.mu.Lock()
	fake.stderr, fake.code = "denied"+hostile, 1
	fake.mu.Unlock()
	m.ssh.preview = remotePreview{}
	m.applyTestMsg(t, m.syncRemotePreview()())
	frame("a failed read")

	m.applyTestMsg(t, m.pollConnection("box")())
	state = m.ssh.hosts["box"]
	state.Snapshot, state.Err = raw, errors.New("box: denied"+hostile)
	m.ssh.hosts["box"] = state
	m.rebuildRows()
	m.rail.Focus(boxRow)
	frame("an offline host")
}
