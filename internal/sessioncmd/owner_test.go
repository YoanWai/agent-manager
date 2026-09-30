package sessioncmd

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
)

type refusingArchiveOwner struct {
	request ArchiveRequest
	err     error
}

func (o *refusingArchiveOwner) Archive(request ArchiveRequest) (Session, error) {
	o.request = request
	return Session{}, o.err
}

func TestSessionsOwnerFailureDoesNotOpenLocalRuntime(t *testing.T) {
	want := errors.New("owner unavailable")
	owner := &refusingArchiveOwner{err: want}
	sessions := NewSessionsWithArchiveOwner(t.TempDir(), CLIVocabulary(), owner)
	sessions.loadConfig = func(string) (config.Config, error) {
		t.Fatal("an explicitly routed archive must not open local config or store")
		return config.Config{}, nil
	}
	_, err := sessions.Archive("cafe", "beef", true)
	if !errors.Is(err, want) {
		t.Fatalf("error = %v, want %v", err, want)
	}
	if owner.request.CallerID != "cafe" || owner.request.TargetID != "beef" || !owner.request.Archived || owner.request.Words.ListSessions != CLIVocabulary().ListSessions {
		t.Fatalf("request = %+v", owner.request)
	}
}

func TestSessionOwnerPreservesArchiveAndRestore(t *testing.T) {
	h := newSessionHarness(t)
	created, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "worker", Prompt: "OWNER-SNAPSHOT"})
	if err != nil {
		t.Fatal(err)
	}
	waitForSessionOutput(t, h.sessions, h.caller.ID, created.ID, "OWNER-SNAPSHOT")
	cfg, err := h.sessions.loadConfig(h.sessions.configDir)
	if err != nil {
		t.Fatal(err)
	}
	owner := NewSessionOwner(cfg, h.store, h.driver)
	h.sessions.archiveOwner = owner
	archived, err := h.sessions.Archive(h.caller.ID, created.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || !archived.Running || !h.driver.Exists(created.ID) {
		t.Fatalf("archive changed pane lifetime: %+v", archived)
	}
	snapshot, err := h.store.Snapshot(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot == "" {
		t.Fatal("live pane was not snapshotted")
	}
	if _, err := h.sessions.Archive(h.caller.ID, h.caller.ID, true); err == nil || !strings.Contains(err.Error(), "archive itself") {
		t.Fatalf("self archive = %v", err)
	}
	restored, err := h.sessions.Archive(h.caller.ID, created.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	if restored.Archived || !restored.Running {
		t.Fatalf("restore = %+v", restored)
	}
	if _, err := h.store.ListSessions(true); err != nil {
		t.Fatalf("borrowed store closed: %v", err)
	}
}

func TestInboxOwnerUsesDeliveryTimeAndPreservesQueuedMessages(t *testing.T) {
	h := newSessionHarness(t)
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-time.Hour)
	ids := make([]int64, 3)
	for i := range ids {
		id, err := h.store.Enqueue(store.InboxMessage{SessionID: h.caller.ID, SenderID: "beef", SenderName: "sender", Body: fmt.Sprint(i), Fingerprint: fmt.Sprint(i), SentAt: old}, store.DefaultInboxLimits)
		if err != nil {
			t.Fatal(err)
		}
		ids[i] = id
	}
	if err := h.store.MarkDelivered(ids[0], old); err != nil {
		t.Fatal(err)
	}
	if err := h.store.MarkDelivered(ids[1], recent); err != nil {
		t.Fatal(err)
	}
	owner := NewInboxOwner(h.store)
	owner.now = func() time.Time { return now }
	if err := owner.MaintainInbox(); err != nil {
		t.Fatal(err)
	}
	if _, err := h.store.Message(ids[0], "beef"); err == nil {
		t.Fatal("expired delivered message remained")
	}
	for _, id := range ids[1:] {
		if _, err := h.store.Message(id, "beef"); err != nil {
			t.Fatalf("retained message %d: %v", id, err)
		}
	}
}
