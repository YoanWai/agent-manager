package sessioncmd

import (
	"strings"
	"testing"
	"time"
)

func TestMessageSubjectIsWhatAWatcherReads(t *testing.T) {
	long := strings.Repeat("word ", 30)
	for _, tc := range []struct {
		name, subject, body, want string
	}{
		{"given subject wins", "review report, two findings", "Full report follows\nmore", "review report, two findings"},
		{"first line", "", "Rebase on main\nthen run the tests", "Rebase on main"},
		{"blank leading lines", "  ", "\n   \n\t\n  Rebase   on\tmain  \nrest", "Rebase on main"},
		{"long line cut", "", long, strings.TrimSpace(long)[:71] + "…"},
		{"multibyte counted by columns", "", strings.Repeat("界", 40), strings.Repeat("界", 35) + "…"},
		{"control characters removed", "", "re\x1b[31mbase\u009b on\r main", "re[31mbase on main"},
		{"empty body", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := MessageSubject(tc.subject, tc.body); got != tc.want {
				t.Fatalf("MessageSubject(%q, %q) = %q, want %q", tc.subject, tc.body, got, tc.want)
			}
		})
	}
}

func TestSendKeepsAValidSubjectAndRefusesABadOne(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	for _, tc := range []struct{ subject, want string }{
		{"two\nlines", "line break or a control character"},
		{"tab\there", "line break or a control character"},
		{strings.Repeat("s", maxSubjectBytes+1), "over the 100 byte limit"},
	} {
		if _, err := h.sessions.Send(h.caller.ID, worker.ID, tc.subject, "rebase on main"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("subject %q err = %v, want %q", tc.subject, err, tc.want)
		}
	}
	sent, err := h.sessions.Send(h.caller.ID, worker.ID, "  rebase  ", "Rebase on main and rerun the suite")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	read, err := h.sessions.ReadMessage("", sent.MessageID)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if read.Subject != "rebase" || read.Body != "Rebase on main and rerun the suite" {
		t.Fatalf("read = %+v", read)
	}
	if _, err := h.sessions.SendFrom("", "laptop-agent", worker.ID, "bad\x07", "hello"); err == nil {
		t.Fatal("SendFrom took a subject holding a control character")
	}
	fromAfar, err := h.sessions.SendFrom("", "laptop-agent", worker.ID, "from afar", "hello there")
	if err != nil {
		t.Fatalf("SendFrom: %v", err)
	}
	read, err = h.sessions.ReadMessage(h.caller.ID, fromAfar.MessageID)
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if read.Subject != "from afar" || read.FromSessionID != "" || read.FromName != "laptop-agent" {
		t.Fatalf("read from afar = %+v", read)
	}
}

// The log and message_status read a message's state through one switch, so a
// hold the watcher sees is the very sentence the sender is told.
func TestMessagesShareMessageStatusWording(t *testing.T) {
	h := newSessionHarness(t)
	worker, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Tool: "resting", Name: "worker"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	first, err := h.sessions.Send(h.caller.ID, worker.ID, "", "first task\nwith detail")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	second, err := h.sessions.Send(h.caller.ID, worker.ID, "second", "second task")
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if err := h.store.MarkDelivered(first.MessageID, time.Now()); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}
	if _, err := h.sessions.Archive(h.caller.ID, worker.ID, true); err != nil {
		t.Fatalf("Archive: %v", err)
	}

	status, err := h.sessions.MessageStatus(h.caller.ID, second.MessageID)
	if err != nil {
		t.Fatalf("MessageStatus: %v", err)
	}
	entries, err := h.sessions.Messages("", MessagesOptions{Session: worker.ID, Limit: DefaultMessagesLimit})
	if err != nil {
		t.Fatalf("Messages: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %+v", entries)
	}
	delivered, held := entries[0], entries[1]
	if delivered.MessageID != first.MessageID || delivered.State != "delivered" || delivered.DeliveredAt == "" ||
		delivered.Subject != "first task" || delivered.QueuePosition != 0 || delivered.Body != "" {
		t.Fatalf("delivered entry = %+v", delivered)
	}
	if held.State != "held" || held.State != status.State || held.Reason != status.Reason || held.QueuePosition != 1 {
		t.Fatalf("held entry = %+v, message_status = %+v", held, status)
	}
	if held.FromSessionID != h.caller.ID || held.FromName != h.caller.Name || held.ToSessionID != worker.ID || held.ToName != "worker" || held.Subject != "second" {
		t.Fatalf("held entry names = %+v", held)
	}
	if _, err := time.Parse(time.RFC3339, held.SentAt); err != nil {
		t.Fatalf("sent_at %q is not RFC3339: %v", held.SentAt, err)
	}

	queued, err := h.sessions.Messages(h.caller.ID, MessagesOptions{Session: worker.ID, QueuedOnly: true, Limit: DefaultMessagesLimit})
	if err != nil || len(queued) != 1 || queued[0].MessageID != second.MessageID {
		t.Fatalf("queued = %+v err %v", queued, err)
	}
	if _, err := h.sessions.Messages("", MessagesOptions{Session: "0000dead", Limit: 1}); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown session err = %v", err)
	}
	if _, err := h.sessions.Messages("", MessagesOptions{Limit: 0}); err == nil {
		t.Fatal("a zero limit was taken")
	}
	if _, err := h.sessions.ReadMessage("", second.MessageID+100); err == nil || !strings.Contains(err.Error(), "aged out of the log") {
		t.Fatalf("missing message err = %v", err)
	}
}
