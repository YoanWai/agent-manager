package store

import (
	"database/sql"
	"errors"
	"slices"
	"testing"
	"time"
)

func listedIDs(listed []ListedMessage) []int64 {
	ids := make([]int64, len(listed))
	for i, msg := range listed {
		ids[i] = msg.ID
	}
	return ids
}

func TestInboxMessagesFiltersOrdersAndLimits(t *testing.T) {
	st := newTestStore(t)
	start := time.Now()
	enqueue := func(to, from, body string, offset int) int64 {
		t.Helper()
		msg := InboxMessage{SessionID: to, SenderID: from, SenderName: from + "-name", Subject: "about " + body, Body: body, Fingerprint: body, SentAt: start.Add(time.Duration(offset) * time.Second)}
		id, err := st.Enqueue(msg, DefaultInboxLimits)
		if err != nil {
			t.Fatalf("Enqueue %s: %v", body, err)
		}
		return id
	}
	toTarget := enqueue("target01", "sender01", "first", 0)
	delivered := enqueue("target01", "sender02", "second", 1)
	fromTarget := enqueue("other001", "target01", "third", 2)
	unrelated := enqueue("other001", "sender02", "fourth", 3)
	lastToTarget := enqueue("target01", "sender02", "fifth", 4)
	if err := st.MarkDelivered(delivered, start); err != nil {
		t.Fatalf("MarkDelivered: %v", err)
	}

	all, err := st.InboxMessages(InboxFilter{Limit: 20})
	if err != nil {
		t.Fatalf("InboxMessages: %v", err)
	}
	if want := []int64{toTarget, delivered, fromTarget, unrelated, lastToTarget}; !slices.Equal(listedIDs(all), want) {
		t.Fatalf("whole log = %v, want oldest to newest %v", listedIDs(all), want)
	}
	if all[0].Subject != "about first" || all[0].SenderName != "sender01-name" || all[0].Body != "first" {
		t.Fatalf("listed message lost its fields: %+v", all[0])
	}

	recent, err := st.InboxMessages(InboxFilter{Limit: 2})
	if err != nil {
		t.Fatalf("InboxMessages limit: %v", err)
	}
	if want := []int64{unrelated, lastToTarget}; !slices.Equal(listedIDs(recent), want) {
		t.Fatalf("limited log = %v, want the two most recent %v", listedIDs(recent), want)
	}

	session, err := st.InboxMessages(InboxFilter{Session: "target01", Limit: 20})
	if err != nil {
		t.Fatalf("InboxMessages session: %v", err)
	}
	if want := []int64{toTarget, delivered, fromTarget, lastToTarget}; !slices.Equal(listedIDs(session), want) {
		t.Fatalf("session log = %v, want messages to and from it %v", listedIDs(session), want)
	}

	queued, err := st.InboxMessages(InboxFilter{Session: "target01", QueuedOnly: true, Limit: 20})
	if err != nil {
		t.Fatalf("InboxMessages queued: %v", err)
	}
	if want := []int64{toTarget, fromTarget, lastToTarget}; !slices.Equal(listedIDs(queued), want) {
		t.Fatalf("queue = %v, want undelivered in delivery order %v", listedIDs(queued), want)
	}
	positions := map[int64]int{}
	for _, msg := range queued {
		positions[msg.ID] = msg.QueuePosition
	}
	// Positions count within each recipient's own queue.
	if positions[toTarget] != 1 || positions[lastToTarget] != 2 || positions[fromTarget] != 1 {
		t.Fatalf("queue positions = %v", positions)
	}
	for _, msg := range session {
		if msg.ID == delivered && msg.QueuePosition != 0 {
			t.Fatalf("a delivered message holds queue position %d", msg.QueuePosition)
		}
	}

	head, err := st.InboxMessages(InboxFilter{QueuedOnly: true, Limit: 1})
	if err != nil || !slices.Equal(listedIDs(head), []int64{toTarget}) {
		t.Fatalf("limited queue = %v err %v, want its head", listedIDs(head), err)
	}
}

func TestLoggedMessageReadsAnySendersMessage(t *testing.T) {
	st := newTestStore(t)
	msg := message("rebase on main", time.Now())
	msg.Subject = "rebase"
	id, err := st.Enqueue(msg, DefaultInboxLimits)
	if err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	got, err := st.LoggedMessage(id)
	if err != nil {
		t.Fatalf("LoggedMessage: %v", err)
	}
	if got.SenderID != "sender01" || got.SessionID != "target01" || got.Subject != "rebase" || got.Body != "rebase on main" || got.SentAt.IsZero() {
		t.Fatalf("LoggedMessage = %+v", got)
	}
	if _, err := st.LoggedMessage(id + 1); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("missing id err = %v, want sql.ErrNoRows", err)
	}
}
