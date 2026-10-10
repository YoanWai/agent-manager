package remote

import (
	"context"
	"testing"
	"time"
)

type scriptedHost struct {
	t      *testing.T
	now    time.Time
	awake  bool
	down   bool
	serves int
}

func (h *scriptedHost) run(_ context.Context, argv []string) ([]byte, []byte, error) {
	words := remoteWords(h.t, argv)
	if h.down {
		return nil, []byte("ssh: connect to host gpu port 22: Operation timed out\n"), exitStatus(255)
	}
	switch words[1] {
	case "snapshot":
		if h.awake {
			return []byte(`{"version":1,"manager_awake":true,"sessions":[{"id":"a1"}]}`), nil, nil
		}
		return []byte(`{"version":1,"manager_awake":false,"sessions":[{"id":"a1"}]}`), nil, nil
	case "serve":
		h.serves++
		return []byte(`{"started":true,"pid":99}`), nil, nil
	}
	h.t.Fatalf("unexpected call %q", words)
	return nil, nil, nil
}

func TestRefreshKeepsTheLastGoodSnapshotAndStartsTheManagerOncePerMinute(t *testing.T) {
	start := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	host := &scriptedHost{t: t, now: start}
	c := newTestClient(t, host.run, WithClock(func() time.Time { return host.now }))
	ctx := context.Background()

	state := c.Refresh(ctx, "gpu")
	if !state.OK || state.Err != nil || !state.StartedManager || host.serves != 1 || !state.LastOK.Equal(start) {
		t.Fatalf("first refresh = %+v, serves %d", state, host.serves)
	}

	host.now = start.Add(30 * time.Second)
	state = c.Refresh(ctx, "gpu")
	if !state.OK || state.StartedManager || host.serves != 1 {
		t.Fatalf("refresh within the minute = %+v, serves %d", state, host.serves)
	}

	host.down = true
	host.now = start.Add(40 * time.Second)
	c.Refresh(ctx, "gpu")
	state = c.Refresh(ctx, "gpu")
	if state.OK || state.Failures != 2 || !Unreachable(state.Err) || len(state.Snapshot.Sessions) != 1 || !state.LastOK.Equal(start.Add(30*time.Second)) {
		t.Fatalf("refresh while down = %+v", state)
	}
	if cached := c.State("gpu"); cached.Failures != 2 || len(cached.Snapshot.Sessions) != 1 {
		t.Fatalf("State = %+v", cached)
	}

	host.down = false
	host.now = start.Add(61 * time.Second)
	state = c.Refresh(ctx, "gpu")
	if !state.OK || state.Failures != 0 || state.Err != nil || !state.StartedManager || host.serves != 2 {
		t.Fatalf("refresh after the minute = %+v, serves %d", state, host.serves)
	}

	host.awake = true
	host.now = start.Add(5 * time.Minute)
	state = c.Refresh(ctx, "gpu")
	if !state.OK || state.StartedManager || host.serves != 2 {
		t.Fatalf("refresh with a manager awake = %+v, serves %d", state, host.serves)
	}
}

func TestStateIsDroppedWithItsConnection(t *testing.T) {
	host := &scriptedHost{t: t, awake: true}
	c := newTestClient(t, host.run)
	c.Refresh(context.Background(), "gpu")
	c.Refresh(context.Background(), "cpu")

	c.SetConnections([]Connection{{Name: "gpu", Destination: "me@other-gpu"}, {Name: "cpu", Destination: "me@cpu"}})
	if state := c.State("gpu"); state.OK || len(state.Snapshot.Sessions) != 0 {
		t.Fatalf("repointed connection kept %+v", state)
	}
	if state := c.State("cpu"); !state.OK {
		t.Fatalf("unchanged connection lost its state: %+v", state)
	}
	c.SetConnections(nil)
	if state := c.State("cpu"); state.OK {
		t.Fatalf("removed connection kept %+v", state)
	}
}

func TestStateIsReadWithoutIO(t *testing.T) {
	c := newTestClient(t, func(context.Context, []string) ([]byte, []byte, error) {
		t.Fatal("State ran ssh")
		return nil, nil, nil
	})
	if state := c.State("gpu"); state.OK || state.Err != nil {
		t.Fatalf("State before any refresh = %+v", state)
	}
}
