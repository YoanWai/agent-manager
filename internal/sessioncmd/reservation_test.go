package sessioncmd

import (
	"strings"
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

func TestReservationsSurfaceOverlapWithoutBlockingIt(t *testing.T) {
	h := newSessionHarness(t)
	rival, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "rival"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	held, err := h.sessions.Reserve(h.caller.ID, []string{"internal/store/*.go"}, "", "adding the inbox table", 0)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if len(held.Reserved) != 1 || len(held.Conflicts) != 0 {
		t.Fatalf("first reservation = %+v", held)
	}
	if !held.Reserved[0].Mine || held.Reserved[0].Mode != "exclusive" {
		t.Fatalf("reservation = %+v", held.Reserved[0])
	}

	// A literal path under a reserved glob is the collision worktrees miss.
	clash, err := h.sessions.Reserve(rival.ID, []string{"internal/store/store.go"}, "", "", 0)
	if err != nil {
		t.Fatalf("Reserve rival: %v", err)
	}
	if len(clash.Conflicts) != 1 {
		t.Fatalf("overlap was not reported: %+v", clash)
	}
	if clash.Conflicts[0].Holder != "calling-agent" || clash.Conflicts[0].Note != "adding the inbox table" {
		t.Fatalf("conflict = %+v", clash.Conflicts[0])
	}
	// Advisory: the second lease is still granted, so neither agent is stuck.
	if len(clash.Reserved) != 1 {
		t.Fatalf("an advisory lease must still be granted: %+v", clash)
	}

	listed, err := h.sessions.Reservations(h.caller.ID)
	if err != nil {
		t.Fatalf("Reservations: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("listed = %+v", listed)
	}
}

func TestSharedLeasesOnlyClashWithExclusiveOnes(t *testing.T) {
	h := newSessionHarness(t)
	rival, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "rival"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := h.sessions.Reserve(h.caller.ID, []string{"docs/usage.md"}, "shared", "", 0); err != nil {
		t.Fatalf("Reserve shared: %v", err)
	}
	quiet, err := h.sessions.Reserve(rival.ID, []string{"docs/usage.md"}, "shared", "", 0)
	if err != nil {
		t.Fatalf("Reserve second shared: %v", err)
	}
	if len(quiet.Conflicts) != 0 {
		t.Fatalf("two shared leases should not clash: %+v", quiet.Conflicts)
	}
	loud, err := h.sessions.Reserve(rival.ID, []string{"docs/usage.md"}, "exclusive", "", 0)
	if err != nil {
		t.Fatalf("Reserve exclusive: %v", err)
	}
	if len(loud.Conflicts) != 1 {
		t.Fatalf("an exclusive lease over a shared one should clash: %+v", loud)
	}
	if _, err := h.sessions.Reserve(h.caller.ID, []string{"docs/usage.md"}, "sideways", "", 0); err == nil ||
		!strings.Contains(err.Error(), "unknown mode") {
		t.Fatalf("bad mode error = %v", err)
	}
	if _, err := h.sessions.Reserve(h.caller.ID, nil, "", "", 0); err == nil {
		t.Fatal("reserving nothing should be refused")
	}
	for _, ttl := range []time.Duration{-time.Minute, MaxReservationTTL + time.Minute} {
		if _, err := h.sessions.Reserve(h.caller.ID, []string{"docs/usage.md"}, "", "", ttl); err == nil ||
			!strings.Contains(err.Error(), "outside 0 to "+MaxReservationTTL.String()) {
			t.Fatalf("a %s lease = %v, want a refusal naming the bound", ttl, err)
		}
	}
}

func TestALapsedLeaseStopsBlockingAndReleaseClearsTheRest(t *testing.T) {
	h := newSessionHarness(t)
	rival, err := h.sessions.Create(h.caller.ID, CreateSessionOptions{Name: "rival"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	// A lease whose expiry is already behind us stands in for an agent that
	// died holding one, which is the case the whole advisory design rests on.
	lapsed := time.Now().Add(-time.Minute)
	if _, err := h.store.Reserve(store.Reservation{
		ID:         "lapsed01",
		SessionID:  rival.ID,
		Pattern:    "internal/ui/*.go",
		Mode:       store.ReservationExclusive,
		AcquiredAt: lapsed.Add(-time.Hour),
		ExpiresAt:  lapsed,
	}); err != nil {
		t.Fatalf("write a lapsed lease: %v", err)
	}
	fresh, err := h.sessions.Reserve(h.caller.ID, []string{"internal/ui/model.go"}, "", "", 0)
	if err != nil {
		t.Fatalf("Reserve after lapse: %v", err)
	}
	if len(fresh.Conflicts) != 0 {
		t.Fatalf("a lapsed lease still blocks: %+v", fresh.Conflicts)
	}

	if _, err := h.sessions.Reserve(h.caller.ID, []string{"a.go", "b.go"}, "", "", 0); err != nil {
		t.Fatalf("Reserve more: %v", err)
	}
	released, err := h.sessions.ReleaseFiles(h.caller.ID, []string{"a.go"})
	if err != nil || released != 1 {
		t.Fatalf("ReleaseFiles = %d, %v", released, err)
	}
	released, err = h.sessions.ReleaseFiles(h.caller.ID, nil)
	if err != nil || released != 2 {
		t.Fatalf("ReleaseFiles all = %d, %v", released, err)
	}
	listed, err := h.sessions.Reservations(h.caller.ID)
	if err != nil {
		t.Fatalf("Reservations: %v", err)
	}
	if len(listed) != 0 {
		t.Fatalf("leases survived a full release: %+v", listed)
	}
}

func TestReleasingBlankPathsDoesNotDropEveryLease(t *testing.T) {
	h := newSessionHarness(t)
	if _, err := h.sessions.Reserve(h.caller.ID, []string{"a.go", "b.go"}, "", "", 0); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := h.sessions.ReleaseFiles(h.caller.ID, []string{"  "}); err == nil ||
		!strings.Contains(err.Error(), "omit paths entirely") {
		t.Fatalf("blank release error = %v", err)
	}
	listed, err := h.sessions.Reservations(h.caller.ID)
	if err != nil {
		t.Fatalf("Reservations: %v", err)
	}
	if len(listed) != 2 {
		t.Fatalf("a blank path released real leases: %+v", listed)
	}
}
