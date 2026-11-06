package store

import (
	"slices"
	"strings"
)

// InboxFilter narrows the message log. Session keeps the messages sent to or
// from that session, and an empty one keeps every session's.
type InboxFilter struct {
	Session    string
	QueuedOnly bool
	Limit      int
}

// ListedMessage is a message from the log, with its place in its recipient's
// queue while it is undelivered; 1 is the one typed in next.
type ListedMessage struct {
	InboxMessage
	QueuePosition int
}

const inboxColumns = `id, session_id, sender_id, sender_name, subject, body, fingerprint,
       sent_at, claimed_at, delivered_at, dropped_at, read_at`

type inboxRow interface {
	Scan(dest ...any) error
}

func scanInboxMessage(row inboxRow, extra ...any) (InboxMessage, error) {
	var msg InboxMessage
	var sentAt, claimedAt, deliveredAt, droppedAt, readAt int64
	dest := append([]any{&msg.ID, &msg.SessionID, &msg.SenderID, &msg.SenderName, &msg.Subject, &msg.Body, &msg.Fingerprint,
		&sentAt, &claimedAt, &deliveredAt, &droppedAt, &readAt}, extra...)
	if err := row.Scan(dest...); err != nil {
		return InboxMessage{}, err
	}
	msg.SentAt = decodeTime(sentAt)
	msg.ClaimedAt = decodeTime(claimedAt)
	msg.DeliveredAt = decodeTime(deliveredAt)
	msg.DroppedAt = decodeTime(droppedAt)
	msg.ReadAt = decodeTime(readAt)
	return msg, nil
}

// InboxMessages lists the log the filter keeps. The queue comes in delivery
// order from its head; the whole log is its most recent Limit messages,
// oldest first.
func (s *Store) InboxMessages(filter InboxFilter) ([]ListedMessage, error) {
	var where []string
	var args []any
	if filter.Session != "" {
		where = append(where, `(session_id = ? OR sender_id = ?)`)
		args = append(args, filter.Session, filter.Session)
	}
	if filter.QueuedOnly {
		where = append(where, `delivered_at = 0`)
	}
	query := `SELECT ` + inboxColumns + `,
       CASE WHEN delivered_at = 0 THEN (
         SELECT COUNT(*) FROM session_inbox queue
          WHERE queue.session_id = session_inbox.session_id AND queue.delivered_at = 0 AND queue.id <= session_inbox.id
       ) ELSE 0 END
  FROM session_inbox`
	if len(where) > 0 {
		query += ` WHERE ` + strings.Join(where, ` AND `)
	}
	order := `DESC`
	if filter.QueuedOnly {
		order = `ASC`
	}
	query += ` ORDER BY id ` + order + ` LIMIT ?`
	args = append(args, filter.Limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var listed []ListedMessage
	for rows.Next() {
		var position int
		msg, err := scanInboxMessage(rows, &position)
		if err != nil {
			return nil, err
		}
		listed = append(listed, ListedMessage{InboxMessage: msg, QueuePosition: position})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !filter.QueuedOnly {
		slices.Reverse(listed)
	}
	return listed, nil
}

// LoggedMessage reads one message by id whoever sent it, for the person
// watching rather than the sender confirming delivery.
func (s *Store) LoggedMessage(id int64) (InboxMessage, error) {
	return scanInboxMessage(s.db.QueryRow(`SELECT `+inboxColumns+` FROM session_inbox WHERE id = ?`, id))
}
