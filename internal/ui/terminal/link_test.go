package terminal

import (
	"errors"
	"os/exec"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestBrowserCommandsHonorBrowserCandidates(t *testing.T) {
	target := "https://example.com/search?q=a b;still-an-argument"
	commands := BrowserCommands("linux", `missing:'/opt/Remote Browser/bin/open' --new-tab=%s:remote`, target)
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
	err := OpenBrowserWith("linux", "missing:remote", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "remote" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("OpenBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "remote"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserFallsBackToXDGOpen(t *testing.T) {
	var attempted []string
	err := OpenBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return nil
		}
		return errors.New("not found")
	})
	if err != nil {
		t.Fatalf("OpenBrowserWith() error = %v", err)
	}
	if want := []string{"missing", "xdg-open"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

func TestOpenBrowserReportsAllFailures(t *testing.T) {
	missingErr := errors.New("missing")
	xdgErr := errors.New("xdg failed")
	var attempted []string
	err := OpenBrowserWith("linux", "missing", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		if cmd.Args[0] == "xdg-open" {
			return xdgErr
		}
		return missingErr
	})
	if !errors.Is(err, missingErr) || !errors.Is(err, xdgErr) {
		t.Fatalf("OpenBrowserWith() error = %v, want both failures", err)
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
	err := OpenBrowserWith("linux", "false:true", "https://example.com", func(cmd *exec.Cmd) error {
		attempted = append(attempted, cmd.Args[0])
		return cmd.Run()
	})
	if err != nil {
		t.Fatalf("OpenBrowserWith() error = %v", err)
	}
	if want := []string{"false", "true"}; !slices.Equal(attempted, want) {
		t.Fatalf("attempted %q, want %q", attempted, want)
	}
}

// Over SSH the opener would start a browser on the remote host, out of the
// user's sight, so the link goes to the page instead.
func TestOpenLinkOverSSHShowsThePage(t *testing.T) {
	seams := Seams{
		Open:   func(string) error { t.Fatal("the remote host opened a browser"); return nil },
		Copy:   func(string) error { return nil },
		Remote: func() bool { return true },
	}
	target := "https://example.com/issues/new?template=bug_report.yml"
	msg := OpenLink(target, seams)()
	if msg != (LinkPageMsg{URL: target}) {
		t.Fatalf("OpenLink() = %#v, want the page for %q", msg, target)
	}
}

func TestOpenLinkCmdOverSSHShowsThePage(t *testing.T) {
	seams := Seams{
		Opener: func(string) error { t.Fatal("the remote host opened a browser"); return nil },
		Remote: func() bool { return true },
	}
	msg := OpenLinkCmd("https://example.com/docs", seams)()
	if msg != (LinkPageMsg{URL: "https://example.com/docs"}) {
		t.Fatalf("OpenLinkCmd() = %#v, want the page", msg)
	}
}

func TestOpenLinkCmdFailureDropsTheURL(t *testing.T) {
	seams := Seams{
		Opener: func(string) error { return errors.New("no opener") },
		Remote: func() bool { return false },
	}
	msg := OpenLinkCmd("https://example.com/docs?token=secret", seams)()
	failure, ok := msg.(LinkOpenErrMsg)
	if !ok {
		t.Fatalf("OpenLinkCmd returned %T, want LinkOpenErrMsg", msg)
	}
	if !strings.Contains(failure.Err.Error(), "no opener") {
		t.Fatalf("the opener's reason was dropped: %v", failure.Err)
	}
	if strings.Contains(failure.Err.Error(), "token=secret") {
		t.Fatalf("the error repeats the URL: %v", failure.Err)
	}
}
