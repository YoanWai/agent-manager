package ui

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	tea "github.com/charmbracelet/bubbletea"
)

// A Unix socket path stops near 104 bytes, past which t.TempDir's names run.
func focusSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "amfocus")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return filepath.Join(dir, "agentmgr.focus")
}

func askFocus(t *testing.T, path string, request sessioncmd.FocusRequest) <-chan sessioncmd.FocusAnswer {
	t.Helper()
	answers := make(chan sessioncmd.FocusAnswer, 1)
	go func() {
		var answer sessioncmd.FocusAnswer
		conn, err := net.Dial("unix", path)
		if err != nil {
			answer.Error = "dial: " + err.Error()
			answers <- answer
			return
		}
		defer conn.Close()
		if err := json.NewEncoder(conn).Encode(request); err != nil {
			answer.Error = "send: " + err.Error()
		} else if err := json.NewDecoder(conn).Decode(&answer); err != nil {
			answer.Error = "read: " + err.Error()
		}
		answers <- answer
	}()
	return answers
}

// Each connection carries one request and reads back what the model did
// with that request, including a refusal.
func TestFocusEndpointAnswersEachRequestWithItsOutcome(t *testing.T) {
	m, deep := focusRequestModel(t)
	path := focusSocketPath(t)
	msgs := make(chan tea.Msg, 1)
	stop := listenFocus(path, func(msg tea.Msg) { msgs <- msg })
	defer stop()

	serve := func(request sessioncmd.FocusRequest) sessioncmd.FocusAnswer {
		answers := askFocus(t, path, request)
		updated, _ := m.Update(<-msgs)
		m = updated.(*Model)
		return <-answers
	}
	if answer := serve(sessioncmd.FocusRequest{ID: deep.ID, Enter: true}); answer.Error != "" {
		t.Fatalf("focus answered %q", answer.Error)
	}
	if sess, ok := m.selected(); !ok || sess.ID != deep.ID || m.mode != modeFocus {
		t.Fatalf("cursor %+v mode %v, want deep focused", sess, m.mode)
	}
	if answer := serve(sessioncmd.FocusRequest{ID: "sess-long-gone"}); answer.Error == "" {
		t.Fatal("a request for a session with no row was answered as done")
	}
}

// One manager answers per tmux server: a second one leaves a live endpoint
// to its owner, and a socket left by a manager that exited is taken over.
func TestFocusEndpointKeepsALiveOwnerAndReplacesAStaleSocket(t *testing.T) {
	path := focusSocketPath(t)
	first := make(chan tea.Msg, 1)
	stopFirst := listenFocus(path, func(msg tea.Msg) { first <- msg })
	second := make(chan tea.Msg, 1)
	stopSecond := listenFocus(path, func(msg tea.Msg) { second <- msg })
	defer stopSecond()
	askFocus(t, path, sessioncmd.FocusRequest{ID: "a"})
	request := (<-first).(focusRequestMsg)
	request.answer <- nil
	if len(second) != 0 {
		t.Fatal("the second manager took a request from the live endpoint")
	}
	stopFirst()

	stale, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	stale.(*net.UnixListener).SetUnlinkOnClose(false)
	stale.Close()
	third := make(chan tea.Msg, 1)
	stopThird := listenFocus(path, func(msg tea.Msg) { third <- msg })
	defer stopThird()
	askFocus(t, path, sessioncmd.FocusRequest{ID: "b"})
	if request := (<-third).(focusRequestMsg); request.request.ID != "b" {
		t.Fatalf("request = %+v, want b", request.request)
	}
}
