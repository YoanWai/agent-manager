package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

type fakeBrowsers struct {
	callerID  string
	browserID string
	closedID  string
	command   string
	opts      sessioncmd.CreateBrowserOptions
	failWith  error
}

func (f *fakeBrowsers) List(sessionID string) ([]sessioncmd.Browser, error) {
	f.callerID = sessionID
	return []sessioncmd.Browser{{ID: "b1", Name: "docs", Directory: "/repo", Status: "idle", Running: true, URL: "https://example.com"}}, f.failWith
}

func (f *fakeBrowsers) Create(sessionID string, opts sessioncmd.CreateBrowserOptions) (sessioncmd.Browser, error) {
	f.callerID, f.opts = sessionID, opts
	return sessioncmd.Browser{ID: "b2", Name: "browser-b2", Directory: "/repo", URL: opts.URL}, f.failWith
}

func (f *fakeBrowsers) Command(sessionID, browserID, command string) (sessioncmd.BrowserOutput, error) {
	f.callerID, f.browserID, f.command = sessionID, browserID, command
	return sessioncmd.BrowserOutput{BrowserID: browserID, Command: command, Output: "- button \"Sign in\" [ref=e14]"}, f.failWith
}

func (f *fakeBrowsers) Close(sessionID, browserID string) error {
	f.callerID, f.closedID = sessionID, browserID
	return f.failWith
}

func TestBrowserVerbsDispatch(t *testing.T) {
	fake := &fakeBrowsers{}
	listed := &bytes.Buffer{}
	if err := runBrowserList(listed, fake, nil, "cafe0001"); err != nil {
		t.Fatalf("browser list: %v", err)
	}
	if !strings.Contains(listed.String(), "- docs (id b1) in root at /repo; status=idle; running=true; url=https://example.com") {
		t.Fatalf("browser list output = %q", listed.String())
	}

	created := &bytes.Buffer{}
	if err := runBrowserCreate(created, fake, []string{"--url", "https://example.com", "--directory", "/repo"}, "cafe0001"); err != nil {
		t.Fatalf("browser create: %v", err)
	}
	if fake.opts.URL != "https://example.com" || fake.opts.Directory != "/repo" || fake.opts.Group != nil || fake.opts.Nest != nil {
		t.Fatalf("browser create opts = %+v", fake.opts)
	}
	if created.String() != "created browser-b2 (id b2) in root at /repo showing https://example.com\n" {
		t.Fatalf("browser create output = %q", created.String())
	}

	if err := runBrowserCreate(&bytes.Buffer{}, fake, []string{"--group", "", "--nest=false"}, "cafe0001"); err != nil {
		t.Fatalf("browser create --group --nest: %v", err)
	}
	if fake.opts.Group == nil || *fake.opts.Group != "" || fake.opts.Nest == nil || *fake.opts.Nest {
		t.Fatalf("typed --group and --nest=false reached the layer as %+v", fake.opts)
	}

	commanded := &bytes.Buffer{}
	if err := runBrowserCommand(commanded, fake, []string{"b1", "snapshot -i"}, "cafe0001"); err != nil {
		t.Fatalf("browser command: %v", err)
	}
	if fake.callerID != "cafe0001" || fake.browserID != "b1" || fake.command != "snapshot -i" {
		t.Fatalf("browser command reached the layer as caller %q, browser %q, command %q", fake.callerID, fake.browserID, fake.command)
	}
	if commanded.String() != "- button \"Sign in\" [ref=e14]\n" {
		t.Fatalf("browser command output = %q", commanded.String())
	}

	for _, operands := range [][]string{{"b1"}, {"b1", "click", "@e14"}} {
		unsplit := &fakeBrowsers{}
		err := runBrowserCommand(&bytes.Buffer{}, unsplit, operands, "cafe0001")
		if err == nil || !strings.HasPrefix(err.Error(), "usage: agent-manager browser command") {
			t.Fatalf("browser command %q = %v, want the usage line", operands, err)
		}
		if unsplit.callerID != "" {
			t.Fatalf("browser command %q reached the layer", operands)
		}
	}

	closed := &bytes.Buffer{}
	if err := runBrowserClose(closed, fake, []string{"b1"}, "cafe0001"); err != nil {
		t.Fatalf("browser close: %v", err)
	}
	if fake.closedID != "b1" || closed.String() != "closed browser b1\n" {
		t.Fatalf("browser close got %q, printed %q", fake.closedID, closed.String())
	}

	if err := dispatch(&bytes.Buffer{}, "browser", browserVerbs(), []string{"open"}, func() string { return "cafe0001" }, t.TempDir()); err == nil {
		t.Fatal("an unknown browser verb should not dispatch")
	}
}

func TestMissingBrowserIDIsAUsageError(t *testing.T) {
	for name, run := range map[string]func(*bytes.Buffer, *fakeBrowsers, []string) error{
		"command": func(out *bytes.Buffer, f *fakeBrowsers, args []string) error {
			return runBrowserCommand(out, f, args, "cafe0001")
		},
		"close": func(out *bytes.Buffer, f *fakeBrowsers, args []string) error {
			return runBrowserClose(out, f, args, "cafe0001")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeBrowsers{}
			err := run(&bytes.Buffer{}, fake, nil)
			if err == nil {
				t.Fatal("a command with no browser id should not reach the layer")
			}
			if !strings.HasPrefix(err.Error(), "usage: agent-manager browser "+name) {
				t.Fatalf("error = %q, want the usage line", err)
			}
			if fake.callerID != "" {
				t.Fatal("the layer was called despite the missing id")
			}
		})
	}
}
