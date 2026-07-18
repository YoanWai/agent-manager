package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDecodeTimeReadsBothEncodings(t *testing.T) {
	seconds := time.Date(2026, 7, 18, 12, 0, 0, 0, time.UTC)
	if got := decodeTime(seconds.Unix()); !got.Equal(seconds) {
		t.Errorf("legacy seconds: got %v, want %v", got, seconds)
	}
	nanos := time.Date(2026, 7, 18, 12, 0, 0, 123456789, time.UTC)
	if got := decodeTime(nanos.UnixNano()); !got.Equal(nanos) {
		t.Errorf("nanoseconds: got %v, want %v", got, nanos)
	}
	if got := decodeTime(0); !got.IsZero() {
		t.Errorf("zero: got %v, want zero time", got)
	}
}

// A session's launch time keeps sub-second precision across the store, so
// two sessions started in the same second stay distinguishable and ordered.
func TestCreatedAtKeepsSubSecondPrecision(t *testing.T) {
	st, err := Open(filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	launch := time.Now().Truncate(time.Second).Add(250 * time.Millisecond)
	if err := st.CreateSession(Session{ID: "s1", Name: "n", Tool: "codex", Cwd: "/x", Group: "g", Status: "idle", CreatedAt: launch}); err != nil {
		t.Fatal(err)
	}
	got, err := st.Get("s1")
	if err != nil {
		t.Fatal(err)
	}
	if got.CreatedAt.UnixNano() != launch.UnixNano() {
		t.Fatalf("CreatedAt round-trip lost precision: got %d, want %d",
			got.CreatedAt.UnixNano(), launch.UnixNano())
	}
}
