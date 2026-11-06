package ui

import (
	"fmt"
	"time"

	"github.com/YoanWai/agent-manager/internal/sessioncmd"
	uiinbox "github.com/YoanWai/agent-manager/internal/ui/inbox"
	uirail "github.com/YoanWai/agent-manager/internal/ui/rail"
)

type inboxRoot struct{ *Model }

func (r inboxRoot) LoadInbox(sessionID string, gen uint64) {
	r.enqueueEffect(uiinbox.LoadRequest{SessionID: sessionID, Gen: gen}, 0, false)
}

func (r inboxRoot) ReadInboxMessage(messageID int64, gen uint64) {
	r.enqueueEffect(uiinbox.ReadRequest{MessageID: messageID, Gen: gen}, 0, false)
}

// openInbox opens the inbox of the session row target names; a group has
// none.
func (m *Model) openInbox(target uirail.Selection) {
	sess, ok := m.sessionByID(target.SessionID)
	if !ok {
		return
	}
	m.push(uiinbox.NewSurface(inboxRoot{m}, sess.ID, m.displayName(sess), m.prefs.glyphMode))
}

func (m *Model) applyInboxLoad(request uiinbox.LoadRequest, log uiinbox.Log, err error) {
	if s, ok := surfaceOf[*uiinbox.Surface](m); ok && s.SessionID() == request.SessionID {
		s.Loaded(request.Gen, log, err)
	}
}

func (m *Model) applyInboxRead(request uiinbox.ReadRequest, body string, err error) {
	if s, ok := surfaceOf[*uiinbox.Surface](m); ok {
		s.BodyRead(request.Gen, body, err)
	}
}

// followInbox reloads the open inbox on every workspace refresh. A queue
// count is not enough to go on: one message going in while another arrives
// leaves it unchanged, and a hold reason changes with the recipient's screen.
func (m *Model) followInbox() {
	if s, ok := surfaceOf[*uiinbox.Surface](m); ok {
		s.Refresh()
	}
}

func (s effectServices) inboxServices() uiinbox.Services {
	open := func() (*sessioncmd.Sessions, error) {
		backend, err := sessioncmd.BorrowBackend(sessioncmd.Runtime{
			Config: s.cfg, Store: s.store, Driver: s.driver, Hooks: s.hooks, Snapshot: s.store.SetSnapshot,
		})
		if err != nil {
			return nil, err
		}
		return sessioncmd.NewSessionsWithBackend(backend, sessioncmd.CLIVocabulary()), nil
	}
	return uiinbox.Services{
		Load: func(sessionID string) (uiinbox.Log, error) {
			sessions, err := open()
			if err != nil {
				return uiinbox.Log{}, err
			}
			queued, err := sessions.Messages("", sessioncmd.MessagesOptions{Session: sessionID, QueuedOnly: true, Limit: sessioncmd.DefaultMessagesLimit})
			if err != nil {
				return uiinbox.Log{}, err
			}
			latest, err := sessions.Messages("", sessioncmd.MessagesOptions{Session: sessionID, Limit: sessioncmd.DefaultMessagesLimit})
			if err != nil {
				return uiinbox.Log{}, err
			}
			queuedEntries, err := inboxEntries(queued)
			if err != nil {
				return uiinbox.Log{}, err
			}
			latestEntries, err := inboxEntries(latest)
			if err != nil {
				return uiinbox.Log{}, err
			}
			return uiinbox.Split(queuedEntries, latestEntries), nil
		},
		Read: func(messageID int64) (string, error) {
			sessions, err := open()
			if err != nil {
				return "", err
			}
			entry, err := sessions.ReadMessage("", messageID)
			return entry.Body, err
		},
	}
}

func inboxEntries(listed []sessioncmd.MessageEntry) ([]uiinbox.Entry, error) {
	entries := make([]uiinbox.Entry, 0, len(listed))
	for _, entry := range listed {
		sentAt, err := time.Parse(time.RFC3339, entry.SentAt)
		if err != nil {
			return nil, fmt.Errorf("message %d: %w", entry.MessageID, err)
		}
		entries = append(entries, uiinbox.Entry{
			ID: entry.MessageID, From: entry.FromName, To: entry.ToName, SentAt: sentAt,
			Subject: entry.Subject, State: entry.State, Reason: entry.Reason, Position: entry.QueuePosition,
		})
	}
	return entries, nil
}
