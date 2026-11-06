package sessioncmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/YoanWai/agent-manager/internal/store"
	"github.com/charmbracelet/x/ansi"
)

// MessageEntry is one message as the person watching sees it: who sent it to
// whom, when, what it is about and where it stands.
type MessageEntry struct {
	MessageID     int64  `json:"message_id"`
	FromSessionID string `json:"from_session_id" jsonschema:"sending session; empty for a sender on another machine, which has no session here"`
	FromName      string `json:"from_name"`
	ToSessionID   string `json:"to_session_id"`
	ToName        string `json:"to_name"`
	SentAt        string `json:"sent_at" jsonschema:"RFC3339 time the message was sent"`
	Subject       string `json:"subject" jsonschema:"the subject its sender gave, or else the first line of the message, cut short"`
	State         string `json:"state" jsonschema:"queued, held, delivered, dropped or answered, as message_status reports them"`
	Reason        string `json:"reason,omitempty" jsonschema:"why a held or dropped message is in that state, and what to do about it"`
	DeliveredAt   string `json:"delivered_at,omitempty" jsonschema:"RFC3339 time the message reached the prompt"`
	QueuePosition int    `json:"queue_position,omitempty" jsonschema:"while undelivered, its place in the recipient's queue; 1 means it is next"`
	Body          string `json:"body,omitempty" jsonschema:"the full message, on a single read only"`
}

type MessagesOptions struct {
	// Session keeps the messages sent to or from it; empty keeps every session's.
	Session    string
	QueuedOnly bool
	Limit      int
}

const DefaultMessagesLimit = 20

const (
	maxSubjectBytes   = 100
	subjectShownWidth = 72
)

// MessageSubject is the line a watcher reads for a message: the sender's own
// subject when it gave one, else the body's first line, on one line and cut
// to a width a list row holds.
func MessageSubject(subject, body string) string {
	line := subject
	if strings.TrimSpace(line) == "" {
		line = firstLine(body)
	}
	line = strings.Join(strings.Fields(strings.Map(dropControl, line)), " ")
	return ansi.Truncate(line, subjectShownWidth, "…")
}

func firstLine(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) != "" {
			return line
		}
	}
	return ""
}

// dropControl keeps tabs as spaces so words they split stay apart.
func dropControl(r rune) rune {
	switch {
	case r == '\t':
		return ' '
	case unicode.IsControl(r):
		return -1
	}
	return r
}

func validSubject(subject string) (string, error) {
	subject = strings.TrimSpace(subject)
	if len(subject) > maxSubjectBytes {
		return "", fmt.Errorf("subject is %d bytes, over the %d byte limit; keep it to a short line", len(subject), maxSubjectBytes)
	}
	if strings.ContainsFunc(subject, unicode.IsControl) {
		return "", fmt.Errorf("subject %q holds a line break or a control character; keep it to one plain line", subject)
	}
	return subject, nil
}

// messageOutcome is where a message stands, the one reading message_status
// and the message log share so a hold reads the same in both.
type messageOutcome struct {
	State       string
	Reason      string
	DeliveredAt string
}

func outcomeOf(msg store.InboxMessage, heldReason func(recipient string) (string, error)) (messageOutcome, error) {
	switch {
	// A drop stamps the delivery column as well, so that a message nothing
	// will ever type leaves the queue, and is read first for that reason.
	case !msg.DroppedAt.IsZero():
		return messageOutcome{State: "dropped", Reason: "Agent Manager could not type it into the pane, and never retries a message; send it again"}, nil
	case !msg.ReadAt.IsZero():
		return messageOutcome{State: "answered", DeliveredAt: msg.DeliveredAt.Format(time.RFC3339)}, nil
	case !msg.DeliveredAt.IsZero():
		return messageOutcome{State: "delivered", DeliveredAt: msg.DeliveredAt.Format(time.RFC3339)}, nil
	}
	held, err := heldReason(msg.SessionID)
	if err != nil {
		return messageOutcome{}, err
	}
	if held != "" {
		return messageOutcome{State: "held", Reason: held}, nil
	}
	return messageOutcome{State: "queued"}, nil
}

// Messages lists the message log, which keeps a delivered message for a day.
// Every session is the same user's, so any caller may read all of it.
func (s *Sessions) Messages(sessionID string, opts MessagesOptions) ([]MessageEntry, error) {
	if opts.Limit < 1 {
		return nil, fmt.Errorf("limit is %d; list at least one message", opts.Limit)
	}
	runtime, err := s.open()
	if err != nil {
		return nil, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return nil, err
	}
	if opts.Session != "" {
		if _, err := runtime.store.Get(opts.Session); errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("session %s does not exist; call %s for current ids", opts.Session, runtime.words.ListSessions)
		} else if err != nil {
			return nil, err
		}
	}
	listed, err := runtime.store.InboxMessages(store.InboxFilter{Session: opts.Session, QueuedOnly: opts.QueuedOnly, Limit: opts.Limit})
	if err != nil {
		return nil, err
	}
	log := newMessageLog(runtime)
	entries := make([]MessageEntry, 0, len(listed))
	for _, msg := range listed {
		entry, err := log.entry(msg.InboxMessage)
		if err != nil {
			return nil, err
		}
		if entry.State == "queued" || entry.State == "held" {
			entry.QueuePosition = msg.QueuePosition
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

// ReadMessage returns one message from the log with its full body.
func (s *Sessions) ReadMessage(sessionID string, messageID int64) (MessageEntry, error) {
	runtime, err := s.open()
	if err != nil {
		return MessageEntry{}, err
	}
	defer runtime.Close()
	if _, err := runtime.optionalCaller(sessionID); err != nil {
		return MessageEntry{}, err
	}
	msg, err := runtime.store.LoggedMessage(messageID)
	if errors.Is(err, sql.ErrNoRows) {
		return MessageEntry{}, fmt.Errorf("message %d does not exist, or has aged out of the log", messageID)
	}
	if err != nil {
		return MessageEntry{}, err
	}
	entry, err := newMessageLog(runtime).entry(msg)
	if err != nil {
		return MessageEntry{}, err
	}
	entry.Body = msg.Body
	return entry, nil
}

// messageLog reads each recipient's name and hold once per call, however many
// of its messages the log holds.
type messageLog struct {
	runtime *runtime
	names   map[string]string
	holds   map[string]string
}

func newMessageLog(r *runtime) *messageLog {
	return &messageLog{runtime: r, names: map[string]string{}, holds: map[string]string{}}
}

func (l *messageLog) name(id string) (string, error) {
	if name, ok := l.names[id]; ok {
		return name, nil
	}
	sess, err := l.runtime.store.Get(id)
	if err != nil {
		return "", err
	}
	l.names[id] = sess.Name
	return sess.Name, nil
}

func (l *messageLog) heldReason(recipient string) (string, error) {
	if held, ok := l.holds[recipient]; ok {
		return held, nil
	}
	held, err := l.runtime.heldReason(recipient)
	if err != nil {
		return "", err
	}
	l.holds[recipient] = held
	return held, nil
}

func (l *messageLog) entry(msg store.InboxMessage) (MessageEntry, error) {
	outcome, err := outcomeOf(msg, l.heldReason)
	if err != nil {
		return MessageEntry{}, err
	}
	toName, err := l.name(msg.SessionID)
	if err != nil {
		return MessageEntry{}, err
	}
	return MessageEntry{
		MessageID:     msg.ID,
		FromSessionID: msg.SenderID,
		FromName:      msg.SenderName,
		ToSessionID:   msg.SessionID,
		ToName:        toName,
		SentAt:        msg.SentAt.Format(time.RFC3339),
		Subject:       MessageSubject(msg.Subject, msg.Body),
		State:         outcome.State,
		Reason:        outcome.Reason,
		DeliveredAt:   outcome.DeliveredAt,
	}, nil
}
