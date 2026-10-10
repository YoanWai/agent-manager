package ui

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	tea "github.com/charmbracelet/bubbletea"
)

// focusRequestMsg carries one request from the focus endpoint into Update,
// with the channel the outcome goes back on. A request handed over after
// expires is dropped: its caller has already been told it timed out.
type focusRequestMsg struct {
	request sessioncmd.FocusRequest
	answer  chan<- error
	expires time.Time
}

// listenFocus serves focus requests on path until the returned stop runs.
// Each connection carries one request and gets the outcome back on itself,
// so no request can be mistaken for another. A manager already answering on
// path keeps it; a socket left by one that exited is replaced.
func listenFocus(path string, send func(tea.Msg)) (stop func()) {
	if conn, err := net.Dial("unix", path); err == nil {
		conn.Close()
		return func() {}
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return func() {}
	}
	listener, err := net.Listen("unix", path)
	if err != nil {
		return func() {}
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go answerFocus(conn, send)
		}
	}()
	// Closing a listener Go created also unlinks its socket.
	return func() { listener.Close() }
}

func answerFocus(conn net.Conn, send func(tea.Msg)) {
	defer conn.Close()
	expires := time.Now().Add(sessioncmd.FocusAnswerWait)
	if err := conn.SetDeadline(expires.Add(time.Second)); err != nil {
		return
	}
	var request sessioncmd.FocusRequest
	if err := json.NewDecoder(conn).Decode(&request); err != nil {
		return
	}
	answer := make(chan error, 1)
	// The program stops taking messages while it is suspended in a full
	// screen attach, so the hand-over must not hold up the reply.
	go send(focusRequestMsg{request: request, answer: answer, expires: expires})
	var reply sessioncmd.FocusAnswer
	select {
	case err := <-answer:
		if err != nil {
			reply.Error = err.Error()
		}
	case <-time.After(time.Until(expires)):
		reply.Error = "Agent Manager did not act on the request in time; it does while a session is attached full screen only once you detach, and then drops it"
	}
	_ = json.NewEncoder(conn).Encode(reply)
}
