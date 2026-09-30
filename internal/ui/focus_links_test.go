package ui

import (
	"errors"
	"strings"
	"testing"
)

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
