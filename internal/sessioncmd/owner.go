package sessioncmd

import (
	"sync"
	"time"

	"github.com/YoanWai/agent-manager/internal/config"
	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/YoanWai/agent-manager/internal/tmux"
)

type ArchiveRequest struct {
	CallerID string
	TargetID string
	Archived bool
	Words    Vocabulary
}

type ArchiveOwner interface {
	Archive(ArchiveRequest) (Session, error)
}

type InboxMaintenance interface {
	MaintainInbox() error
}

func NewSessionsWithArchiveOwner(configDir string, words Vocabulary, owner ArchiveOwner) *Sessions {
	if owner == nil {
		panic("archive owner is required for explicit composition")
	}
	sessions := NewSessions(configDir, words)
	sessions.archiveOwner = owner
	return sessions
}

type SessionOwner struct {
	mu        sync.Mutex
	lifecycle *Lifecycle
}

func NewSessionOwner(cfg config.Config, st *store.Store, driver *tmux.Driver) *SessionOwner {
	lifecycle, err := NewLifecycle(Runtime{
		Config:   cfg,
		Store:    st,
		Driver:   driver,
		Snapshot: st.SetSnapshot,
	})
	if err != nil {
		panic(err)
	}
	return &SessionOwner{lifecycle: lifecycle}
}

func (owner *SessionOwner) Archive(request ArchiveRequest) (Session, error) {
	owner.mu.Lock()
	defer owner.mu.Unlock()
	return owner.lifecycle.SetArchivedForSession(request.CallerID, request.TargetID, request.Archived, request.Words)
}

type InboxOwner struct {
	store *store.Store
	now   func() time.Time
}

func NewInboxOwner(st *store.Store) *InboxOwner {
	return &InboxOwner{store: st, now: time.Now}
}

func (owner *InboxOwner) MaintainInbox() error {
	return owner.store.PruneInbox(owner.now().Add(-24 * time.Hour))
}
