package execution

import (
	"iter"
	"maps"
	"time"
)

type SessionSummary struct {
	ID       string
	Name     string
	Tool     string
	Group    string
	Status   string
	Archived bool
	ParentID string
}

type View struct {
	listedAt time.Time
	sessions []SessionSummary
	queued   map[string]int
	agents   AgentStats
}

func ReadOnly(snapshot Snapshot) View {
	sessions := make([]SessionSummary, 0, len(snapshot.Sessions))
	for _, session := range snapshot.Sessions {
		sessions = append(sessions, SessionSummary{ID: session.ID, Name: session.Name, Tool: session.Tool, Group: session.Group, Status: session.Status, Archived: session.Archived, ParentID: session.ParentID})
	}
	return View{listedAt: snapshot.ListedAt, sessions: sessions, queued: maps.Clone(snapshot.QueuedMessages), agents: snapshot.Agents}
}

func (v View) ListedAt() time.Time { return v.listedAt }

func (v View) Sessions() iter.Seq[SessionSummary] {
	return func(yield func(SessionSummary) bool) {
		for _, session := range v.sessions {
			if !yield(session) {
				return
			}
		}
	}
}

func (v View) Queued(sessionID string) int { return v.queued[sessionID] }

func (v View) Agents() AgentStats { return v.agents }
