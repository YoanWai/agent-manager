package ui

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/clipboard"
)

func localTerminal() bool { return false }

// overSSH puts the test on a terminal at the far end of an SSH session.
func overSSH(t *testing.T) {
	t.Helper()
	remoteTerminal = func() bool { return true }
	t.Cleanup(func() { remoteTerminal = localTerminal })
}

func stubLinkCopy(t *testing.T, copyErr error) *string {
	t.Helper()
	copied := new(string)
	copyBrowserURL = func(target string) error {
		*copied = target
		return copyErr
	}
	t.Cleanup(func() { copyBrowserURL = clipboard.WriteText })
	return copied
}

func runLinkPage(t *testing.T, target, input string) string {
	t.Helper()
	var out strings.Builder
	page := &linkPage{url: target}
	page.SetStdin(strings.NewReader(input))
	page.SetStdout(&out)
	page.SetStderr(io.Discard)
	if err := page.Run(); err != nil {
		t.Fatalf("Run() = %v", err)
	}
	return out.String()
}

// The URL stands alone on its row, so the terminal's own link detection and
// a triple-click selection both take exactly the URL.
func TestLinkPagePrintsTheURLAndCopiesIt(t *testing.T) {
	copied := stubLinkCopy(t, nil)
	target := "https://github.com/YoanWai/agent-manager/issues/new?template=bug_report.yml&version=0.38.0"
	out := runLinkPage(t, target, "\n")
	if !slices.Contains(strings.Split(out, "\n"), target) {
		t.Fatalf("the URL has no row of its own:\n%s", out)
	}
	if *copied != target {
		t.Fatalf("copied %q, want %q", *copied, target)
	}
}

func TestLinkPageNamesACopyFailure(t *testing.T) {
	stubLinkCopy(t, errors.New("terminal clipboard unavailable"))
	out := runLinkPage(t, "https://example.com/manual", "\n")
	if !strings.Contains(out, "terminal clipboard unavailable") {
		t.Fatalf("the copy failure was dropped:\n%s", out)
	}
}

// The page is on the normal screen only until Run returns, when the manager
// takes the alternate screen back over it.
func TestLinkPageHoldsUntilEnter(t *testing.T) {
	stubLinkCopy(t, nil)
	input, typed := io.Pipe()
	page := &linkPage{url: "https://example.com/manual"}
	page.SetStdin(input)
	page.SetStdout(io.Discard)
	done := make(chan error, 1)
	go func() { done <- page.Run() }()

	select {
	case err := <-done:
		t.Fatalf("the page closed before Enter: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	if _, err := typed.Write([]byte("\n")); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Run() = %v", err)
	}
}

type brokenTerminal struct{}

func (brokenTerminal) Write([]byte) (int, error) { return 0, errors.New("input/output error") }

// A terminal that took none of the page, as after the ssh link dropped, is a
// failure for the manager to report, not a page the user saw.
func TestLinkPageReportsAFailedWrite(t *testing.T) {
	stubLinkCopy(t, nil)
	page := &linkPage{url: "https://example.com/manual"}
	page.SetStdin(strings.NewReader("\n"))
	page.SetStdout(brokenTerminal{})
	if err := page.Run(); err == nil || !strings.Contains(err.Error(), "input/output error") {
		t.Fatalf("Run() = %v, want the write failure", err)
	}
}

// Ctrl+D at the prompt closes the input rather than sending Enter. The user
// is just as done with the page.
func TestLinkPageClosedInputReturns(t *testing.T) {
	stubLinkCopy(t, nil)
	runLinkPage(t, "https://example.com/manual", "")
}

func TestLinkPageMsgHandsTheTerminalOver(t *testing.T) {
	m := buildModel(t)
	_, cmd := m.Update(linkPageMsg{url: "https://example.com/manual"})
	if cmd == nil || cmd() == nil {
		t.Fatal("a link page message started nothing")
	}
}
