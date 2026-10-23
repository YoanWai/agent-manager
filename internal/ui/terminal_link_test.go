package ui

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/clipboard"
)

// Over SSH the opener would start a browser on the remote host, out of the
// user's sight, so the link goes to the page instead.
func TestOpenLinkOverSSHShowsThePage(t *testing.T) {
	overSSH(t)
	opened := ""
	openBrowser = func(url string) error {
		opened = url
		return nil
	}
	t.Cleanup(func() { openBrowser = defaultOpenBrowser })

	target := "https://example.com/issues/new?template=bug_report.yml"
	msg := openLink(target)()
	if opened != "" {
		t.Fatalf("the remote host opened %q", opened)
	}
	if msg != (linkPageMsg{url: target}) {
		t.Fatalf("openLink() = %#v, want the page for %q", msg, target)
	}
}

func TestBrowserCommandsHonorBrowserCandidates(t *testing.T) {
	target := "https://example.com/search?q=a b;still-an-argument"
	commands := browserCommands("linux", `missing:'/opt/Remote Browser/bin/open' --new-tab=%s:remote`, target)
	want := [][]string{
		{"missing", target},
		{"/opt/Remote Browser/bin/open", "--new-tab=" + target},
		{"remote", target},
		{"xdg-open", target},
	}
	if len(commands) != len(want) {
		t.Fatalf("commands = %d, want %d", len(commands), len(want))
	}
	for i := range commands {
		if !slices.Equal(commands[i].Args, want[i]) {
			t.Errorf("command %d = %q, want %q", i, commands[i].Args, want[i])
		}
	}
}

func TestOpenBrowserTriesCandidatesBeforeXDGOpen(t *testing.T) {
	var attempted []string
	err := openBrowserWith("linux", "missing:remote", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "remote" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "remote"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserFallsBackToXDGOpen(t *testing.T) {
	var attempted []string
	err := openBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "xdg-open"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserReportsAllFailures(t *testing.T) {
	missingErr := errors.New("missing")
	xdgErr := errors.New("xdg failed")
	var attempted []string
	err := openBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return xdgErr
		}
		return missingErr
	})
	if !errors.Is(err, missingErr) || !errors.Is(err, xdgErr) {
		t.Fatalf("openBrowserWith() error = %v, want both failures", err)
	}
	if want := []string{"missing", "xdg-open"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserContinuesAfterRuntimeFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses Unix true and false commands")
	}
	var attempted []string
	err := openBrowserWith("linux", "false:true", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("openBrowserWith() error = %v", err)
	}
	if want := []string{"false", "true"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
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
		openBrowser = defaultOpenBrowser
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
		openBrowser = defaultOpenBrowser
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
		t.Fatalf("openLinkCmd returned %T, want linkOpenErrMsg", msg)
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
