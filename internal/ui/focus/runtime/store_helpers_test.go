package runtime

import (
	"testing"
	"time"

	"github.com/YoanWai/agent-manager/internal/store"
)

// storeWith opens a store holding one session, which is what a queued write
// reads before it refuses or performs the write.
func storeWith(t *testing.T) (*store.Store, store.Session) {
	t.Helper()
	st, err := store.Open(t.TempDir() + "/state.db")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	sess := store.Session{
		ID:         "s1",
		Name:       "one",
		Tool:       "claude",
		Cwd:        t.TempDir(),
		CreatedAt:  time.Now(),
		Status:     "idle",
		TmuxSocket: "amtest",
	}
	if err := st.CreateSession(sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	current, err := st.Get(sess.ID)
	if err != nil {
		t.Fatalf("read session: %v", err)
	}
	return st, current
}
