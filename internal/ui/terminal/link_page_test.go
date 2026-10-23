package terminal

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
)

func stubLinkCopy(t *testing.T, copyErr error) *string {
	t.Helper()
	original := copyToClipboard
	copied := new(string)
	copyToClipboard = func(target string) error {
		*copied = target
		return copyErr
	}
	t.Cleanup(func() { copyToClipboard = original })
	return copied
}

func runLinkPage(t *testing.T, target, input string) string {
	t.Helper()
	var out strings.Builder
	page := &LinkPage{URL: target}
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

type brokenTerminal struct{}

func (brokenTerminal) Write([]byte) (int, error) { return 0, errors.New("input/output error") }

// A terminal that took none of the page, as after the ssh link dropped, is a
// failure for the manager to report, not a page the user saw.
func TestLinkPageReportsAFailedWrite(t *testing.T) {
	stubLinkCopy(t, nil)
	page := &LinkPage{URL: "https://example.com/manual"}
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
