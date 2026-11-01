package terminal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
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

// A file link is revealed, never opened, so a click cannot run what an agent
// linked to; a web link still goes to the platform's URL handler.
func TestOpenerCommandRevealsFiles(t *testing.T) {
	type run struct {
		goos, target string
		want         []string
	}
	cases := []run{
		{"darwin", "file:///tmp/notes.md", []string{"open", "-R", "/tmp/notes.md"}},
		{"darwin", "file:///Applications/Tool.app", []string{"open", "-R", "/Applications/Tool.app"}},
		{"windows", "file:///C:/Temp/notes.md", []string{"explorer", `/select,C:\Temp\notes.md`}},
		{"darwin", "https://example.com/a", []string{"open", "https://example.com/a"}},
		{"linux", "https://example.com/a", []string{"xdg-open", "https://example.com/a"}},
		{"windows", "https://example.com/a", []string{"rundll32", "url.dll,FileProtocolHandler", "https://example.com/a"}},
	}
	refused := []run{
		{goos: "windows", target: "file://///attacker/share/x"},
		{goos: "windows", target: "file:////attacker/share/x"},
		{goos: "windows", target: "file:///relative"},
		{goos: "windows", target: "file:///C:/a,/root,C:/b"},
		{goos: "windows", target: `file:///C:/a"b`},
		{goos: "darwin", target: "file://attacker/share/x"},
		{goos: "linux", target: "file:////attacker/x"},
		{goos: "linux", target: "file:///no/such/folder/x"},
	}
	// xdg-open gets a folder only once it is one here, which a Windows host,
	// naming its folders with a drive letter, cannot stand in for.
	if runtime.GOOS != "windows" {
		folder := t.TempDir()
		script := filepath.Join(folder, "run.sh")
		if err := os.WriteFile(script, []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		cases = append(cases,
			run{"linux", "file://" + script, []string{"xdg-open", folder}},
			run{"linux", "file://" + script + "/.", []string{"xdg-open", folder}},
		)
		refused = append(refused, run{goos: "linux", target: "file://" + script + "/inside"})
	}

	for _, c := range cases {
		cmd, err := OpenerCommand(c.goos, c.target)
		if err != nil {
			t.Errorf("%s %s: %v", c.goos, c.target, err)
			continue
		}
		if !slices.Equal(cmd.Args, c.want) {
			t.Errorf("%s %s runs %q, want %q", c.goos, c.target, cmd.Args, c.want)
		}
	}
	for _, c := range refused {
		if cmd, err := OpenerCommand(c.goos, c.target); err == nil {
			t.Errorf("%s %s runs %q, want it refused", c.goos, c.target, cmd.Args)
		}
	}
}
