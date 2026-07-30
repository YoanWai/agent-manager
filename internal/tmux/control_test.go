package tmux

import (
	"io"
	"strings"
	"testing"
	"time"
)

// fakeServer feeds scripted control-mode output to a Control and records
// what the client writes, standing in for a tmux server.
type fakeServer struct {
	control *Control
	writes  *strings.Builder
	feed    io.WriteCloser
}

func newFakeServer() *fakeServer {
	stdoutRead, stdoutWrite := io.Pipe()
	writes := &strings.Builder{}
	return &fakeServer{
		control: newControl(nopWriteCloser{writes}, stdoutRead),
		writes:  writes,
		feed:    stdoutWrite,
	}
}

type nopWriteCloser struct{ io.Writer }

func (nopWriteCloser) Close() error { return nil }

func (f *fakeServer) send(lines ...string) {
	io.WriteString(f.feed, strings.Join(lines, "\n")+"\n")
}

// waitWritten blocks until the client's stdin contains want, so a scripted
// reply cannot outrun the waiter it is meant to resolve.
func waitWritten(t *testing.T, server *fakeServer, want string) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for !strings.Contains(server.writes.String(), want) {
		select {
		case <-deadline:
			t.Fatalf("command %q never written to stdin", want)
		default:
			time.Sleep(time.Millisecond)
		}
	}
}

func TestControlCommandRoundTrip(t *testing.T) {
	server := newFakeServer()
	// tmux greets every control client with an unsolicited empty block.
	server.send("%begin 1 0 0", "%end 1 0 0")

	type result struct {
		text string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		text, err := server.control.Command("capture-pane -p")
		got <- result{text, err}
	}()

	deadline := time.After(2 * time.Second)
	for server.writes.Len() == 0 {
		select {
		case <-deadline:
			t.Fatal("command never written to stdin")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if want := "capture-pane -p\n"; server.writes.String() != want {
		t.Fatalf("stdin = %q, want %q", server.writes.String(), want)
	}

	server.send("%begin 2 1 0", "line one", "line two", "%end 2 1 0")
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("Command: %v", r.err)
		}
		if r.text != "line one\nline two" {
			t.Fatalf("reply = %q", r.text)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reply never resolved")
	}
}

// Pane text is echoed raw inside reply blocks, so a pane that happens to
// display the control protocol (a diff of this file, tmux docs) must not
// terminate the block early: only the %end carrying the block's own tag
// does. A mismatched %end taken as a terminator shifts every later reply
// onto the wrong caller for the life of the client.
func TestControlBlockSurvivesProtocolLookalikes(t *testing.T) {
	server := newFakeServer()
	server.send("%begin 1 0 0", "%end 1 0 0")

	type result struct {
		text string
		err  error
	}
	got := make(chan result, 1)
	go func() {
		text, err := server.control.Command("capture-pane -p")
		got <- result{text, err}
	}()
	waitWritten(t, server, "capture-pane -p\n")
	server.send(
		"%begin 2 1 0",
		"%end 123 456 0",
		"%error 123 456 0",
		"%begin 999 999 0",
		"%output fake",
		"after",
		"%end 2 1 0",
	)
	select {
	case r := <-got:
		if r.err != nil {
			t.Fatalf("Command: %v", r.err)
		}
		want := "%end 123 456 0\n%error 123 456 0\n%begin 999 999 0\n%output fake\nafter"
		if r.text != want {
			t.Fatalf("reply = %q, want %q", r.text, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reply never resolved")
	}
	// The lookalike %output line must not have signalled an event.
	select {
	case <-server.control.Events():
		t.Fatal("pane text posing as an output notification raised an event")
	default:
	}

	// The queue stayed aligned: a second command gets the second reply.
	got2 := make(chan result, 1)
	go func() {
		text, err := server.control.Command("display-message -p x")
		got2 <- result{text, err}
	}()
	waitWritten(t, server, "display-message -p x\n")
	server.send("%begin 3 2 0", "second", "%end 3 2 0")
	select {
	case r := <-got2:
		if r.err != nil || r.text != "second" {
			t.Fatalf("second reply = %q, %v", r.text, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second reply never resolved")
	}
}

// A server that stops answering must cost the caller a bounded wait, not
// hang it: Command runs on the UI loop.
func TestControlCommandTimesOut(t *testing.T) {
	server := newFakeServer()
	server.send("%begin 1 0 0", "%end 1 0 0")
	start := time.Now()
	_, err := server.control.Command("capture-pane -p")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v, want timeout", err)
	}
	if waited := time.Since(start); waited > commandTimeout+time.Second {
		t.Fatalf("timeout took %v", waited)
	}
}

func TestControlErrorBlock(t *testing.T) {
	server := newFakeServer()
	server.send("%begin 1 0 0", "%end 1 0 0")

	got := make(chan error, 1)
	go func() {
		_, err := server.control.Command("bogus-command")
		got <- err
	}()
	server.send("%begin 2 1 0", "unknown command: bogus-command", "%error 2 1 0")
	select {
	case err := <-got:
		if err == nil || !strings.Contains(err.Error(), "unknown command") {
			t.Fatalf("err = %v, want unknown command", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("error reply never resolved")
	}
}

func TestControlOutputEvent(t *testing.T) {
	server := newFakeServer()
	server.send("%begin 1 0 0", "%end 1 0 0")
	server.send(`%output %0 hello\015\012`)
	select {
	case <-server.control.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event for output notification")
	}
}

func TestControlEventsCoalesce(t *testing.T) {
	server := newFakeServer()
	server.send("%output %0 a", "%output %0 b", "%output %0 c")
	select {
	case <-server.control.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event")
	}
	// The burst above may legally coalesce to one pending signal; after
	// draining, a fresh notification must signal again.
	for {
		select {
		case <-server.control.Events():
			continue
		default:
		}
		break
	}
	server.send("%output %0 d")
	select {
	case <-server.control.Events():
	case <-time.After(2 * time.Second):
		t.Fatal("no event after drain")
	}
}

func TestControlServerExitWakesWaiters(t *testing.T) {
	server := newFakeServer()
	got := make(chan error, 1)
	go func() {
		_, err := server.control.Command("capture-pane -p")
		got <- err
	}()
	time.Sleep(10 * time.Millisecond)
	server.feed.Close()
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("waiter resolved without error after exit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waiter hung after server exit")
	}
	select {
	case <-server.control.Done():
	default:
		t.Fatal("Done not closed after exit")
	}
	if _, err := server.control.Command("anything"); err == nil {
		t.Fatal("Command after exit must fail")
	}
}

func TestOutputData(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain`, "plain"},
		{`a\015\012b`, "a\r\nb"},
		{`\033[31mred`, "\x1b[31mred"},
		{`trailing\`, `trailing\`},
		{`short\07`, `short\07`},
		{`not\999octal`, `not\999octal`},
	}
	for _, c := range cases {
		if got := OutputData(c.in); got != c.want {
			t.Errorf("OutputData(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Integration: a real control client against the isolated test server.
// Proves the fork-free capture path and measures paint-to-event latency.
func TestControlLiveCaptureAndEvents(t *testing.T) {
	driver := requireTmux(t)
	id := "ctl" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 80, 24); err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() { driver.Kill(id) })

	control, err := driver.OpenControl(id)
	if err != nil {
		t.Fatalf("OpenControl: %v", err)
	}
	t.Cleanup(func() { control.Close() })

	// Command channel works: capture over the pipe, no fork.
	if _, err := control.Command("capture-pane -p -e -t " + "am_" + id); err != nil {
		t.Fatalf("capture over control pipe: %v", err)
	}

	// Pane output pushes an event.
	for {
		select {
		case <-control.Events():
			continue
		default:
		}
		break
	}
	start := time.Now()
	if err := driver.SendText(id, "echo control-mode-ping"); err != nil {
		t.Fatalf("SendText: %v", err)
	}
	select {
	case <-control.Events():
		t.Logf("paint-to-event latency: %v", time.Since(start))
	case <-time.After(5 * time.Second):
		t.Fatal("no output event after pane wrote")
	}

	// The event-then-capture loop sees the new content.
	deadline := time.Now().Add(5 * time.Second)
	for {
		text, err := control.Command("capture-pane -p -t " + "am_" + id)
		if err != nil {
			t.Fatalf("capture: %v", err)
		}
		if strings.Contains(text, "control-mode-ping") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("capture never showed pane text: %q", text)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Benchmarks the decision that motivates control mode: capture over the
// persistent pipe versus one exec fork per capture.
func BenchmarkCaptureControlPipe(b *testing.B) {
	driver, control, id := benchControl(b)
	defer control.Close()
	defer driver.Kill(id)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := control.Command("capture-pane -p -e -t am_" + id); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCaptureExecFork(b *testing.B) {
	driver, control, id := benchControl(b)
	control.Close()
	defer driver.Kill(id)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := driver.CapturePane(id); err != nil {
			b.Fatal(err)
		}
	}
}

func benchControl(b *testing.B) (*Driver, *Control, string) {
	b.Helper()
	driver, err := NewWithSocket(testSocket)
	if err != nil {
		b.Fatal(err)
	}
	id := "bench" + strings.ReplaceAll(time.Now().Format("150405.000000"), ".", "")
	if err := driver.Create(id, "/tmp", "", nil, 200, 50); err != nil {
		b.Fatal(err)
	}
	control, err := driver.OpenControl(id)
	if err != nil {
		b.Fatal(err)
	}
	return driver, control, id
}
