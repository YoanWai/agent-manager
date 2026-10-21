package remote

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
)

type memoryOpeners struct {
	openers map[Ref]string
	failSet bool
}

func (m *memoryOpeners) SetRemoteTerminalOpener(host, terminalID, sessionID string) error {
	if m.failSet {
		return errors.New("disk I/O error")
	}
	m.openers[Ref{Host: host, ID: terminalID}] = sessionID
	return nil
}

func (m *memoryOpeners) RemoteTerminalOpener(host, terminalID string) (string, error) {
	return m.openers[Ref{Host: host, ID: terminalID}], nil
}

func (m *memoryOpeners) DeleteRemoteTerminal(host, terminalID string) error {
	delete(m.openers, Ref{Host: host, ID: terminalID})
	return nil
}

// terminalHost answers terminal create with t1 and keeps the commands it
// ran.
func terminalHost(t *testing.T) (*Client, *[]string) {
	t.Helper()
	var ran []string
	c := newTestClient(t, func(_ context.Context, argv []string) ([]byte, []byte, error) {
		words := remoteWords(t, argv)[1:]
		ran = append(ran, strings.Join(words[:2], " "))
		switch words[1] {
		case "create":
			return []byte(`{"id":"t1","name":"shell"}`), nil, nil
		case "send":
			return []byte(`{"terminal_id":"t1"}`), nil, nil
		}
		return []byte("closed terminal t1\n"), nil, nil
	})
	return c, &ran
}

func TestASessionDrivesOnlyTheTerminalsItOpened(t *testing.T) {
	ctx := context.Background()
	c, ran := terminalHost(t)
	openers := &memoryOpeners{openers: map[Ref]string{}}
	terminals := c.Terminals(openers)
	if _, err := terminals.Open(ctx, "gpu", "cafe", sessioncmd.CreateTerminalOptions{}); err != nil {
		t.Fatal(err)
	}
	ref := Ref{Host: "gpu", ID: "t1"}
	for _, other := range []string{"beef", ""} {
		if _, err := terminals.Send(ctx, ref, other, "ls", nil); err == nil || !strings.Contains(err.Error(), "was not opened by this session") {
			t.Fatalf("send as %q = %v, want refused", other, err)
		}
		if err := terminals.Close(ctx, ref, other); err == nil {
			t.Fatalf("close as %q was not refused", other)
		}
	}
	if _, err := terminals.Send(ctx, ref, "cafe", "ls", nil); err != nil {
		t.Fatal(err)
	}
	if err := terminals.Close(ctx, ref, "cafe"); err != nil {
		t.Fatal(err)
	}
	if _, kept := openers.openers[ref]; kept {
		t.Fatal("closing kept the opener")
	}
	if want := []string{"terminal create", "terminal send", "terminal close"}; !slices.Equal(*ran, want) {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
}

// The user closes any terminal from its row, and the opener goes with it.
func TestTheUserClosesAnyTerminal(t *testing.T) {
	c, ran := terminalHost(t)
	ref := Ref{Host: "gpu", ID: "t1"}
	openers := &memoryOpeners{openers: map[Ref]string{ref: "cafe"}}
	if err := c.Terminals(openers).CloseForUser(context.Background(), ref); err != nil {
		t.Fatal(err)
	}
	if len(openers.openers) != 0 || !slices.Equal(*ran, []string{"terminal close"}) {
		t.Fatalf("openers %v after running %q", openers.openers, *ran)
	}
}

func TestATerminalWhoseOpenerIsNotRecordedIsClosed(t *testing.T) {
	c, ran := terminalHost(t)
	openers := &memoryOpeners{openers: map[Ref]string{}, failSet: true}
	_, err := c.Terminals(openers).Open(context.Background(), "gpu", "cafe", sessioncmd.CreateTerminalOptions{})
	if err == nil || !strings.Contains(err.Error(), "so it was closed again: disk I/O error") {
		t.Fatalf("open = %v", err)
	}
	if want := []string{"terminal create", "terminal close"}; !slices.Equal(*ran, want) {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
}
