package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/clipboard"
	uiterminal "github.com/YoanWai/agent-manager/internal/ui/terminal"
)

// overSSH puts the test on a terminal at the far end of an SSH session, and
// localTerminal is the suite's everyday answer.
func localTerminal() bool { return false }

func overSSH(t *testing.T) {
	t.Helper()
	remoteTerminal = func() bool { return true }
	t.Cleanup(func() { remoteTerminal = localTerminal })
}

// Over SSH the opener would start a browser on the remote host, out of the
// user's sight, so the link goes to the page instead.
func TestOpenLinkOverSSHShowsThePage(t *testing.T) {
	overSSH(t)
	opened := ""
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = uiterminal.DefaultOpenBrowser })

	target := "https://example.com/issues/new?template=bug_report.yml"
	msg := openLink(target)()
	if opened != "" {
		t.Fatalf("the remote host opened %q", opened)
	}
	if msg != (linkPageMsg{url: target}) {
		t.Fatalf("openLink() = %#v, want the page for %q", msg, target)
	}
}

func TestOpenBrowserFailureCopiesURL(t *testing.T) {
	m := modalModel(t)
	openBrowser = func(string) error { return errors.New("no browser available") }
	var copied string
	copyBrowserURL = func(target string) error {
		copied = target
		return nil
	}
	t.Cleanup(func() {
		openBrowser = uiterminal.DefaultOpenBrowser
		copyBrowserURL = clipboard.WriteText
	})

	target := "https://example.com/manual"
	m.applyCmd(t, openLink(target))
	if copied != target {
		t.Fatalf("copied %q, want %q", copied, target)
	}
	if !strings.Contains(m.errBar.text, "URL copied") || !strings.Contains(m.errBar.text, "no browser available") {
		t.Fatalf("failure = %q, want copy outcome and cause", m.errBar.text)
	}
}

func TestOpenBrowserAndCopyFailureNamesURL(t *testing.T) {
	m := modalModel(t)
	openBrowser = func(string) error { return errors.New("no browser available") }
	copyBrowserURL = func(string) error { return errors.New("no clipboard available") }
	t.Cleanup(func() {
		openBrowser = uiterminal.DefaultOpenBrowser
		copyBrowserURL = clipboard.WriteText
	})

	target := "https://example.com/manual"
	m.applyCmd(t, openLink(target))
	if !strings.Contains(m.errBar.text, target) || !strings.Contains(m.errBar.text, "no clipboard available") {
		t.Fatalf("failure = %q, want URL and copy cause", m.errBar.text)
	}
}

func TestFocusLinkOverSSHShowsThePage(t *testing.T) {
	overSSH(t)
	opened := ""
	prev := openURL
	openURL = func(url string) error { opened = url; return nil }
	t.Cleanup(func() { openURL = prev })

	msg := openLinkCmd("https://example.com/docs")()
	if opened != "" {
		t.Fatalf("the remote host opened %q", opened)
	}
	if msg != (linkPageMsg{url: "https://example.com/docs"}) {
		t.Fatalf("openLinkCmd() = %#v, want the page", msg)
	}
}

func TestLinkOpenFailureReachesTheErrorBar(t *testing.T) {
	prev := openURL
	openURL = func(string) error { return errors.New("no opener") }
	t.Cleanup(func() { openURL = prev })

	msg := openLinkCmd("https://example.com/docs?token=secret")()
	failure, ok := msg.(linkOpenErrMsg)
	if !ok {
		t.Fatalf("openLinkCmd returned %T, want LinkOpenErrMsg", msg)
	}
	if !strings.Contains(failure.err.Error(), "no opener") {
		t.Fatalf("the opener's reason was dropped: %v", failure.err)
	}
	if strings.Contains(failure.err.Error(), "token=secret") {
		t.Fatalf("the error repeats the URL: %v", failure.err)
	}
	m := &Model{}
	m.Update(failure)
	if !strings.Contains(m.errBar.text, "no opener") {
		t.Fatalf("error bar = %q, want the opener's failure", m.errBar.text)
	}
}
