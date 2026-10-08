package remote

import (
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/remote/remotetest"
	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

type exitStatus int

func (e exitStatus) Error() string { return "exit status " + strconv.Itoa(int(e)) }
func (e exitStatus) ExitCode() int { return int(e) }

func newTestClient(t *testing.T, run Runner, opts ...Option) *Client {
	t.Helper()
	if run == nil {
		run = func(context.Context, []string) ([]byte, []byte, error) { return nil, nil, nil }
	}
	marked := func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		stdout, stderr, err := run(ctx, argv)
		return remotetest.Answer(stdout), stderr, err
	}
	return newRawTestClient(t, marked, opts...)
}

// newRawTestClient hands run's stdout over as the host printed it, without
// the marker the login shell prints before agent-manager runs.
func newRawTestClient(t *testing.T, run Runner, opts ...Option) *Client {
	t.Helper()
	c := New(t.TempDir(), append([]Option{WithRunner(run)}, opts...)...)
	t.Cleanup(func() { os.RemoveAll(c.controlDir) })
	c.SetConnections([]Connection{{Name: "gpu", Destination: "me@gpu"}, {Name: "cpu", Destination: "me@cpu"}})
	return c
}

// remoteWords undoes the remote command at the end of argv. The login-shell
// round trip proves a real shell reads it the same way.
func remoteWords(t *testing.T, argv []string) []string {
	t.Helper()
	words, err := remotetest.Words(argv)
	if err != nil {
		t.Fatal(err)
	}
	return words
}

func TestCallsToOneHostRunOneAtATime(t *testing.T) {
	entered := make(chan string, 4)
	release := make(chan struct{})
	c := newTestClient(t, func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		entered <- argv[len(argv)-2]
		<-release
		return []byte(`{"id":"a1"}`), nil, nil
	})
	var wg sync.WaitGroup
	call := func(host string) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Kill(context.Background(), Ref{Host: host, ID: "a1"}); err != nil {
				t.Error(err)
			}
		}()
	}
	call("gpu")
	if got := <-entered; got != "me@gpu" {
		t.Fatalf("first call went to %s", got)
	}
	call("gpu")
	call("cpu")
	if got := <-entered; got != "me@cpu" {
		t.Fatalf("%s ran while gpu's first call held its host", got)
	}
	select {
	case got := <-entered:
		t.Fatalf("second call to %s ran before the first returned", got)
	case <-time.After(50 * time.Millisecond):
	}
	release <- struct{}{}
	release <- struct{}{}
	if got := <-entered; got != "me@gpu" {
		t.Fatalf("queued call went to %s", got)
	}
	release <- struct{}{}
	wg.Wait()
}

func TestWaitingForAHostHonorsTheContext(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		close(entered)
		<-release
		return []byte(`{}`), nil, nil
	})
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Kill(context.Background(), Ref{Host: "gpu", ID: "a1"})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Kill(ctx, Ref{Host: "gpu", ID: "a1"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("queued call returned %v, want context.Canceled", err)
	}
	close(release)
	<-done
}

func TestCallsShareAGlobalCap(t *testing.T) {
	entered := make(chan struct{}, 8)
	release := make(chan struct{})
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		entered <- struct{}{}
		<-release
		return []byte(`{}`), nil, nil
	})
	var hosts []Connection
	for i := range maxInFlight + 1 {
		hosts = append(hosts, Connection{Name: "h" + strconv.Itoa(i), Destination: "h" + strconv.Itoa(i)})
	}
	c.SetConnections(hosts)
	var wg sync.WaitGroup
	for _, host := range hosts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Kill(context.Background(), Ref{Host: host.Name, ID: "a1"})
		}()
	}
	for range maxInFlight {
		<-entered
	}
	select {
	case <-entered:
		t.Fatalf("more than %d calls ran at once", maxInFlight)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-entered
	wg.Wait()
}

func TestErrors(t *testing.T) {
	cases := []struct {
		name        string
		stderr      string
		err         error
		want        string
		unreachable bool
	}{
		{"ssh failed", "Warning: Permanently added 'gpu'\nssh: connect to host gpu port 22: Connection refused\n", exitStatus(255),
			"gpu: unreachable over SSH: ssh: connect to host gpu port 22: Connection refused", true},
		{"ssh failed silently", "", exitStatus(255), "gpu: unreachable over SSH: exit status 255", true},
		{"binary missing", "sh: agent-manager: not found\n", exitStatus(127),
			"gpu: agent-manager is not on the login shell's PATH there; install it or add it to PATH in the login profile", false},
		{"remote refused", "agent-manager: session a1 is not running\n", exitStatus(1), "gpu: session a1 is not running", false},
		{"remote silent", "", exitStatus(1), "gpu: exit status 1", false},
		{"ssh not installed", "", errors.New(`exec: "ssh": executable file not found in $PATH`), `gpu: exec: "ssh": executable file not found in $PATH`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
				return nil, []byte(tc.stderr), tc.err
			})
			_, err := c.Kill(context.Background(), Ref{Host: "gpu", ID: "a1"})
			if err == nil || err.Error() != tc.want {
				t.Fatalf("error = %v, want %s", err, tc.want)
			}
			if Unreachable(err) != tc.unreachable {
				t.Fatalf("Unreachable = %v, want %v", !tc.unreachable, tc.unreachable)
			}
			var remote *Error
			if !errors.As(err, &remote) || remote.Host != "gpu" {
				t.Fatalf("error %v does not name its host", err)
			}
		})
	}
}

func TestDeadlineIsUnreachable(t *testing.T) {
	c := newTestClient(t, func(ctx context.Context, _ []string) ([]byte, []byte, error) {
		<-ctx.Done()
		return nil, nil, errors.New("signal: killed")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := c.Kill(ctx, Ref{Host: "gpu", ID: "a1"})
	if !Unreachable(err) || err.Error() != "gpu: did not answer in time: context deadline exceeded" {
		t.Fatalf("error = %v, unreachable %v", err, Unreachable(err))
	}
}

func TestEveryCallHasADeadline(t *testing.T) {
	deadlines := map[string]time.Duration{}
	c := newTestClient(t, func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok {
			t.Fatalf("%q runs without a deadline", argv)
		}
		deadlines[remoteWords(t, argv)[1]] = time.Until(deadline)
		return []byte(`{}`), nil, nil
	})
	ctx := context.Background()
	c.Kill(ctx, Ref{Host: "gpu", ID: "a1"})
	c.Spawn(ctx, "gpu", sessioncmd.CreateSessionOptions{})
	if d := deadlines["kill"]; d <= 14*time.Second || d > callTimeout {
		t.Fatalf("kill deadline %v, want %v", d, callTimeout)
	}
	if d := deadlines["spawn"]; d <= 59*time.Second || d > createTimeout {
		t.Fatalf("spawn deadline %v, want %v", d, createTimeout)
	}
}

func TestUnknownConnection(t *testing.T) {
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		t.Fatal("a call to an unknown connection ran ssh")
		return nil, nil, nil
	})
	_, err := c.Snapshot(context.Background(), "nowhere")
	if err == nil || err.Error() != "nowhere: no connection has that name" {
		t.Fatalf("error = %v", err)
	}
}

func TestConnectionsAreCopied(t *testing.T) {
	c := newTestClient(t, nil)
	listed := c.Connections()
	listed[0].Destination = "elsewhere"
	if got := c.Connections()[0].Destination; got != "me@gpu" {
		t.Fatalf("Connections shares its slice: destination became %s", got)
	}
}
